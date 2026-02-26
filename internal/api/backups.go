package api

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/robfig/cron/v3"
)

const (
	defaultBackupSchedule = "0 2 * * *"
	// Cron expressions are minute-based; poll more frequently so `* * * * *`
	// schedules trigger near the expected minute boundary.
	backupPollInterval    = 30 * time.Second
	backupRunStaleAfter   = 24 * time.Hour
	backupPruneInterval   = 24 * time.Hour
	resticRetryLock       = "2m"
	defaultSFTPPort       = 22
	defaultS3BucketLookup = "auto"
	maxShellOutputBytes   = 1 << 20   // 1 MiB
	zipExportDiskHeadroom = 256 << 20 // 256 MiB
)

type BackupService struct {
	db                 *database.DB
	agent              *agent.Client
	once               sync.Once
	wg                 sync.WaitGroup
	sem                chan struct{}
	exportSem          chan struct{}
	rcloneInstallMu    sync.RWMutex
	rcloneInstallState *BackupRcloneStatus
}

func NewBackupService(db *database.DB, agentClient *agent.Client) *BackupService {
	workerCount := runtime.NumCPU() / 2
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > 4 {
		workerCount = 4
	}
	return &BackupService{
		db:        db,
		agent:     agentClient,
		sem:       make(chan struct{}, workerCount),
		exportSem: make(chan struct{}, 1),
	}
}

func (s *BackupService) Start(ctx context.Context) {
	s.once.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runLoop(ctx)
		}()
	})
}

func (s *BackupService) runLoop(ctx context.Context) {
	s.runDueBackups(ctx)
	t := time.NewTicker(backupPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.runDueBackups(ctx)
		}
	}
}

func parseCronSchedule(expr string) (cron.Schedule, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	return parser.Parse(strings.TrimSpace(expr))
}

func nextRunAt(expr string, from time.Time) (time.Time, error) {
	sched, err := parseCronSchedule(expr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(from), nil
}

func toJSONStringSlice(v []string) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func fromJSONStringSlice(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []string{}
	}
	seen := make(map[string]struct{}, len(out))
	res := make([]string, 0, len(out))
	for _, item := range out {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		res = append(res, item)
	}
	return res
}

type resticLSNode struct {
	StructType string `json:"struct_type"`
	Path       string `json:"path"`
	Type       string `json:"type"`
}

type backupManifest struct {
	Version   int                    `json:"version"`
	CreatedAt string                 `json:"created_at"`
	Username  string                 `json:"username"`
	Sites     []backupManifestSite   `json:"sites"`
	Databases []backupManifestDBDump `json:"databases"`
}

type backupManifestSite struct {
	Domain   string `json:"domain"`
	RootPath string `json:"root_path"`
}

type backupManifestDBDump struct {
	DBName   string `json:"db_name"`
	DumpPath string `json:"dump_path"`
}

type resticBackupMessage struct {
	MessageType string `json:"message_type"`
	SnapshotID  string `json:"snapshot_id"`
}

type resticSnapshotsEnvelope struct {
	Snapshots []BackupSnapshot `json:"snapshots"`
}

type resticStatsSummary struct {
	TotalSize uint64 `json:"total_size"`
}

func normalizePathForMatch(v string) string {
	v = filepath.ToSlash(filepath.Clean(strings.TrimSpace(v)))
	if v == "." || v == "/" || v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "/") {
		v = "/" + v
	}
	return strings.TrimSuffix(v, "/")
}

func resolveSnapshotPathBySuffixUnique(ctx context.Context, run func(context.Context, string) ([]byte, error), resticCmd, snapshotID, suffix string, wantDir bool) (string, error) {
	snap := strings.TrimSpace(snapshotID)
	if snap == "" {
		return "", fmt.Errorf("snapshot id is required")
	}
	matchSuffix := normalizePathForMatch(suffix)
	if matchSuffix == "" {
		return "", fmt.Errorf("path suffix is required")
	}
	output, err := run(ctx, fmt.Sprintf("%s ls --json %s", resticCmd, shellQuote(snap)))
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	matches := make([]string, 0, 2)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "[" || line == "]" {
			continue
		}
		line = strings.TrimSuffix(line, ",")
		var node resticLSNode
		if err := json.Unmarshal([]byte(line), &node); err != nil {
			continue
		}
		if node.StructType != "node" || strings.TrimSpace(node.Path) == "" {
			continue
		}
		if wantDir && node.Type != "dir" {
			continue
		}
		if !wantDir && node.Type != "file" {
			continue
		}
		p := normalizePathForMatch(node.Path)
		if p == "" {
			continue
		}
		if p == matchSuffix || strings.HasSuffix(p, matchSuffix) {
			matches = append(matches, p)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read snapshot path list: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("path with suffix %s was not found in snapshot %s", matchSuffix, snap)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("snapshot %s has ambiguous matches for suffix %s", snap, matchSuffix)
	}
	return filepath.Clean(matches[0]), nil
}

func findManifestSitePath(manifest *backupManifest, domain string) (string, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "", fmt.Errorf("site domain is required")
	}
	var matches []string
	for _, site := range manifest.Sites {
		if strings.EqualFold(strings.TrimSpace(site.Domain), domain) {
			p := normalizePathForMatch(site.RootPath)
			if p != "" {
				matches = append(matches, p)
			}
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("snapshot manifest has no site entry for domain %s", domain)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("snapshot manifest has multiple site entries for domain %s", domain)
	}
	return matches[0], nil
}

func findManifestDatabaseDumpPath(manifest *backupManifest, dbName string) (string, error) {
	dbName = strings.TrimSpace(dbName)
	if dbName == "" {
		return "", fmt.Errorf("database name is required")
	}
	var matches []string
	for _, dbDump := range manifest.Databases {
		if strings.EqualFold(strings.TrimSpace(dbDump.DBName), dbName) {
			p := normalizePathForMatch(dbDump.DumpPath)
			if p != "" {
				matches = append(matches, p)
			}
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("snapshot manifest has no database entry for %s", dbName)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("snapshot manifest has multiple database entries for %s", dbName)
	}
	return matches[0], nil
}

func loadSnapshotManifest(ctx context.Context, run func(context.Context, string) ([]byte, error), resticCmd, snapshotID string) (*backupManifest, error) {
	manifestPath, err := resolveSnapshotPathBySuffixUnique(ctx, run, resticCmd, snapshotID, filepath.Join(".fastcp", "backups", "manifest.json"), false)
	if err != nil {
		return nil, err
	}
	output, err := run(ctx, fmt.Sprintf("%s dump %s %s", resticCmd, shellQuote(strings.TrimSpace(snapshotID)), shellQuote(manifestPath)))
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot manifest: %w", err)
	}
	var manifest backupManifest
	if err := json.Unmarshal(output, &manifest); err != nil {
		return nil, fmt.Errorf("invalid snapshot manifest: %w", err)
	}
	if manifest.Version != 1 {
		return nil, fmt.Errorf("unsupported snapshot manifest version: %d", manifest.Version)
	}
	return &manifest, nil
}

func parseSnapshotIDFromBackupOutput(raw []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	fallbackID := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg resticBackupMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if strings.TrimSpace(msg.SnapshotID) == "" {
			continue
		}
		if msg.MessageType == "summary" {
			return strings.TrimSpace(msg.SnapshotID)
		}
		if fallbackID == "" {
			fallbackID = strings.TrimSpace(msg.SnapshotID)
		}
	}
	return fallbackID
}

func parseSnapshotsJSON(raw []byte) ([]BackupSnapshot, error) {
	clean := bytes.TrimSpace(raw)
	if len(clean) == 0 {
		return []BackupSnapshot{}, nil
	}
	var snapshots []BackupSnapshot
	if err := json.Unmarshal(clean, &snapshots); err == nil {
		return snapshots, nil
	}
	var envelope resticSnapshotsEnvelope
	if err := json.Unmarshal(clean, &envelope); err == nil && len(envelope.Snapshots) > 0 {
		return envelope.Snapshots, nil
	}
	start := bytes.IndexByte(clean, '[')
	end := bytes.LastIndexByte(clean, ']')
	if start >= 0 && end > start {
		if err := json.Unmarshal(clean[start:end+1], &snapshots); err == nil {
			return snapshots, nil
		}
	}
	scanner := bufio.NewScanner(bytes.NewReader(clean))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineSnapshots := make([]BackupSnapshot, 0, 32)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "[" || line == "]" {
			continue
		}
		line = strings.TrimSuffix(line, ",")
		var one BackupSnapshot
		if err := json.Unmarshal([]byte(line), &one); err == nil && strings.TrimSpace(one.ID) != "" {
			lineSnapshots = append(lineSnapshots, one)
			continue
		}
		var lineEnvelope resticSnapshotsEnvelope
		if err := json.Unmarshal([]byte(line), &lineEnvelope); err == nil && len(lineEnvelope.Snapshots) > 0 {
			lineSnapshots = append(lineSnapshots, lineEnvelope.Snapshots...)
		}
	}
	if len(lineSnapshots) > 0 {
		return lineSnapshots, nil
	}
	snippet := string(clean)
	if len(snippet) > 180 {
		snippet = snippet[:180] + "..."
	}
	return nil, fmt.Errorf("invalid snapshots JSON output: %q", snippet)
}

func sanitizeSnapshotIDForFile(v string) string {
	clean := strings.TrimSpace(v)
	if clean == "" {
		return "snapshot"
	}
	var out []rune
	for _, r := range clean {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "snapshot"
	}
	if len(out) > 16 {
		out = out[:16]
	}
	return string(out)
}

type zipExportEntry struct {
	SourcePath  string
	ArchivePath string
}

func sanitizeArchivePathSegment(v, fallback string) string {
	raw := strings.TrimSpace(v)
	if raw == "" {
		raw = strings.TrimSpace(fallback)
	}
	if raw == "" {
		raw = "item"
	}
	raw = strings.ReplaceAll(raw, "/", "-")
	raw = strings.ReplaceAll(raw, "\\", "-")
	var out []rune
	lastDash := false
	for _, r := range raw {
		isSafe := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if isSafe {
			out = append(out, r)
			lastDash = false
			continue
		}
		if !lastDash {
			out = append(out, '-')
			lastDash = true
		}
	}
	clean := strings.Trim(string(out), "-.")
	if clean == "" {
		return "item"
	}
	return clean
}

func zipMappedPaths(zipPath string, entries []zipExportEntry, deleteSourceFiles bool) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zw := zip.NewWriter(zipFile)
	defer zw.Close()

	for _, entry := range entries {
		src := filepath.Clean(strings.TrimSpace(entry.SourcePath))
		dst := filepath.ToSlash(strings.TrimSpace(entry.ArchivePath))
		dst = strings.Trim(dst, "/")
		if src == "" || src == "." || dst == "" {
			return fmt.Errorf("invalid zip export entry")
		}
		info, statErr := os.Lstat(src)
		if statErr != nil {
			return statErr
		}
		if info.IsDir() {
			if err := filepath.Walk(src, func(path string, walkInfo os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				rel, err := filepath.Rel(src, path)
				if err != nil {
					return err
				}
				zipName := dst
				if rel != "." {
					zipName = dst + "/" + filepath.ToSlash(rel)
				}
				header, err := zip.FileInfoHeader(walkInfo)
				if err != nil {
					return err
				}
				header.Name = zipName
				if walkInfo.IsDir() {
					if !strings.HasSuffix(header.Name, "/") {
						header.Name += "/"
					}
					_, err = zw.CreateHeader(header)
					return err
				}
				header.Method = zip.Deflate
				writer, err := zw.CreateHeader(header)
				if err != nil {
					return err
				}
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				_, err = io.Copy(writer, f)
				closeErr := f.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return closeErr
				}
				if deleteSourceFiles {
					if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
						return removeErr
					}
				}
				return nil
			}); err != nil {
				return err
			}
			if deleteSourceFiles {
				if removeErr := os.RemoveAll(src); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					return removeErr
				}
			}
			continue
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = dst
		header.Method = zip.Deflate
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		_, err = io.Copy(writer, f)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if deleteSourceFiles {
			if removeErr := os.Remove(src); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return removeErr
			}
		}
	}
	return nil
}

func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'"'"'`) + "'"
}

type limitedOutputBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	maxBytes  int
	truncated bool
}

func (b *limitedOutputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.maxBytes <= 0 {
		return len(p), nil
	}
	remaining := b.maxBytes - b.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.buf.Write(p[:remaining])
			b.truncated = true
			return len(p), nil
		}
		_, _ = b.buf.Write(p)
		return len(p), nil
	}
	b.truncated = true
	return len(p), nil
}

func (b *limitedOutputBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.buf.Bytes()
	if !b.truncated {
		return append([]byte(nil), out...)
	}
	notice := []byte("\n[output truncated]")
	res := make([]byte, 0, len(out)+len(notice))
	res = append(res, out...)
	res = append(res, notice...)
	return res
}

func (s *BackupService) prepareUserShellCommand(ctx context.Context, username string, env map[string]string, script string) (*exec.Cmd, error) {
	homeDir, uid, gid, err := s.userHome(username)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "bash", "-lc", "set -euo pipefail; "+script)
	cmd.Dir = homeDir
	cmd.Env = []string{
		"HOME=" + homeDir,
		"USER=" + username,
		"LOGNAME=" + username,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	if os.Geteuid() == 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: uint32(uid),
				Gid: uint32(gid),
			},
		}
	} else {
		current, curErr := osuser.Current()
		if curErr != nil || current.Username != username {
			return nil, fmt.Errorf("cannot switch to user %s without root privileges", username)
		}
	}
	return cmd, nil
}

func (s *BackupService) runAsUserShell(ctx context.Context, username string, env map[string]string, script string) ([]byte, error) {
	cmd, err := s.prepareUserShellCommand(ctx, username, env, script)
	if err != nil {
		return nil, err
	}
	return cmd.CombinedOutput()
}

func (s *BackupService) runAsUserShellLimited(ctx context.Context, username string, env map[string]string, script string, maxBytes int) ([]byte, error) {
	cmd, err := s.prepareUserShellCommand(ctx, username, env, script)
	if err != nil {
		return nil, err
	}
	out := &limitedOutputBuffer{maxBytes: maxBytes}
	cmd.Stdout = out
	cmd.Stderr = out
	err = cmd.Run()
	return out.Bytes(), err
}

func isResticLockError(runErr error, output []byte) bool {
	if runErr == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(string(output)))
	errText := strings.ToLower(strings.TrimSpace(runErr.Error()))
	return strings.Contains(text, "already locked") ||
		strings.Contains(text, "repository is already locked") ||
		strings.Contains(errText, "already locked")
}

func (s *BackupService) runWithResticLockRecovery(ctx context.Context, username string, env map[string]string, cfg *database.BackupConfig, script string) ([]byte, error) {
	output, err := s.runAsUserShell(ctx, username, env, script)
	if err == nil || !strings.Contains(script, "restic ") || !isResticLockError(err, output) {
		return output, err
	}

	// Try to clear stale locks, then retry once.
	resticCmd := "restic --retry-lock " + shellQuote(resticRetryLock)
	if cfg != nil {
		resticCmd = resticCommand(cfg)
	}
	unlockScript := resticCmd + " unlock"
	unlockOut, unlockErr := s.runAsUserShell(ctx, username, env, unlockScript)
	if unlockErr != nil {
		return output, fmt.Errorf("%w (stale-lock recovery failed: %s)", err, strings.TrimSpace(string(unlockOut)))
	}

	retryOut, retryErr := s.runAsUserShell(ctx, username, env, script)
	if retryErr != nil {
		return retryOut, retryErr
	}
	return retryOut, nil
}

func (s *BackupService) runWithResticLockRecoveryLimited(ctx context.Context, username string, env map[string]string, cfg *database.BackupConfig, script string, maxBytes int) ([]byte, error) {
	output, err := s.runAsUserShellLimited(ctx, username, env, script, maxBytes)
	if err == nil || !strings.Contains(script, "restic ") || !isResticLockError(err, output) {
		return output, err
	}
	resticCmd := "restic --retry-lock " + shellQuote(resticRetryLock)
	if cfg != nil {
		resticCmd = resticCommand(cfg)
	}
	unlockScript := resticCmd + " unlock"
	unlockOut, unlockErr := s.runAsUserShellLimited(ctx, username, env, unlockScript, maxBytes)
	if unlockErr != nil {
		return output, fmt.Errorf("%w (stale-lock recovery failed: %s)", err, strings.TrimSpace(string(unlockOut)))
	}
	retryOut, retryErr := s.runAsUserShellLimited(ctx, username, env, script, maxBytes)
	if retryErr != nil {
		return retryOut, retryErr
	}
	return retryOut, nil
}

func (s *BackupService) userHome(username string) (string, int, int, error) {
	u, err := osuser.Lookup(username)
	if err != nil {
		return "", 0, 0, fmt.Errorf("user not found: %w", err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return u.HomeDir, uid, gid, nil
}

func (s *BackupService) ensureDirOwned(path string, uid, gid int) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}
	return os.Chown(path, uid, gid)
}

func normalizeBackendType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "local", "sftp", "s3", "rclone":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return ""
	}
}

func normalizeAbsolutePath(v string) (string, error) {
	p := filepath.Clean(strings.TrimSpace(v))
	if p == "." || p == "" {
		return "", fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("path must be absolute")
	}
	return p, nil
}

func buildSFTPRepository(username, host string, port int, absPath string) (string, error) {
	if strings.TrimSpace(username) == "" {
		return "", fmt.Errorf("sftp username is required")
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("sftp host is required")
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("sftp port must be between 1 and 65535")
	}
	pathValue, err := normalizeAbsolutePath(absPath)
	if err != nil {
		return "", fmt.Errorf("sftp path: %w", err)
	}
	hostPort := net.JoinHostPort(host, strconv.Itoa(port))
	pathSuffix := strings.TrimPrefix(filepath.ToSlash(pathValue), "/")
	return fmt.Sprintf("sftp://%s@%s//%s", url.User(username).String(), hostPort, pathSuffix), nil
}

func buildS3Repository(endpoint, bucket, prefix string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("s3 endpoint is required")
	}
	endpoint = strings.TrimRight(endpoint, "/")
	bucket = strings.Trim(strings.TrimSpace(bucket), "/")
	if bucket == "" {
		return "", fmt.Errorf("s3 bucket is required")
	}
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	repo := endpoint + "/" + bucket
	if prefix != "" {
		repo += "/" + prefix
	}
	return "s3:" + repo, nil
}

func normalizeS3BucketLookup(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", defaultS3BucketLookup:
		return defaultS3BucketLookup, nil
	case "dns", "path":
		return strings.ToLower(strings.TrimSpace(v)), nil
	default:
		return "", fmt.Errorf("s3 bucket lookup must be one of: auto, dns, path")
	}
}

func resticCommand(cfg *database.BackupConfig) string {
	parts := []string{"restic", "--retry-lock", shellQuote(resticRetryLock)}
	if strings.EqualFold(cfg.BackendType, "s3") {
		if region := strings.TrimSpace(cfg.S3Region); region != "" {
			parts = append(parts, "-o", shellQuote("s3.region="+region))
		}
		lookup := strings.TrimSpace(cfg.S3BucketLookup)
		if lookup == "" {
			lookup = defaultS3BucketLookup
		}
		parts = append(parts, "-o", shellQuote("s3.bucket-lookup="+lookup))
		if cfg.S3ListObjectsV1 {
			parts = append(parts, "-o", shellQuote("s3.list-objects-v1=true"))
		}
	}
	return strings.Join(parts, " ")
}

func (s *BackupService) resticEnv(cfg *database.BackupConfig, homeDir, resticPassword string) (map[string]string, error) {
	env := map[string]string{
		"RESTIC_REPOSITORY": cfg.Repository,
		"RESTIC_PASSWORD":   resticPassword,
		"HOME":              homeDir,
	}
	if strings.EqualFold(cfg.BackendType, "s3") {
		if strings.TrimSpace(cfg.S3AccessKeyEnc) == "" || strings.TrimSpace(cfg.S3SecretKeyEnc) == "" {
			return nil, fmt.Errorf("s3 credentials are not configured")
		}
		accessKey, err := crypto.Decrypt(cfg.S3AccessKeyEnc)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt s3 access key")
		}
		secretKey, err := crypto.Decrypt(cfg.S3SecretKeyEnc)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt s3 secret key")
		}
		env["AWS_ACCESS_KEY_ID"] = accessKey
		env["AWS_SECRET_ACCESS_KEY"] = secretKey
		if strings.TrimSpace(cfg.S3SessionTokenEnc) != "" {
			token, err := crypto.Decrypt(cfg.S3SessionTokenEnc)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt s3 session token")
			}
			if strings.TrimSpace(token) != "" {
				env["AWS_SESSION_TOKEN"] = token
			}
		}
		if strings.TrimSpace(cfg.S3Region) != "" {
			env["AWS_DEFAULT_REGION"] = strings.TrimSpace(cfg.S3Region)
		}
	}
	return env, nil
}

func copyRcloneStatus(status *BackupRcloneStatus) *BackupRcloneStatus {
	if status == nil {
		return nil
	}
	cp := *status
	return &cp
}

func (s *BackupService) GetRcloneStatus(ctx context.Context) (*BackupRcloneStatus, error) {
	if s.agent == nil {
		return &BackupRcloneStatus{
			Status:    "failed",
			Installed: false,
			Message:   "agent client is not available",
		}, nil
	}
	agentStatus, err := s.agent.GetRcloneStatus(ctx)
	if err != nil {
		return nil, err
	}
	s.rcloneInstallMu.Lock()
	defer s.rcloneInstallMu.Unlock()
	if s.rcloneInstallState != nil && s.rcloneInstallState.Status == "installing" {
		current := copyRcloneStatus(s.rcloneInstallState)
		if agentStatus.Installed {
			current.Installed = true
			current.Version = agentStatus.Version
		}
		return current, nil
	}
	if agentStatus.Installed {
		state := &BackupRcloneStatus{
			Status:    "available",
			Installed: true,
			Version:   strings.TrimSpace(agentStatus.Version),
			Message:   "rclone is installed and ready.",
		}
		s.rcloneInstallState = state
		return copyRcloneStatus(state), nil
	}
	if s.rcloneInstallState != nil && s.rcloneInstallState.Status == "failed" {
		current := copyRcloneStatus(s.rcloneInstallState)
		current.Installed = false
		return current, nil
	}
	state := &BackupRcloneStatus{
		Status:    "missing",
		Installed: false,
		Message:   "rclone is not installed.",
	}
	s.rcloneInstallState = state
	return copyRcloneStatus(state), nil
}

func (s *BackupService) InstallRclone(ctx context.Context) (*BackupRcloneStatus, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	s.rcloneInstallMu.Lock()
	if s.rcloneInstallState != nil && s.rcloneInstallState.Status == "installing" {
		current := copyRcloneStatus(s.rcloneInstallState)
		s.rcloneInstallMu.Unlock()
		return current, nil
	}
	state := &BackupRcloneStatus{
		Status:    "installing",
		Installed: false,
		Message:   "Installing rclone...",
		StartedAt: now,
	}
	s.rcloneInstallState = state
	s.rcloneInstallMu.Unlock()

	go func(startedAt string) {
		jobCtx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()

		if s.agent == nil {
			finishedAt := time.Now().UTC().Format(time.RFC3339)
			s.rcloneInstallMu.Lock()
			s.rcloneInstallState = &BackupRcloneStatus{
				Status:     "failed",
				Installed:  false,
				Message:    "agent client is not available",
				StartedAt:  startedAt,
				FinishedAt: finishedAt,
			}
			s.rcloneInstallMu.Unlock()
			return
		}

		installedStatus, err := s.agent.InstallRclone(jobCtx)
		finishedAt := time.Now().UTC().Format(time.RFC3339)
		s.rcloneInstallMu.Lock()
		defer s.rcloneInstallMu.Unlock()
		if err != nil {
			s.rcloneInstallState = &BackupRcloneStatus{
				Status:     "failed",
				Installed:  false,
				Message:    err.Error(),
				StartedAt:  startedAt,
				FinishedAt: finishedAt,
			}
			return
		}
		s.rcloneInstallState = &BackupRcloneStatus{
			Status:     "available",
			Installed:  installedStatus != nil && installedStatus.Installed,
			Version:    strings.TrimSpace(installedStatus.Version),
			Message:    "rclone installed successfully.",
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
		}
	}(now)

	return copyRcloneStatus(state), nil
}

func (s *BackupService) ensureRcloneAvailable(ctx context.Context) error {
	status, err := s.GetRcloneStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to detect rclone status: %w", err)
	}
	if status.Installed {
		return nil
	}
	if status.Status == "installing" {
		return fmt.Errorf("rclone is currently installing; please wait")
	}
	if strings.TrimSpace(status.Message) != "" {
		return fmt.Errorf("rclone is not available: %s", status.Message)
	}
	return fmt.Errorf("rclone is not available")
}

func isResticRepositoryMissingOutput(output string) bool {
	text := strings.ToLower(strings.TrimSpace(output))
	if text == "" {
		return false
	}
	return strings.Contains(text, "is there a repository at the following location") ||
		strings.Contains(text, "config file does not exist") ||
		strings.Contains(text, "unable to open config file")
}

func (s *BackupService) testResolvedConfigConnection(ctx context.Context, username string, cfg *database.BackupConfig, passwordEnc string) (string, error) {
	if strings.TrimSpace(passwordEnc) == "" {
		return "", fmt.Errorf("repository password is required")
	}
	if strings.EqualFold(cfg.BackendType, "rclone") {
		if err := s.ensureRcloneAvailable(ctx); err != nil {
			return "", err
		}
	}
	password, err := crypto.Decrypt(passwordEnc)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt repository password")
	}
	homeDir, _, _, err := s.userHome(username)
	if err != nil {
		return "", err
	}
	env, err := s.resticEnv(cfg, homeDir, password)
	if err != nil {
		return "", err
	}
	resticCmd := resticCommand(cfg)
	output, runErr := s.runWithResticLockRecoveryLimited(
		ctx,
		username,
		env,
		cfg,
		fmt.Sprintf("%s cat config", resticCmd),
		maxShellOutputBytes,
	)
	if runErr == nil {
		return "Backup backend connection is valid.", nil
	}
	trimmed := strings.TrimSpace(string(output))
	if isResticRepositoryMissingOutput(trimmed) {
		return "Connection is valid, but the repository is not initialized yet (it will be initialized on first backup run).", nil
	}
	return "", fmt.Errorf("backup backend validation failed: %v: %s", runErr, trimmed)
}

func availableDiskBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func (s *BackupService) snapshotRestoreSizeBytes(ctx context.Context, username, snapshotID string, cfg *database.BackupConfig, env map[string]string) (uint64, error) {
	resticCmd := resticCommand(cfg)
	statsScript := fmt.Sprintf("%s stats --json --mode restore-size %s", resticCmd, shellQuote(strings.TrimSpace(snapshotID)))
	output, err := s.runWithResticLockRecoveryLimited(ctx, username, env, cfg, statsScript, maxShellOutputBytes)
	if err != nil {
		return 0, fmt.Errorf("failed to read snapshot restore size: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var stats resticStatsSummary
	if err := json.Unmarshal(output, &stats); err != nil {
		return 0, fmt.Errorf("failed to parse restic stats output: %w", err)
	}
	return stats.TotalSize, nil
}

func (s *BackupService) ensureDiskHeadroomForSnapshotExport(ctx context.Context, username, snapshotID, targetPath string, cfg *database.BackupConfig, env map[string]string) error {
	restoreSize, err := s.snapshotRestoreSizeBytes(ctx, username, snapshotID, cfg, env)
	if err != nil {
		return err
	}
	availableBytes, err := availableDiskBytes(targetPath)
	if err != nil {
		return fmt.Errorf("failed to read available disk space: %w", err)
	}
	requiredBytes := restoreSize + zipExportDiskHeadroom
	if requiredBytes < restoreSize {
		requiredBytes = ^uint64(0)
	}
	if availableBytes < requiredBytes {
		return fmt.Errorf("not enough free disk space for snapshot export: require at least %d bytes, available %d bytes", requiredBytes, availableBytes)
	}
	return nil
}

func (s *BackupService) getConfigRow(ctx context.Context, username string) (*database.BackupConfig, error) {
	var cfg database.BackupConfig
	var lastRun, nextRun, runningStart, lastPrune sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT username, repository, password_enc, COALESCE(backend_type, 'sftp'),
		COALESCE(sftp_username, ''), COALESCE(sftp_host, ''), COALESCE(sftp_port, 22), COALESCE(sftp_path, ''),
		COALESCE(s3_endpoint, ''), COALESCE(s3_bucket, ''), COALESCE(s3_prefix, ''), COALESCE(s3_region, ''),
		COALESCE(s3_bucket_lookup, 'auto'), COALESCE(s3_list_objects_v1, 0),
		COALESCE(s3_access_key_enc, ''), COALESCE(s3_secret_key_enc, ''), COALESCE(s3_session_token_enc, ''),
		enabled, schedule_cron,
		COALESCE(exclude_site_ids, '[]'), COALESCE(exclude_database_ids, '[]'),
		COALESCE(keep_last, 7), COALESCE(keep_daily, 7), COALESCE(keep_weekly, 4), COALESCE(keep_monthly, 6),
		last_run_at, next_run_at, COALESCE(last_status, 'idle'), COALESCE(last_message, ''), COALESCE(running_job_id, ''),
		running_started_at, last_prune_at, created_at, updated_at
		FROM backup_configs WHERE username = ?`, username).
		Scan(&cfg.Username, &cfg.Repository, &cfg.PasswordEnc, &cfg.BackendType,
			&cfg.SFTPUsername, &cfg.SFTPHost, &cfg.SFTPPort, &cfg.SFTPPath,
			&cfg.S3Endpoint, &cfg.S3Bucket, &cfg.S3Prefix, &cfg.S3Region,
			&cfg.S3BucketLookup, &cfg.S3ListObjectsV1,
			&cfg.S3AccessKeyEnc, &cfg.S3SecretKeyEnc, &cfg.S3SessionTokenEnc,
			&cfg.Enabled, &cfg.ScheduleCron,
			&cfg.ExcludeSiteIDs, &cfg.ExcludeDatabaseIDs,
			&cfg.KeepLast, &cfg.KeepDaily, &cfg.KeepWeekly, &cfg.KeepMonthly,
			&lastRun, &nextRun, &cfg.LastStatus, &cfg.LastMessage, &cfg.RunningJobID,
			&runningStart, &lastPrune, &cfg.CreatedAt, &cfg.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if lastRun.Valid {
		t := lastRun.Time.UTC()
		cfg.LastRunAt = &t
	}
	if nextRun.Valid {
		t := nextRun.Time.UTC()
		cfg.NextRunAt = &t
	}
	if runningStart.Valid {
		t := runningStart.Time.UTC()
		cfg.RunningStartedAt = &t
	}
	if lastPrune.Valid {
		t := lastPrune.Time.UTC()
		cfg.LastPruneAt = &t
	}
	return &cfg, nil
}

func (s *BackupService) toAPIConfig(ctx context.Context, cfg *database.BackupConfig, username string) (*BackupConfig, error) {
	catalog, err := s.GetCatalog(ctx, username)
	if err != nil {
		return nil, err
	}
	result := &BackupConfig{
		Username:           username,
		Repository:         cfg.Repository,
		HasPassword:        strings.TrimSpace(cfg.PasswordEnc) != "",
		BackendType:        cfg.BackendType,
		SFTPUsername:       cfg.SFTPUsername,
		SFTPHost:           cfg.SFTPHost,
		SFTPPort:           cfg.SFTPPort,
		SFTPPath:           cfg.SFTPPath,
		S3Endpoint:         cfg.S3Endpoint,
		S3Bucket:           cfg.S3Bucket,
		S3Prefix:           cfg.S3Prefix,
		S3Region:           cfg.S3Region,
		S3BucketLookup:     cfg.S3BucketLookup,
		S3ListObjectsV1:    cfg.S3ListObjectsV1,
		HasS3Credentials:   strings.TrimSpace(cfg.S3AccessKeyEnc) != "" && strings.TrimSpace(cfg.S3SecretKeyEnc) != "",
		HasS3SessionToken:  strings.TrimSpace(cfg.S3SessionTokenEnc) != "",
		Enabled:            cfg.Enabled,
		ScheduleCron:       cfg.ScheduleCron,
		ExcludeSiteIDs:     fromJSONStringSlice(cfg.ExcludeSiteIDs),
		ExcludeDatabaseIDs: fromJSONStringSlice(cfg.ExcludeDatabaseIDs),
		KeepLast:           cfg.KeepLast,
		KeepDaily:          cfg.KeepDaily,
		KeepWeekly:         cfg.KeepWeekly,
		KeepMonthly:        cfg.KeepMonthly,
		LastStatus:         cfg.LastStatus,
		LastMessage:        cfg.LastMessage,
		RunningJobID:       cfg.RunningJobID,
		Catalog:            catalog,
	}
	if cfg.LastRunAt != nil {
		ts := cfg.LastRunAt.UTC().Format(time.RFC3339)
		result.LastRunAt = &ts
	}
	if cfg.NextRunAt != nil {
		ts := cfg.NextRunAt.UTC().Format(time.RFC3339)
		result.NextRunAt = &ts
	}
	if cfg.RunningStartedAt != nil {
		ts := cfg.RunningStartedAt.UTC().Format(time.RFC3339)
		result.RunningStartedAt = &ts
	}
	return result, nil
}

func (s *BackupService) defaultConfig(username string) *database.BackupConfig {
	now := time.Now().UTC()
	return &database.BackupConfig{
		Username:           username,
		Repository:         "",
		PasswordEnc:        "",
		BackendType:        "sftp",
		SFTPUsername:       username,
		SFTPHost:           "",
		SFTPPort:           defaultSFTPPort,
		SFTPPath:           "",
		S3BucketLookup:     defaultS3BucketLookup,
		Enabled:            false,
		ScheduleCron:       defaultBackupSchedule,
		ExcludeSiteIDs:     "[]",
		ExcludeDatabaseIDs: "[]",
		KeepLast:           7,
		KeepDaily:          7,
		KeepWeekly:         4,
		KeepMonthly:        6,
		LastStatus:         "idle",
		LastMessage:        "",
		RunningJobID:       "",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func (s *BackupService) GetConfig(ctx context.Context, username string) (*BackupConfig, error) {
	cfg, err := s.getConfigRow(ctx, username)
	if err == sql.ErrNoRows {
		return s.toAPIConfig(ctx, s.defaultConfig(username), username)
	}
	if err != nil {
		return nil, err
	}
	return s.toAPIConfig(ctx, cfg, username)
}

func (s *BackupService) SaveConfig(ctx context.Context, username string, req *SaveBackupConfigRequest) (*BackupConfig, error) {
	schedule := strings.TrimSpace(req.ScheduleCron)
	if schedule == "" {
		schedule = defaultBackupSchedule
	}
	if _, err := parseCronSchedule(schedule); err != nil {
		return nil, fmt.Errorf("invalid backup schedule: %w", err)
	}
	if req.KeepLast < 0 || req.KeepDaily < 0 || req.KeepWeekly < 0 || req.KeepMonthly < 0 {
		return nil, fmt.Errorf("retention values must be zero or greater")
	}

	existing, err := s.getConfigRow(ctx, username)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	passwordEnc := ""
	backendType := normalizeBackendType(req.BackendType)
	if backendType == "" {
		if existing != nil {
			backendType = normalizeBackendType(existing.BackendType)
		}
		if backendType == "" {
			backendType = "sftp"
		}
	}
	repo := strings.TrimSpace(req.Repository)
	sftpUsername := strings.TrimSpace(req.SFTPUsername)
	sftpHost := strings.TrimSpace(req.SFTPHost)
	sftpPort := req.SFTPPort
	sftpPath := strings.TrimSpace(req.SFTPPath)
	s3Endpoint := strings.TrimSpace(req.S3Endpoint)
	s3Bucket := strings.TrimSpace(req.S3Bucket)
	s3Prefix := strings.TrimSpace(req.S3Prefix)
	s3Region := strings.TrimSpace(req.S3Region)
	s3BucketLookup := strings.TrimSpace(req.S3BucketLookup)
	s3ListObjectsV1 := req.S3ListObjectsV1
	s3AccessKeyEnc := ""
	s3SecretKeyEnc := ""
	s3SessionTokenEnc := ""
	if existing != nil {
		passwordEnc = existing.PasswordEnc
		if repo == "" {
			repo = existing.Repository
		}
		if sftpUsername == "" {
			sftpUsername = strings.TrimSpace(existing.SFTPUsername)
		}
		if sftpHost == "" {
			sftpHost = strings.TrimSpace(existing.SFTPHost)
		}
		if sftpPort == 0 {
			sftpPort = existing.SFTPPort
		}
		if sftpPath == "" {
			sftpPath = strings.TrimSpace(existing.SFTPPath)
		}
		if s3Endpoint == "" {
			s3Endpoint = strings.TrimSpace(existing.S3Endpoint)
		}
		if s3Bucket == "" {
			s3Bucket = strings.TrimSpace(existing.S3Bucket)
		}
		if s3Prefix == "" {
			s3Prefix = strings.TrimSpace(existing.S3Prefix)
		}
		if s3Region == "" {
			s3Region = strings.TrimSpace(existing.S3Region)
		}
		if s3BucketLookup == "" {
			s3BucketLookup = strings.TrimSpace(existing.S3BucketLookup)
		}
		s3AccessKeyEnc = existing.S3AccessKeyEnc
		s3SecretKeyEnc = existing.S3SecretKeyEnc
		s3SessionTokenEnc = existing.S3SessionTokenEnc
	}
	if sftpPort == 0 {
		sftpPort = defaultSFTPPort
	}
	normalizedLookup, lookupErr := normalizeS3BucketLookup(s3BucketLookup)
	if lookupErr != nil {
		return nil, lookupErr
	}
	s3BucketLookup = normalizedLookup
	if strings.TrimSpace(req.RepositoryPassword) != "" {
		enc, encErr := crypto.Encrypt(req.RepositoryPassword)
		if encErr != nil {
			return nil, fmt.Errorf("failed to encrypt repository password: %w", encErr)
		}
		passwordEnc = enc
	}
	switch backendType {
	case "local":
		localPath, pathErr := normalizeAbsolutePath(repo)
		if pathErr != nil {
			return nil, fmt.Errorf("local backup path is required and must be absolute")
		}
		repo = localPath
		sftpUsername, sftpHost, sftpPath = "", "", ""
		sftpPort = defaultSFTPPort
		s3Endpoint, s3Bucket, s3Prefix, s3Region = "", "", "", ""
		s3BucketLookup = defaultS3BucketLookup
		s3ListObjectsV1 = false
	case "sftp":
		if sftpUsername == "" {
			sftpUsername = username
		}
		repoSFTP, repoErr := buildSFTPRepository(sftpUsername, sftpHost, sftpPort, sftpPath)
		if repoErr != nil {
			return nil, repoErr
		}
		repo = repoSFTP
		s3Endpoint, s3Bucket, s3Prefix, s3Region = "", "", "", ""
		s3BucketLookup = defaultS3BucketLookup
		s3ListObjectsV1 = false
	case "s3":
		repoS3, repoErr := buildS3Repository(s3Endpoint, s3Bucket, s3Prefix)
		if repoErr != nil {
			return nil, repoErr
		}
		repo = repoS3
		if strings.TrimSpace(req.S3AccessKeyID) != "" {
			enc, encErr := crypto.Encrypt(req.S3AccessKeyID)
			if encErr != nil {
				return nil, fmt.Errorf("failed to encrypt s3 access key: %w", encErr)
			}
			s3AccessKeyEnc = enc
		}
		if strings.TrimSpace(req.S3SecretAccessKey) != "" {
			enc, encErr := crypto.Encrypt(req.S3SecretAccessKey)
			if encErr != nil {
				return nil, fmt.Errorf("failed to encrypt s3 secret key: %w", encErr)
			}
			s3SecretKeyEnc = enc
		}
		if strings.TrimSpace(req.S3SessionToken) != "" {
			enc, encErr := crypto.Encrypt(req.S3SessionToken)
			if encErr != nil {
				return nil, fmt.Errorf("failed to encrypt s3 session token: %w", encErr)
			}
			s3SessionTokenEnc = enc
		}
		if strings.TrimSpace(s3AccessKeyEnc) == "" || strings.TrimSpace(s3SecretKeyEnc) == "" {
			return nil, fmt.Errorf("s3 access key and secret key are required")
		}
		sftpUsername, sftpHost, sftpPath = "", "", ""
		sftpPort = defaultSFTPPort
	case "rclone":
		repo = strings.TrimSpace(repo)
		if repo == "" {
			return nil, fmt.Errorf("rclone repository is required")
		}
		if !strings.HasPrefix(strings.ToLower(repo), "rclone:") {
			return nil, fmt.Errorf("rclone repository must start with rclone:")
		}
		sftpUsername, sftpHost, sftpPath = "", "", ""
		sftpPort = defaultSFTPPort
		s3Endpoint, s3Bucket, s3Prefix, s3Region = "", "", "", ""
		s3BucketLookup = defaultS3BucketLookup
		s3ListObjectsV1 = false
	default:
		return nil, fmt.Errorf("unsupported backup backend type")
	}
	if repo == "" && req.Enabled {
		return nil, fmt.Errorf("repository is required when backups are enabled")
	}
	if strings.TrimSpace(passwordEnc) == "" && req.Enabled {
		return nil, fmt.Errorf("repository password is required when backups are enabled")
	}
	if strings.TrimSpace(repo) != "" && strings.TrimSpace(passwordEnc) != "" && (req.Enabled || backendType == "rclone") {
		testCfg := &database.BackupConfig{
			BackendType:       backendType,
			Repository:        repo,
			S3Region:          s3Region,
			S3BucketLookup:    s3BucketLookup,
			S3ListObjectsV1:   s3ListObjectsV1,
			S3AccessKeyEnc:    s3AccessKeyEnc,
			S3SecretKeyEnc:    s3SecretKeyEnc,
			S3SessionTokenEnc: s3SessionTokenEnc,
		}
		if _, testErr := s.testResolvedConfigConnection(ctx, username, testCfg, passwordEnc); testErr != nil {
			return nil, testErr
		}
	}
	now := time.Now().UTC()
	var nextRun sql.NullTime
	if req.Enabled {
		next, nextErr := nextRunAt(schedule, now)
		if nextErr != nil {
			return nil, fmt.Errorf("invalid schedule: %w", nextErr)
		}
		nextRun = sql.NullTime{Time: next.UTC(), Valid: true}
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO backup_configs (
		username, repository, password_enc, backend_type,
		sftp_username, sftp_host, sftp_port, sftp_path,
		s3_endpoint, s3_bucket, s3_prefix, s3_region, s3_bucket_lookup, s3_list_objects_v1,
		s3_access_key_enc, s3_secret_key_enc, s3_session_token_enc,
		enabled, schedule_cron, exclude_site_ids, exclude_database_ids,
		keep_last, keep_daily, keep_weekly, keep_monthly, next_run_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(username) DO UPDATE SET
		repository=excluded.repository,
		password_enc=excluded.password_enc,
		backend_type=excluded.backend_type,
		sftp_username=excluded.sftp_username,
		sftp_host=excluded.sftp_host,
		sftp_port=excluded.sftp_port,
		sftp_path=excluded.sftp_path,
		s3_endpoint=excluded.s3_endpoint,
		s3_bucket=excluded.s3_bucket,
		s3_prefix=excluded.s3_prefix,
		s3_region=excluded.s3_region,
		s3_bucket_lookup=excluded.s3_bucket_lookup,
		s3_list_objects_v1=excluded.s3_list_objects_v1,
		s3_access_key_enc=excluded.s3_access_key_enc,
		s3_secret_key_enc=excluded.s3_secret_key_enc,
		s3_session_token_enc=excluded.s3_session_token_enc,
		enabled=excluded.enabled,
		schedule_cron=excluded.schedule_cron,
		exclude_site_ids=excluded.exclude_site_ids,
		exclude_database_ids=excluded.exclude_database_ids,
		keep_last=excluded.keep_last,
		keep_daily=excluded.keep_daily,
		keep_weekly=excluded.keep_weekly,
		keep_monthly=excluded.keep_monthly,
		next_run_at=excluded.next_run_at,
		updated_at=excluded.updated_at`,
		username,
		repo,
		passwordEnc,
		backendType,
		sftpUsername,
		sftpHost,
		sftpPort,
		sftpPath,
		s3Endpoint,
		s3Bucket,
		s3Prefix,
		s3Region,
		s3BucketLookup,
		s3ListObjectsV1,
		s3AccessKeyEnc,
		s3SecretKeyEnc,
		s3SessionTokenEnc,
		req.Enabled,
		schedule,
		toJSONStringSlice(req.ExcludeSiteIDs),
		toJSONStringSlice(req.ExcludeDatabaseIDs),
		req.KeepLast,
		req.KeepDaily,
		req.KeepWeekly,
		req.KeepMonthly,
		nextRun,
		now,
	)
	if err != nil {
		return nil, err
	}
	cfg, err := s.getConfigRow(ctx, username)
	if err != nil {
		return nil, err
	}
	return s.toAPIConfig(ctx, cfg, username)
}

func (s *BackupService) TestConfig(ctx context.Context, username string, req *SaveBackupConfigRequest) (*BackupConfigTestResponse, error) {
	backendType := normalizeBackendType(req.BackendType)
	existing, err := s.getConfigRow(ctx, username)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if backendType == "" {
		if existing != nil {
			backendType = normalizeBackendType(existing.BackendType)
		}
		if backendType == "" {
			backendType = "sftp"
		}
	}
	repo := strings.TrimSpace(req.Repository)
	sftpUsername := strings.TrimSpace(req.SFTPUsername)
	sftpHost := strings.TrimSpace(req.SFTPHost)
	sftpPort := req.SFTPPort
	sftpPath := strings.TrimSpace(req.SFTPPath)
	s3Endpoint := strings.TrimSpace(req.S3Endpoint)
	s3Bucket := strings.TrimSpace(req.S3Bucket)
	s3Prefix := strings.TrimSpace(req.S3Prefix)
	s3Region := strings.TrimSpace(req.S3Region)
	s3BucketLookup := strings.TrimSpace(req.S3BucketLookup)
	s3ListObjectsV1 := req.S3ListObjectsV1
	passwordEnc := ""
	s3AccessKeyEnc := ""
	s3SecretKeyEnc := ""
	s3SessionTokenEnc := ""
	if existing != nil {
		if repo == "" {
			repo = strings.TrimSpace(existing.Repository)
		}
		if sftpUsername == "" {
			sftpUsername = strings.TrimSpace(existing.SFTPUsername)
		}
		if sftpHost == "" {
			sftpHost = strings.TrimSpace(existing.SFTPHost)
		}
		if sftpPort == 0 {
			sftpPort = existing.SFTPPort
		}
		if sftpPath == "" {
			sftpPath = strings.TrimSpace(existing.SFTPPath)
		}
		if s3Endpoint == "" {
			s3Endpoint = strings.TrimSpace(existing.S3Endpoint)
		}
		if s3Bucket == "" {
			s3Bucket = strings.TrimSpace(existing.S3Bucket)
		}
		if s3Prefix == "" {
			s3Prefix = strings.TrimSpace(existing.S3Prefix)
		}
		if s3Region == "" {
			s3Region = strings.TrimSpace(existing.S3Region)
		}
		if s3BucketLookup == "" {
			s3BucketLookup = strings.TrimSpace(existing.S3BucketLookup)
		}
		passwordEnc = existing.PasswordEnc
		s3AccessKeyEnc = existing.S3AccessKeyEnc
		s3SecretKeyEnc = existing.S3SecretKeyEnc
		s3SessionTokenEnc = existing.S3SessionTokenEnc
	}
	if sftpPort == 0 {
		sftpPort = defaultSFTPPort
	}
	lookup, lookupErr := normalizeS3BucketLookup(s3BucketLookup)
	if lookupErr != nil {
		return nil, lookupErr
	}
	s3BucketLookup = lookup
	if strings.TrimSpace(req.RepositoryPassword) != "" {
		enc, encErr := crypto.Encrypt(req.RepositoryPassword)
		if encErr != nil {
			return nil, fmt.Errorf("failed to encrypt repository password: %w", encErr)
		}
		passwordEnc = enc
	}
	switch backendType {
	case "local":
		localPath, pathErr := normalizeAbsolutePath(repo)
		if pathErr != nil {
			return nil, fmt.Errorf("local backup path is required and must be absolute")
		}
		repo = localPath
	case "sftp":
		if sftpUsername == "" {
			sftpUsername = username
		}
		repoSFTP, repoErr := buildSFTPRepository(sftpUsername, sftpHost, sftpPort, sftpPath)
		if repoErr != nil {
			return nil, repoErr
		}
		repo = repoSFTP
	case "s3":
		repoS3, repoErr := buildS3Repository(s3Endpoint, s3Bucket, s3Prefix)
		if repoErr != nil {
			return nil, repoErr
		}
		repo = repoS3
		if strings.TrimSpace(req.S3AccessKeyID) != "" {
			enc, encErr := crypto.Encrypt(req.S3AccessKeyID)
			if encErr != nil {
				return nil, fmt.Errorf("failed to encrypt s3 access key: %w", encErr)
			}
			s3AccessKeyEnc = enc
		}
		if strings.TrimSpace(req.S3SecretAccessKey) != "" {
			enc, encErr := crypto.Encrypt(req.S3SecretAccessKey)
			if encErr != nil {
				return nil, fmt.Errorf("failed to encrypt s3 secret key: %w", encErr)
			}
			s3SecretKeyEnc = enc
		}
		if strings.TrimSpace(req.S3SessionToken) != "" {
			enc, encErr := crypto.Encrypt(req.S3SessionToken)
			if encErr != nil {
				return nil, fmt.Errorf("failed to encrypt s3 session token: %w", encErr)
			}
			s3SessionTokenEnc = enc
		}
		if strings.TrimSpace(s3AccessKeyEnc) == "" || strings.TrimSpace(s3SecretKeyEnc) == "" {
			return nil, fmt.Errorf("s3 access key and secret key are required")
		}
	case "rclone":
		repo = strings.TrimSpace(repo)
		if repo == "" {
			return nil, fmt.Errorf("rclone repository is required")
		}
		if !strings.HasPrefix(strings.ToLower(repo), "rclone:") {
			return nil, fmt.Errorf("rclone repository must start with rclone:")
		}
	default:
		return nil, fmt.Errorf("unsupported backup backend type")
	}
	if strings.TrimSpace(repo) == "" {
		return nil, fmt.Errorf("repository is required")
	}
	if strings.TrimSpace(passwordEnc) == "" {
		return nil, fmt.Errorf("repository password is required")
	}
	testCfg := &database.BackupConfig{
		BackendType:       backendType,
		Repository:        repo,
		S3Region:          s3Region,
		S3BucketLookup:    s3BucketLookup,
		S3ListObjectsV1:   s3ListObjectsV1,
		S3AccessKeyEnc:    s3AccessKeyEnc,
		S3SecretKeyEnc:    s3SecretKeyEnc,
		S3SessionTokenEnc: s3SessionTokenEnc,
	}
	message, testErr := s.testResolvedConfigConnection(ctx, username, testCfg, passwordEnc)
	if testErr != nil {
		return nil, testErr
	}
	return &BackupConfigTestResponse{
		Status:  "success",
		Message: message,
	}, nil
}

func (s *BackupService) GetCatalog(ctx context.Context, username string) (*BackupCatalog, error) {
	sites, err := s.db.ListSites(ctx, username)
	if err != nil {
		return nil, err
	}
	databasesList, err := s.db.ListDatabases(ctx, username)
	if err != nil {
		return nil, err
	}
	catalog := &BackupCatalog{
		Sites:     make([]BackupCatalogSite, 0, len(sites)),
		Databases: make([]BackupCatalogDatabase, 0, len(databasesList)),
	}
	for _, site := range sites {
		catalog.Sites = append(catalog.Sites, BackupCatalogSite{
			ID:     site.ID,
			Domain: site.Domain,
			Path:   filepath.Dir(site.DocumentRoot),
		})
	}
	for _, dbItem := range databasesList {
		catalog.Databases = append(catalog.Databases, BackupCatalogDatabase{
			ID:     dbItem.ID,
			DBName: dbItem.DBName,
		})
	}
	sort.Slice(catalog.Sites, func(i, j int) bool { return catalog.Sites[i].Domain < catalog.Sites[j].Domain })
	sort.Slice(catalog.Databases, func(i, j int) bool { return catalog.Databases[i].DBName < catalog.Databases[j].DBName })
	return catalog, nil
}

func (s *BackupService) runDueBackups(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT username FROM backup_configs
		WHERE enabled = 1
		  AND repository <> ''
		  AND next_run_at IS NOT NULL
		  AND datetime(next_run_at) <= CURRENT_TIMESTAMP
		ORDER BY next_run_at ASC
		LIMIT 20`)
	if err != nil {
		slog.Warn("backup scheduler query failed", "error", err)
		return
	}
	defer rows.Close()
	var usernames []string
	for rows.Next() {
		var username string
		if scanErr := rows.Scan(&username); scanErr == nil {
			usernames = append(usernames, username)
		}
	}
	for _, username := range usernames {
		if _, triggerErr := s.triggerBackup(ctx, username, "scheduled", false); triggerErr != nil {
			slog.Warn("scheduled backup trigger failed", "username", username, "error", triggerErr)
		}
	}
}

func (s *BackupService) claimBackupRun(ctx context.Context, username, jobType string, allowDisabled bool) (*database.BackupConfig, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()

	var cfg database.BackupConfig
	var lastRun, nextRun, runningStart, lastPrune sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT username, repository, password_enc, COALESCE(backend_type, 'sftp'),
		COALESCE(sftp_username, ''), COALESCE(sftp_host, ''), COALESCE(sftp_port, 22), COALESCE(sftp_path, ''),
		COALESCE(s3_endpoint, ''), COALESCE(s3_bucket, ''), COALESCE(s3_prefix, ''), COALESCE(s3_region, ''),
		COALESCE(s3_bucket_lookup, 'auto'), COALESCE(s3_list_objects_v1, 0),
		COALESCE(s3_access_key_enc, ''), COALESCE(s3_secret_key_enc, ''), COALESCE(s3_session_token_enc, ''),
		enabled, schedule_cron,
		COALESCE(exclude_site_ids, '[]'), COALESCE(exclude_database_ids, '[]'),
		COALESCE(keep_last, 7), COALESCE(keep_daily, 7), COALESCE(keep_weekly, 4), COALESCE(keep_monthly, 6),
		last_run_at, next_run_at, COALESCE(last_status, 'idle'), COALESCE(last_message, ''), COALESCE(running_job_id, ''),
		running_started_at, last_prune_at, created_at, updated_at
		FROM backup_configs WHERE username = ?`, username).
		Scan(&cfg.Username, &cfg.Repository, &cfg.PasswordEnc, &cfg.BackendType,
			&cfg.SFTPUsername, &cfg.SFTPHost, &cfg.SFTPPort, &cfg.SFTPPath,
			&cfg.S3Endpoint, &cfg.S3Bucket, &cfg.S3Prefix, &cfg.S3Region,
			&cfg.S3BucketLookup, &cfg.S3ListObjectsV1,
			&cfg.S3AccessKeyEnc, &cfg.S3SecretKeyEnc, &cfg.S3SessionTokenEnc,
			&cfg.Enabled, &cfg.ScheduleCron,
			&cfg.ExcludeSiteIDs, &cfg.ExcludeDatabaseIDs,
			&cfg.KeepLast, &cfg.KeepDaily, &cfg.KeepWeekly, &cfg.KeepMonthly,
			&lastRun, &nextRun, &cfg.LastStatus, &cfg.LastMessage, &cfg.RunningJobID,
			&runningStart, &lastPrune, &cfg.CreatedAt, &cfg.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, "", fmt.Errorf("backup repository is not configured")
	}
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(cfg.Repository) == "" {
		return nil, "", fmt.Errorf("backup repository is not configured")
	}
	if strings.TrimSpace(cfg.PasswordEnc) == "" {
		return nil, "", fmt.Errorf("backup repository password is not configured")
	}
	if !allowDisabled && !cfg.Enabled {
		return nil, "", fmt.Errorf("backups are disabled")
	}

	now := time.Now().UTC()
	if cfg.RunningJobID != "" {
		if runningStart.Valid && runningStart.Time.After(now.Add(-backupRunStaleAfter)) {
			return nil, "", fmt.Errorf("backup already running")
		}
	}
	jobID := uuid.New().String()
	_, err = tx.ExecContext(ctx, `INSERT INTO backup_jobs (id, username, job_type, status, message, started_at)
		VALUES (?, ?, ?, 'running', 'Backup started.', ?)`,
		jobID, username, jobType, now)
	if err != nil {
		return nil, "", err
	}
	_, err = tx.ExecContext(ctx, `UPDATE backup_configs
		SET running_job_id = ?, running_started_at = ?, last_status = 'running', last_message = 'Backup started.',
		    updated_at = ?
		WHERE username = ?`,
		jobID, now, now, username)
	if err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	if lastRun.Valid {
		t := lastRun.Time.UTC()
		cfg.LastRunAt = &t
	}
	if nextRun.Valid {
		t := nextRun.Time.UTC()
		cfg.NextRunAt = &t
	}
	if runningStart.Valid {
		t := runningStart.Time.UTC()
		cfg.RunningStartedAt = &t
	}
	if lastPrune.Valid {
		t := lastPrune.Time.UTC()
		cfg.LastPruneAt = &t
	}
	return &cfg, jobID, nil
}

func (s *BackupService) finishBackupRun(ctx context.Context, username, jobID, status, snapshotID, message string) {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		slog.Warn("backup finalize begin tx failed", "username", username, "error", err)
		return
	}
	defer tx.Rollback()

	_, _ = tx.ExecContext(ctx, `UPDATE backup_jobs
		SET status = ?, snapshot_id = ?, message = ?, finished_at = ?
		WHERE id = ?`, status, snapshotID, message, now, jobID)

	var enabled bool
	var schedule string
	if err := tx.QueryRowContext(ctx, "SELECT enabled, schedule_cron FROM backup_configs WHERE username = ?", username).Scan(&enabled, &schedule); err != nil {
		schedule = defaultBackupSchedule
	}
	var nextRun sql.NullTime
	if enabled {
		if next, nextErr := nextRunAt(schedule, now); nextErr == nil {
			nextRun = sql.NullTime{Time: next.UTC(), Valid: true}
		}
	}
	_, _ = tx.ExecContext(ctx, `UPDATE backup_configs
		SET running_job_id = '', running_started_at = NULL,
			last_status = ?, last_message = ?, last_run_at = ?, next_run_at = ?, updated_at = ?
		WHERE username = ? AND running_job_id = ?`,
		status, message, now, nextRun, now, username, jobID)
	_ = tx.Commit()
}

func (s *BackupService) runBackup(username string, cfg *database.BackupConfig) (string, string, error) {
	if _, err := exec.LookPath("restic"); err != nil {
		return "", "", fmt.Errorf("restic is not installed on this server")
	}
	resticPassword, err := crypto.Decrypt(cfg.PasswordEnc)
	if err != nil {
		return "", "", fmt.Errorf("failed to decrypt restic password")
	}
	homeDir, uid, gid, err := s.userHome(username)
	if err != nil {
		return "", "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()

	backupRoot := filepath.Join(homeDir, ".fastcp", "backups")
	dumpDir := filepath.Join(backupRoot, "mysql-dumps")
	manifestPath := filepath.Join(backupRoot, "manifest.json")
	if err := s.ensureDirOwned(backupRoot, uid, gid); err != nil {
		return "", "", err
	}
	if err := s.ensureDirOwned(dumpDir, uid, gid); err != nil {
		return "", "", err
	}
	if output, cleanErr := s.runAsUserShell(ctx, username, nil,
		fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 -type f -name '*.sql.gz' -delete", shellQuote(dumpDir))); cleanErr != nil {
		return "", "", fmt.Errorf("failed to clean old database dumps: %w: %s", cleanErr, strings.TrimSpace(string(output)))
	}

	excludedSites := make(map[string]struct{})
	for _, id := range fromJSONStringSlice(cfg.ExcludeSiteIDs) {
		excludedSites[id] = struct{}{}
	}
	excludedDatabases := make(map[string]struct{})
	for _, id := range fromJSONStringSlice(cfg.ExcludeDatabaseIDs) {
		excludedDatabases[id] = struct{}{}
	}

	sites, err := s.db.ListSites(ctx, username)
	if err != nil {
		return "", "", fmt.Errorf("failed to load sites: %w", err)
	}
	dbs, err := s.db.ListDatabases(ctx, username)
	if err != nil {
		return "", "", fmt.Errorf("failed to load databases: %w", err)
	}

	siteRoots := make([]string, 0, len(sites))
	manifestSites := make([]backupManifestSite, 0, len(sites))
	includedSiteIDs := make([]string, 0, len(sites))
	includedSiteDomains := make([]string, 0, len(sites))
	seenRoots := map[string]struct{}{}
	for _, site := range sites {
		if _, skip := excludedSites[site.ID]; skip {
			continue
		}
		root := filepath.Clean(filepath.Dir(site.DocumentRoot))
		if !strings.HasPrefix(root, filepath.Join(homeDir, "apps")+string(os.PathSeparator)) {
			continue
		}
		if _, ok := seenRoots[root]; ok {
			continue
		}
		seenRoots[root] = struct{}{}
		siteRoots = append(siteRoots, root)
		manifestSites = append(manifestSites, backupManifestSite{
			Domain:   site.Domain,
			RootPath: root,
		})
		includedSiteIDs = append(includedSiteIDs, site.ID)
		includedSiteDomains = append(includedSiteDomains, strings.ToLower(strings.TrimSpace(site.Domain)))
	}

	dumpCount := 0
	manifestDBDumps := make([]backupManifestDBDump, 0, len(dbs))
	includedDatabaseIDs := make([]string, 0, len(dbs))
	includedDatabaseNames := make([]string, 0, len(dbs))
	for _, dbRec := range dbs {
		if _, skip := excludedDatabases[dbRec.ID]; skip {
			continue
		}
		if strings.TrimSpace(dbRec.DBPassword) == "" {
			continue
		}
		dbPass, decErr := crypto.Decrypt(dbRec.DBPassword)
		if decErr != nil {
			continue
		}
		dumpPath := filepath.Join(dumpDir, dbRec.DBName+".sql.gz")
		script := fmt.Sprintf("mysqldump --single-transaction --quick --lock-tables=false -h 127.0.0.1 -u %s %s | gzip -c > %s",
			shellQuote(dbRec.DBUser),
			shellQuote(dbRec.DBName),
			shellQuote(dumpPath),
		)
		dumpEnv := map[string]string{
			"MYSQL_PWD": dbPass,
		}
		if output, dumpErr := s.runAsUserShell(ctx, username, dumpEnv, script); dumpErr != nil {
			return "", "", fmt.Errorf("failed to dump database %s: %w: %s", dbRec.DBName, dumpErr, strings.TrimSpace(string(output)))
		}
		dumpCount++
		manifestDBDumps = append(manifestDBDumps, backupManifestDBDump{
			DBName:   dbRec.DBName,
			DumpPath: dumpPath,
		})
		includedDatabaseIDs = append(includedDatabaseIDs, dbRec.ID)
		includedDatabaseNames = append(includedDatabaseNames, strings.ToLower(strings.TrimSpace(dbRec.DBName)))
	}
	if len(siteRoots) == 0 && dumpCount == 0 {
		return "", "", fmt.Errorf("nothing to back up (all sites/databases are excluded)")
	}
	manifest := backupManifest{
		Version:   1,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Username:  username,
		Sites:     manifestSites,
		Databases: manifestDBDumps,
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return "", "", fmt.Errorf("failed to build backup manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, manifestRaw, 0600); err != nil {
		return "", "", fmt.Errorf("failed to write backup manifest: %w", err)
	}
	if err := os.Chown(manifestPath, uid, gid); err != nil {
		return "", "", fmt.Errorf("failed to set backup manifest ownership: %w", err)
	}

	paths := make([]string, 0, len(siteRoots)+2)
	paths = append(paths, siteRoots...)
	paths = append(paths, manifestPath)
	if dumpCount > 0 {
		paths = append(paths, dumpDir)
	}

	env, envErr := s.resticEnv(cfg, homeDir, resticPassword)
	if envErr != nil {
		return "", "", envErr
	}
	resticCmd := resticCommand(cfg)
	initScript := fmt.Sprintf("if ! %s cat config >/dev/null 2>&1; then %s init >/dev/null; fi", resticCmd, resticCmd)
	if output, initErr := s.runWithResticLockRecovery(ctx, username, env, cfg, initScript); initErr != nil {
		return "", "", fmt.Errorf("failed to initialize restic repository: %w: %s", initErr, strings.TrimSpace(string(output)))
	}

	var quotedPaths []string
	for _, p := range paths {
		quotedPaths = append(quotedPaths, shellQuote(p))
	}
	// Exclude only root-level runtime directories for each website.
	// This intentionally does not exclude nested dirs like public/temp or plugin temp/log folders.
	excludeArgs := make([]string, 0, len(siteRoots)*2)
	for _, root := range siteRoots {
		excludeArgs = append(excludeArgs, "--exclude "+shellQuote(filepath.Join(root, "temp")))
		excludeArgs = append(excludeArgs, "--exclude "+shellQuote(filepath.Join(root, "logs")))
	}
	tagArgs := []string{
		"--tag " + shellQuote("fastcp"),
		"--tag " + shellQuote("user:"+username),
	}
	for _, siteID := range includedSiteIDs {
		tagArgs = append(tagArgs, "--tag "+shellQuote("site:"+siteID))
	}
	for _, siteDomain := range includedSiteDomains {
		if siteDomain == "" {
			continue
		}
		tagArgs = append(tagArgs, "--tag "+shellQuote("site-domain:"+siteDomain))
	}
	for _, dbID := range includedDatabaseIDs {
		tagArgs = append(tagArgs, "--tag "+shellQuote("db:"+dbID))
	}
	for _, dbName := range includedDatabaseNames {
		if dbName == "" {
			continue
		}
		tagArgs = append(tagArgs, "--tag "+shellQuote("db-name:"+dbName))
	}
	backupScript := fmt.Sprintf("%s backup --host fastcp --json %s %s %s", resticCmd, strings.Join(tagArgs, " "), strings.Join(excludeArgs, " "), strings.Join(quotedPaths, " "))
	backupOutput, backupErr := s.runWithResticLockRecovery(ctx, username, env, cfg, backupScript)
	if backupErr != nil {
		return "", "", fmt.Errorf("restic backup failed: %w: %s", backupErr, strings.TrimSpace(string(backupOutput)))
	}
	snapshotID := parseSnapshotIDFromBackupOutput(backupOutput)
	if snapshotID == "" {
		slog.Warn("snapshot id not found in backup output", "username", username)
	}

	forgetArgs := []string{resticCmd + " forget --group-by '' --tag " + shellQuote("fastcp")}
	if cfg.KeepLast > 0 {
		forgetArgs = append(forgetArgs, fmt.Sprintf("--keep-last %d", cfg.KeepLast))
	}
	if cfg.KeepDaily > 0 {
		forgetArgs = append(forgetArgs, fmt.Sprintf("--keep-daily %d", cfg.KeepDaily))
	}
	if cfg.KeepWeekly > 0 {
		forgetArgs = append(forgetArgs, fmt.Sprintf("--keep-weekly %d", cfg.KeepWeekly))
	}
	if cfg.KeepMonthly > 0 {
		forgetArgs = append(forgetArgs, fmt.Sprintf("--keep-monthly %d", cfg.KeepMonthly))
	}
	retentionPolicyEnabled := cfg.KeepLast > 0 || cfg.KeepDaily > 0 || cfg.KeepWeekly > 0 || cfg.KeepMonthly > 0
	if retentionPolicyEnabled {
		if output, forgetErr := s.runWithResticLockRecovery(ctx, username, env, cfg, strings.Join(forgetArgs, " ")); forgetErr != nil {
			return "", "", fmt.Errorf("restic retention failed: %w: %s", forgetErr, strings.TrimSpace(string(output)))
		}
	}
	shouldPrune := retentionPolicyEnabled && (cfg.LastPruneAt == nil || cfg.LastPruneAt.Before(time.Now().UTC().Add(-backupPruneInterval)))
	if shouldPrune {
		pruneScript := resticCmd + " prune"
		if output, pruneErr := s.runWithResticLockRecovery(ctx, username, env, cfg, pruneScript); pruneErr != nil {
			return "", "", fmt.Errorf("restic prune failed: %w: %s", pruneErr, strings.TrimSpace(string(output)))
		}
		now := time.Now().UTC()
		if _, err := s.db.ExecContext(ctx, "UPDATE backup_configs SET last_prune_at = ?, updated_at = ? WHERE username = ?", now, now, username); err != nil {
			slog.Warn("failed to update last prune time", "username", username, "error", err)
		}
	}

	if snapshotID == "" {
		snapshotsJSON, snapErr := s.runWithResticLockRecovery(ctx, username, env, cfg,
			fmt.Sprintf("%s snapshots --json --tag %s --latest 1", resticCmd, shellQuote("fastcp")))
		if snapErr != nil {
			slog.Warn("failed to read latest snapshot metadata", "username", username, "error", snapErr, "output", strings.TrimSpace(string(snapshotsJSON)))
		} else {
			snapshots, parseErr := parseSnapshotsJSON(snapshotsJSON)
			if parseErr != nil {
				slog.Warn("failed to parse latest snapshot metadata", "username", username, "error", parseErr)
			} else if len(snapshots) > 0 {
				snapshotID = snapshots[0].ID
			}
		}
	}
	msg := fmt.Sprintf("Backed up %d site(s) and %d database dump(s).", len(siteRoots), dumpCount)
	return snapshotID, msg, nil
}

func (s *BackupService) triggerBackup(ctx context.Context, username, triggerType string, allowDisabled bool) (string, error) {
	cfg, jobID, err := s.claimBackupRun(ctx, username, triggerType, allowDisabled)
	if err != nil {
		return "", err
	}
	go func() {
		s.sem <- struct{}{}
		defer func() { <-s.sem }()
		snapshotID, message, runErr := s.runBackup(username, cfg)
		if runErr != nil {
			s.finishBackupRun(context.Background(), username, jobID, "failed", "", runErr.Error())
			return
		}
		s.finishBackupRun(context.Background(), username, jobID, "success", snapshotID, message)
	}()
	return jobID, nil
}

func (s *BackupService) RunNow(ctx context.Context, username string) (string, error) {
	return s.triggerBackup(ctx, username, "manual", true)
}

func (s *BackupService) ListJobs(ctx context.Context, username string, limit int) ([]BackupJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, job_type, status, snapshot_id, message, started_at, finished_at
		FROM backup_jobs WHERE username = ? ORDER BY started_at DESC LIMIT ?`, username, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []BackupJob
	for rows.Next() {
		var item BackupJob
		var started time.Time
		var finished sql.NullTime
		if err := rows.Scan(&item.ID, &item.Username, &item.JobType, &item.Status, &item.SnapshotID, &item.Message, &started, &finished); err != nil {
			continue
		}
		item.StartedAt = started.UTC().Format(time.RFC3339)
		if finished.Valid {
			ft := finished.Time.UTC().Format(time.RFC3339)
			item.FinishedAt = &ft
		}
		jobs = append(jobs, item)
	}
	return jobs, nil
}

func (s *BackupService) ListSnapshots(ctx context.Context, username string, limit int) ([]BackupSnapshot, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	cfg, err := s.getConfigRow(ctx, username)
	if err != nil {
		if err == sql.ErrNoRows {
			return []BackupSnapshot{}, nil
		}
		return nil, err
	}
	if strings.TrimSpace(cfg.Repository) == "" || strings.TrimSpace(cfg.PasswordEnc) == "" {
		return []BackupSnapshot{}, nil
	}
	pass, err := crypto.Decrypt(cfg.PasswordEnc)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt repository password")
	}
	homeDir, _, _, err := s.userHome(username)
	if err != nil {
		return nil, err
	}
	env, envErr := s.resticEnv(cfg, homeDir, pass)
	if envErr != nil {
		return nil, envErr
	}
	resticCmd := resticCommand(cfg)
	output, cmdErr := s.runWithResticLockRecovery(ctx, username, env, cfg, fmt.Sprintf(
		"%s snapshots --json --tag %s --latest %d",
		resticCmd, shellQuote("fastcp"), limit))
	if cmdErr == nil {
		snapshots, parseErr := parseSnapshotsJSON(output)
		if parseErr == nil {
			for i := range snapshots {
				t := strings.TrimSpace(snapshots[i].Time)
				if t == "" {
					continue
				}
				if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
					snapshots[i].Time = parsed.UTC().Format(time.RFC3339)
				}
			}
			if len(snapshots) > 0 {
				return snapshots, nil
			}
		} else {
			slog.Warn("failed to parse tagged snapshots output", "username", username, "error", parseErr)
		}
	} else {
		slog.Warn("tagged snapshots query failed", "username", username, "error", cmdErr, "output", strings.TrimSpace(string(output)))
	}

	// Fallback: list latest snapshots without tag filter to handle legacy
	// repositories/snapshots that may not carry expected tags.
	fallbackOutput, fallbackErr := s.runWithResticLockRecovery(ctx, username, env, cfg, fmt.Sprintf(
		"%s snapshots --json --latest %d",
		resticCmd, limit))
	if fallbackErr != nil {
		return nil, fmt.Errorf("failed to list snapshots: %w: %s", fallbackErr, strings.TrimSpace(string(fallbackOutput)))
	}
	snapshots, parseErr := parseSnapshotsJSON(fallbackOutput)
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse snapshots: %w", parseErr)
	}

	for i := range snapshots {
		t := strings.TrimSpace(snapshots[i].Time)
		if t == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
			snapshots[i].Time = parsed.UTC().Format(time.RFC3339)
		}
	}
	return snapshots, nil
}

func (s *BackupService) createJob(ctx context.Context, username, jobType, message string) (string, error) {
	jobID := uuid.New().String()
	_, err := s.db.ExecContext(ctx, `INSERT INTO backup_jobs (id, username, job_type, status, message, started_at)
		VALUES (?, ?, ?, 'running', ?, ?)`, jobID, username, jobType, message, time.Now().UTC())
	if err != nil {
		return "", err
	}
	return jobID, nil
}

func (s *BackupService) finishJob(ctx context.Context, jobID, status, message string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE backup_jobs SET status = ?, message = ?, finished_at = ? WHERE id = ?`,
		status, message, time.Now().UTC(), jobID)
}

func (s *BackupService) createRestoreJob(ctx context.Context, username, jobType, message string) (string, error) {
	return s.createJob(ctx, username, jobType, message)
}

func (s *BackupService) finishRestoreJob(ctx context.Context, jobID, status, message string) {
	s.finishJob(ctx, jobID, status, message)
}

func (s *BackupService) DeleteSnapshot(ctx context.Context, username, snapshotID string) (string, error) {
	cfg, err := s.getConfigRow(ctx, username)
	if err != nil {
		return "", fmt.Errorf("backup repository is not configured")
	}
	pass, err := crypto.Decrypt(cfg.PasswordEnc)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt repository password")
	}
	homeDir, _, _, err := s.userHome(username)
	if err != nil {
		return "", err
	}
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return "", fmt.Errorf("snapshot_id is required")
	}
	jobID, err := s.createJob(ctx, username, "delete_snapshot", "Snapshot delete started.")
	if err != nil {
		return "", err
	}
	go func() {
		s.sem <- struct{}{}
		defer func() { <-s.sem }()
		env, envErr := s.resticEnv(cfg, homeDir, pass)
		if envErr != nil {
			s.finishJob(context.Background(), jobID, "failed", "Snapshot delete failed: "+envErr.Error())
			return
		}
		forgetScript := fmt.Sprintf("%s forget --group-by '' %s", resticCommand(cfg), shellQuote(snapshotID))
		if output, forgetErr := s.runWithResticLockRecovery(context.Background(), username, env, cfg, forgetScript); forgetErr != nil {
			s.finishJob(context.Background(), jobID, "failed", fmt.Sprintf("Snapshot delete failed: %v: %s", forgetErr, strings.TrimSpace(string(output))))
			return
		}
		s.finishJob(context.Background(), jobID, "success", "Snapshot deleted. Space will be reclaimed during prune.")
	}()
	return jobID, nil
}

func (s *BackupService) CreateSnapshotZip(ctx context.Context, username, snapshotID string) (string, string, error) {
	select {
	case s.exportSem <- struct{}{}:
		defer func() { <-s.exportSem }()
	default:
		return "", "", fmt.Errorf("another snapshot export is already running; please wait")
	}
	cfg, err := s.getConfigRow(ctx, username)
	if err != nil {
		return "", "", fmt.Errorf("backup repository is not configured")
	}
	pass, err := crypto.Decrypt(cfg.PasswordEnc)
	if err != nil {
		return "", "", fmt.Errorf("failed to decrypt repository password")
	}
	homeDir, uid, gid, err := s.userHome(username)
	if err != nil {
		return "", "", err
	}
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return "", "", fmt.Errorf("snapshotId is required")
	}
	workCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()

	downloadRoot := filepath.Join(homeDir, ".fastcp", "backups", "downloads")
	if err := s.ensureDirOwned(downloadRoot, uid, gid); err != nil {
		return "", "", err
	}
	workDir := filepath.Join(downloadRoot, uuid.New().String())
	cleanupWorkDir := true
	defer func() {
		if cleanupWorkDir {
			_ = os.RemoveAll(workDir)
		}
	}()
	if err := s.ensureDirOwned(workDir, uid, gid); err != nil {
		return "", "", err
	}
	restoreTarget := filepath.Join(workDir, "restore")
	if err := s.ensureDirOwned(restoreTarget, uid, gid); err != nil {
		return "", "", err
	}
	env, envErr := s.resticEnv(cfg, homeDir, pass)
	if envErr != nil {
		return "", "", envErr
	}
	resticCmd := resticCommand(cfg)
	run := func(callCtx context.Context, script string) ([]byte, error) {
		return s.runWithResticLockRecoveryLimited(callCtx, username, env, cfg, script, maxShellOutputBytes)
	}
	manifest, manifestErr := loadSnapshotManifest(workCtx, run, resticCmd, snapshotID)
	if manifestErr != nil {
		return "", "", fmt.Errorf("failed to load snapshot manifest for export: %w", manifestErr)
	}
	manifestPathInSnapshot, manifestPathErr := resolveSnapshotPathBySuffixUnique(
		workCtx,
		run,
		resticCmd,
		snapshotID,
		filepath.Join(".fastcp", "backups", "manifest.json"),
		false,
	)
	if manifestPathErr != nil {
		return "", "", fmt.Errorf("failed to resolve snapshot manifest path for export: %w", manifestPathErr)
	}
	if err := s.ensureDiskHeadroomForSnapshotExport(workCtx, username, snapshotID, downloadRoot, cfg, env); err != nil {
		return "", "", err
	}
	includeSet := map[string]struct{}{
		normalizePathForMatch(manifestPathInSnapshot): {},
	}
	for _, site := range manifest.Sites {
		p := normalizePathForMatch(site.RootPath)
		if p != "" {
			includeSet[p] = struct{}{}
		}
	}
	for _, dbDump := range manifest.Databases {
		p := normalizePathForMatch(dbDump.DumpPath)
		if p != "" {
			includeSet[p] = struct{}{}
		}
	}
	includePaths := make([]string, 0, len(includeSet))
	for p := range includeSet {
		if p != "" {
			includePaths = append(includePaths, p)
		}
	}
	sort.Strings(includePaths)
	includeArgs := make([]string, 0, len(includePaths))
	for _, p := range includePaths {
		includeArgs = append(includeArgs, "--include "+shellQuote(p))
	}
	restoreScript := fmt.Sprintf("%s restore %s --target %s %s",
		resticCmd,
		shellQuote(snapshotID),
		shellQuote(restoreTarget),
		strings.Join(includeArgs, " "),
	)
	if output, restoreErr := s.runWithResticLockRecoveryLimited(workCtx, username, env, cfg, restoreScript, maxShellOutputBytes); restoreErr != nil {
		return "", "", fmt.Errorf("failed to restore snapshot for download: %w: %s", restoreErr, strings.TrimSpace(string(output)))
	}

	zipEntries := make([]zipExportEntry, 0, len(manifest.Sites)+len(manifest.Databases)+1)
	manifestRestoredPath := filepath.Join(restoreTarget, strings.TrimPrefix(filepath.ToSlash(normalizePathForMatch(manifestPathInSnapshot)), "/"))
	zipEntries = append(zipEntries, zipExportEntry{
		SourcePath:  manifestRestoredPath,
		ArchivePath: "manifest.json",
	})
	siteNameCount := make(map[string]int, len(manifest.Sites))
	for _, site := range manifest.Sites {
		sourcePathInSnapshot := normalizePathForMatch(site.RootPath)
		if sourcePathInSnapshot == "" {
			continue
		}
		sourcePath := filepath.Join(restoreTarget, strings.TrimPrefix(filepath.ToSlash(sourcePathInSnapshot), "/"))
		baseName := sanitizeArchivePathSegment(site.Domain, filepath.Base(sourcePathInSnapshot))
		if baseName == "" {
			baseName = "site"
		}
		siteNameCount[baseName]++
		archiveName := baseName
		if siteNameCount[baseName] > 1 {
			archiveName = fmt.Sprintf("%s-%d", baseName, siteNameCount[baseName])
		}
		zipEntries = append(zipEntries, zipExportEntry{
			SourcePath:  sourcePath,
			ArchivePath: filepath.ToSlash(filepath.Join("websites", archiveName)),
		})
	}
	dbNameCount := make(map[string]int, len(manifest.Databases))
	for _, dbDump := range manifest.Databases {
		sourcePathInSnapshot := normalizePathForMatch(dbDump.DumpPath)
		if sourcePathInSnapshot == "" {
			continue
		}
		sourcePath := filepath.Join(restoreTarget, strings.TrimPrefix(filepath.ToSlash(sourcePathInSnapshot), "/"))
		baseName := sanitizeArchivePathSegment(dbDump.DBName, strings.TrimSuffix(filepath.Base(sourcePathInSnapshot), ".sql.gz"))
		if baseName == "" {
			baseName = "database"
		}
		dbNameCount[baseName]++
		fileName := baseName
		if dbNameCount[baseName] > 1 {
			fileName = fmt.Sprintf("%s-%d", baseName, dbNameCount[baseName])
		}
		zipEntries = append(zipEntries, zipExportEntry{
			SourcePath:  sourcePath,
			ArchivePath: filepath.ToSlash(filepath.Join("databases", fileName+".sql.gz")),
		})
	}
	for _, entry := range zipEntries {
		if _, statErr := os.Stat(entry.SourcePath); statErr != nil {
			return "", "", fmt.Errorf("failed to prepare snapshot zip path %s: %w", entry.SourcePath, statErr)
		}
	}

	fileName := fmt.Sprintf("snapshot-%s.zip", sanitizeSnapshotIDForFile(snapshotID))
	zipPath := filepath.Join(workDir, fileName)
	if err := zipMappedPaths(zipPath, zipEntries, true); err != nil {
		return "", "", fmt.Errorf("failed to build snapshot zip: %w", err)
	}
	_ = os.RemoveAll(restoreTarget)
	if err := os.Chown(zipPath, uid, gid); err != nil {
		slog.Warn("failed to set snapshot zip ownership", "path", zipPath, "error", err)
	}
	cleanupWorkDir = false
	return zipPath, fileName, nil
}

func (s *BackupService) RestoreSite(ctx context.Context, username string, req *RestoreSiteRequest) (string, error) {
	cfg, err := s.getConfigRow(ctx, username)
	if err != nil {
		return "", fmt.Errorf("backup repository is not configured")
	}
	pass, err := crypto.Decrypt(cfg.PasswordEnc)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt repository password")
	}
	site, err := s.db.GetSite(ctx, req.SiteID)
	if err != nil || site.Username != username {
		return "", fmt.Errorf("site not found")
	}
	jobID, err := s.createRestoreJob(ctx, username, "restore_site", "Website restore started.")
	if err != nil {
		return "", err
	}
	homeDir, _, _, err := s.userHome(username)
	if err != nil {
		return "", err
	}
	siteRoot := filepath.Clean(filepath.Dir(site.DocumentRoot))
	go func() {
		s.sem <- struct{}{}
		defer func() { <-s.sem }()
		restoreTarget := filepath.Join(homeDir, ".fastcp", "backups", "restore", jobID)
		env, envErr := s.resticEnv(cfg, homeDir, pass)
		if envErr != nil {
			s.finishRestoreJob(context.Background(), jobID, "failed", fmt.Sprintf("Website restore failed: %v", envErr))
			return
		}
		resticCmd := resticCommand(cfg)
		run := func(callCtx context.Context, script string) ([]byte, error) {
			return s.runWithResticLockRecovery(callCtx, username, env, cfg, script)
		}
		manifest, manifestErr := loadSnapshotManifest(context.Background(), run, resticCmd, req.SnapshotID)
		if manifestErr != nil {
			s.finishRestoreJob(context.Background(), jobID, "failed", fmt.Sprintf("Website restore failed: %v", manifestErr))
			return
		}
		sourcePathInSnapshot, pathErr := findManifestSitePath(manifest, site.Domain)
		if pathErr != nil {
			s.finishRestoreJob(context.Background(), jobID, "failed", fmt.Sprintf("Website restore failed: %v", pathErr))
			return
		}
		sourcePath := filepath.Join(restoreTarget, strings.TrimPrefix(filepath.ToSlash(sourcePathInSnapshot), "/"))
		script := fmt.Sprintf("rm -rf %s && mkdir -p %s && %s restore %s --target %s --include %s && test -d %s && rsync -a --delete %s/ %s/ && rm -rf %s",
			shellQuote(restoreTarget),
			shellQuote(restoreTarget),
			resticCmd,
			shellQuote(strings.TrimSpace(req.SnapshotID)),
			shellQuote(restoreTarget),
			shellQuote(sourcePathInSnapshot),
			shellQuote(sourcePath),
			shellQuote(sourcePath),
			shellQuote(siteRoot),
			shellQuote(restoreTarget),
		)
		if output, runErr := s.runWithResticLockRecovery(context.Background(), username, env, cfg, script); runErr != nil {
			s.finishRestoreJob(context.Background(), jobID, "failed", fmt.Sprintf("Website restore failed: %v: %s", runErr, strings.TrimSpace(string(output))))
			return
		}
		s.finishRestoreJob(context.Background(), jobID, "success", "Website restored successfully.")
	}()
	return jobID, nil
}

func (s *BackupService) RestoreDatabase(ctx context.Context, username string, req *RestoreDatabaseRequest) (string, error) {
	cfg, err := s.getConfigRow(ctx, username)
	if err != nil {
		return "", fmt.Errorf("backup repository is not configured")
	}
	pass, err := crypto.Decrypt(cfg.PasswordEnc)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt repository password")
	}
	dbRec, err := s.db.GetDatabase(ctx, req.DatabaseID)
	if err != nil || dbRec.Username != username {
		return "", fmt.Errorf("database not found")
	}
	if strings.TrimSpace(dbRec.DBPassword) == "" {
		return "", fmt.Errorf("database credentials are not available for restore")
	}
	dbPass, err := crypto.Decrypt(dbRec.DBPassword)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt database credentials")
	}
	jobID, err := s.createRestoreJob(ctx, username, "restore_database", "Database restore started.")
	if err != nil {
		return "", err
	}
	homeDir, _, _, err := s.userHome(username)
	if err != nil {
		return "", err
	}
	go func() {
		s.sem <- struct{}{}
		defer func() { <-s.sem }()
		restoreTarget := filepath.Join(homeDir, ".fastcp", "backups", "restore", jobID)
		env, envErr := s.resticEnv(cfg, homeDir, pass)
		if envErr != nil {
			s.finishRestoreJob(context.Background(), jobID, "failed", fmt.Sprintf("Database restore failed: %v", envErr))
			return
		}
		env["MYSQL_PWD"] = dbPass
		resticCmd := resticCommand(cfg)
		run := func(callCtx context.Context, script string) ([]byte, error) {
			return s.runWithResticLockRecovery(callCtx, username, env, cfg, script)
		}
		manifest, manifestErr := loadSnapshotManifest(context.Background(), run, resticCmd, req.SnapshotID)
		if manifestErr != nil {
			s.finishRestoreJob(context.Background(), jobID, "failed", fmt.Sprintf("Database restore failed: %v", manifestErr))
			return
		}
		dumpPathInSnapshot, pathErr := findManifestDatabaseDumpPath(manifest, dbRec.DBName)
		if pathErr != nil {
			s.finishRestoreJob(context.Background(), jobID, "failed", fmt.Sprintf("Database restore failed: %v", pathErr))
			return
		}
		restoredDump := filepath.Join(restoreTarget, strings.TrimPrefix(filepath.ToSlash(dumpPathInSnapshot), "/"))
		script := fmt.Sprintf("rm -rf %s && mkdir -p %s && %s restore %s --target %s --include %s && test -f %s && gunzip -c %s | mysql -h 127.0.0.1 -u %s %s && rm -rf %s",
			shellQuote(restoreTarget),
			shellQuote(restoreTarget),
			resticCmd,
			shellQuote(strings.TrimSpace(req.SnapshotID)),
			shellQuote(restoreTarget),
			shellQuote(dumpPathInSnapshot),
			shellQuote(restoredDump),
			shellQuote(restoredDump),
			shellQuote(dbRec.DBUser),
			shellQuote(dbRec.DBName),
			shellQuote(restoreTarget),
		)
		if output, runErr := s.runWithResticLockRecovery(context.Background(), username, env, cfg, script); runErr != nil {
			s.finishRestoreJob(context.Background(), jobID, "failed", fmt.Sprintf("Database restore failed: %v: %s", runErr, strings.TrimSpace(string(output))))
			return
		}
		s.finishRestoreJob(context.Background(), jobID, "success", "Database restored successfully.")
	}()
	return jobID, nil
}

func (s *BackupService) TriggerManualBackup(ctx context.Context, username string) (*BackupRunResponse, error) {
	jobID, err := s.RunNow(ctx, username)
	if err != nil {
		return nil, err
	}
	return &BackupRunResponse{
		JobID:   jobID,
		Status:  "running",
		Message: "Backup job started.",
	}, nil
}

func (s *BackupService) ensureConfigured(ctx context.Context, username string) error {
	cfg, err := s.getConfigRow(ctx, username)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Repository) == "" || strings.TrimSpace(cfg.PasswordEnc) == "" {
		return errors.New("backup repository and password are required")
	}
	return nil
}
