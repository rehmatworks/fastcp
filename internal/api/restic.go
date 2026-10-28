package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/user"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
)

const (
	backupDir     = ".fastcp/backups"
	sftpKeyFile   = ".ssh/fastcp_backup"
	payloadBegin  = "@@FASTCP-BEGIN@@\n"
	payloadEnd    = "\n@@FASTCP-END@@"
	defaultOutput = 1 << 20
)

// scriptPrelude is prepended to every backup script. Values are always
// passed through the environment, never interpolated into the script.
const scriptPrelude = `umask 077
command -v restic >/dev/null 2>&1 || { echo "restic is not installed on this server." >&2; exit 127; }
mkdir -p "$HOME/.tmp" "$HOME/` + backupDir + `"
OPTS=()
if [ -n "${FASTCP_RESTIC_OPTS:-}" ]; then mapfile -t OPTS <<< "$FASTCP_RESTIC_OPTS"; fi
rr() { restic --retry-lock 2m "${OPTS[@]}" "$@"; }
capture() {
  local e out
  e=$(mktemp)
  if out=$("$@" 2>"$e"); then
    rm -f "$e"
    printf '@@FASTCP-BEGIN@@\n%s\n@@FASTCP-END@@\n' "$out"
  else
    local rc=$?
    cat "$e" >&2
    rm -f "$e"
    return $rc
  fi
}
`

// resticRepo is a user's repository with decrypted credentials.
type resticRepo struct {
	username string
	home     string
	env      map[string]string
}

func homeDir(username string) string {
	if u, err := user.Lookup(username); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return "/home/" + username
}

func (s *Server) openSecret(enc string) (string, error) {
	if strings.TrimSpace(enc) == "" {
		return "", nil
	}
	v, err := s.box.Open(crypto.PurposeBackup, enc)
	if err != nil {
		return "", errors.New("a stored backup credential cannot be decrypted; enter it again")
	}
	return v, nil
}

func (s *Server) openRepo(cfg *database.BackupConfig) (*resticRepo, error) {
	if cfg.Repository == "" {
		return nil, badRequest("Set up a backup destination first.")
	}
	password, err := s.openSecret(cfg.PasswordEnc)
	if err != nil {
		return nil, badRequest("%v", err)
	}
	if password == "" {
		return nil, badRequest("The repository password is missing; enter it again.")
	}
	home := homeDir(cfg.Username)
	env := map[string]string{"RESTIC_REPOSITORY": cfg.Repository, "RESTIC_PASSWORD": password}
	var opts []string
	switch cfg.BackendType {
	case "s3":
		access, err := s.openSecret(cfg.S3AccessKeyEnc)
		if err != nil {
			return nil, badRequest("%v", err)
		}
		secret, err := s.openSecret(cfg.S3SecretKeyEnc)
		if err != nil {
			return nil, badRequest("%v", err)
		}
		if access == "" || secret == "" {
			return nil, badRequest("S3 credentials are missing; enter them again.")
		}
		env["AWS_ACCESS_KEY_ID"], env["AWS_SECRET_ACCESS_KEY"] = access, secret
		if token, err := s.openSecret(cfg.S3SessionTokenEnc); err == nil && token != "" {
			env["AWS_SESSION_TOKEN"] = token
		}
		if cfg.S3Region != "" {
			env["AWS_DEFAULT_REGION"] = cfg.S3Region
			opts = append(opts, "s3.region="+cfg.S3Region)
		}
		lookup := cfg.S3BucketLookup
		if lookup == "" {
			lookup = "auto"
		}
		opts = append(opts, "s3.bucket-lookup="+lookup)
		if cfg.S3ListObjectsV1 {
			opts = append(opts, "s3.list-objects-v1=true")
		}
	case "sftp":
		port := cfg.SFTPPort
		if port == 0 {
			port = 22
		}
		opts = append(opts, fmt.Sprintf("sftp.command=ssh -p %d -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ServerAliveInterval=60 -i %s %s@%s -s sftp",
			port, path.Join(home, sftpKeyFile), cfg.SFTPUsername, cfg.SFTPHost))
	}
	if len(opts) > 0 {
		var lines []string
		for _, o := range opts {
			lines = append(lines, "-o", o)
		}
		env["FASTCP_RESTIC_OPTS"] = strings.Join(lines, "\n")
	}
	return &resticRepo{username: cfg.Username, home: home, env: env}, nil
}

type execError struct {
	exit   int
	output string
}

func (e *execError) Error() string { return e.output }

func isLockError(out string) bool {
	return strings.Contains(out, "already locked") || strings.Contains(out, "unable to create lock")
}

// tail keeps the end of command output for error messages.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// plural formats a count with a noun, such as "1 site" or "2 sites".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// runScript executes a backup script as username. With a repository, its
// credentials are added to the environment and a stale restic lock is
// removed and the script retried once.
func (s *Server) runScript(ctx context.Context, username string, repo *resticRepo, script string, env map[string]string, timeout time.Duration, maxOut int) (string, error) {
	merged := map[string]string{}
	if repo != nil {
		for k, v := range repo.env {
			merged[k] = v
		}
	}
	for k, v := range env {
		merged[k] = v
	}
	if maxOut <= 0 {
		maxOut = defaultOutput
	}
	req := &agent.UserExecRequest{
		Username: username, Script: scriptPrelude + script, Env: merged,
		TimeoutSec: int(timeout.Seconds()), MaxOutput: maxOut,
	}
	for attempt := 0; ; attempt++ {
		res, err := s.agent.UserExec(ctx, req)
		if err != nil {
			return "", err
		}
		if res.ExitCode == 0 {
			return res.Output, nil
		}
		if attempt == 0 && repo != nil && isLockError(res.Output) {
			unlock := &agent.UserExecRequest{Username: username, Script: scriptPrelude + "rr unlock", Env: merged, TimeoutSec: 300}
			if u, err := s.agent.UserExec(ctx, unlock); err == nil && u.ExitCode == 0 {
				continue
			}
		}
		return res.Output, &execError{exit: res.ExitCode, output: tail(res.Output, 1500)}
	}
}

// runRestic runs a script against the user's repository.
func (s *Server) runRestic(ctx context.Context, repo *resticRepo, script string, env map[string]string, timeout time.Duration, maxOut int) (string, error) {
	return s.runScript(ctx, repo.username, repo, script, env, timeout, maxOut)
}

func payload(out string) (string, bool) {
	i := strings.LastIndex(out, payloadBegin)
	if i < 0 {
		return "", false
	}
	rest := out[i+len(payloadBegin):]
	j := strings.Index(rest, payloadEnd)
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}

// ---------------------------------------------------------------------------
// Snapshots and manifests

type resticSnapshot struct {
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Time     time.Time `json:"time"`
	Paths    []string  `json:"paths"`
	Tags     []string  `json:"tags"`
	Hostname string    `json:"hostname"`
	Summary  *struct {
		TotalBytes uint64 `json:"total_bytes_processed"`
		TotalFiles uint64 `json:"total_files_processed"`
		DataAdded  uint64 `json:"data_added"`
	} `json:"summary,omitempty"`
}

type snapshotView struct {
	ID          string    `json:"id"`
	ShortID     string    `json:"short_id"`
	Time        time.Time `json:"time"`
	Sites       []string  `json:"sites"`
	Databases   []string  `json:"databases"`
	SiteIDs     []string  `json:"site_ids"`
	DatabaseIDs []string  `json:"database_ids"`
	Bytes       uint64    `json:"bytes,omitempty"`
	Files       uint64    `json:"files,omitempty"`
	Added       uint64    `json:"data_added,omitempty"`
}

func viewSnapshot(sn resticSnapshot) snapshotView {
	v := snapshotView{ID: sn.ID, ShortID: sn.ShortID, Time: sn.Time.UTC(),
		Sites: []string{}, Databases: []string{}, SiteIDs: []string{}, DatabaseIDs: []string{}}
	for _, t := range sn.Tags {
		if d, ok := strings.CutPrefix(t, "site-domain:"); ok {
			v.Sites = append(v.Sites, d)
		} else if d, ok := strings.CutPrefix(t, "db-name:"); ok {
			v.Databases = append(v.Databases, d)
		} else if id, ok := strings.CutPrefix(t, "site:"); ok {
			v.SiteIDs = append(v.SiteIDs, id)
		} else if id, ok := strings.CutPrefix(t, "db:"); ok {
			v.DatabaseIDs = append(v.DatabaseIDs, id)
		}
	}
	if sn.Summary != nil {
		v.Bytes, v.Files, v.Added = sn.Summary.TotalBytes, sn.Summary.TotalFiles, sn.Summary.DataAdded
	}
	return v
}

const snapshotsScript = `
if ! rr cat config >/dev/null 2>&1; then printf '@@FASTCP-BEGIN@@\n[]\n@@FASTCP-END@@\n'; exit 0; fi
capture rr snapshots --json --tag "fastcp,user:$FASTCP_OWNER"
`

// snapshots lists the user's snapshots, newest first.
func (s *Server) snapshots(ctx context.Context, repo *resticRepo) ([]resticSnapshot, error) {
	out, err := s.runRestic(ctx, repo, snapshotsScript, map[string]string{"FASTCP_OWNER": repo.username}, 5*time.Minute, 16<<20)
	if err != nil {
		return nil, err
	}
	body, ok := payload(out)
	if !ok {
		return nil, fmt.Errorf("unexpected restic output: %s", tail(out, 300))
	}
	var list []resticSnapshot
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		return nil, fmt.Errorf("could not read the snapshot list: %w", err)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Time.After(list[j].Time) })
	return list, nil
}

// findSnapshot resolves a (short) snapshot ID among the user's snapshots.
func (s *Server) findSnapshot(ctx context.Context, repo *resticRepo, id string) (*resticSnapshot, error) {
	list, err := s.snapshots(ctx, repo)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id || strings.HasPrefix(list[i].ID, id) {
			return &list[i], nil
		}
	}
	return nil, notFound("Snapshot")
}

type backupManifest struct {
	Version   int    `json:"version"`
	CreatedAt string `json:"created_at"`
	Username  string `json:"username"`
	Sites     []struct {
		Domain   string `json:"domain"`
		RootPath string `json:"root_path"`
	} `json:"sites"`
	Databases []struct {
		DBName   string `json:"db_name"`
		DumpPath string `json:"dump_path"`
	} `json:"databases"`
}

const manifestScript = `
p="$HOME/` + backupDir + `/manifest.json"
if ! capture rr dump "$FASTCP_SNAPSHOT" "$p"; then
  p=$(rr ls --json "$FASTCP_SNAPSHOT" 2>/dev/null | grep -o '"path":"[^"]*/\.fastcp/backups/manifest\.json"' | head -n1 | sed 's/^"path":"//; s/"$//') || true
  [ -n "$p" ] || { echo "This snapshot has no FastCP manifest." >&2; exit 3; }
  capture rr dump "$FASTCP_SNAPSHOT" "$p"
fi
`

func (s *Server) loadManifest(ctx context.Context, repo *resticRepo, snapshotID string) (*backupManifest, error) {
	out, err := s.runRestic(ctx, repo, manifestScript, map[string]string{"FASTCP_SNAPSHOT": snapshotID}, 10*time.Minute, 4<<20)
	if err != nil {
		return nil, err
	}
	body, ok := payload(out)
	if !ok {
		return nil, errors.New("could not read the snapshot manifest")
	}
	var m backupManifest
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return nil, fmt.Errorf("the snapshot manifest is damaged: %w", err)
	}
	if m.Username != "" && m.Username != repo.username {
		return nil, errors.New("this snapshot belongs to another account")
	}
	return &m, nil
}

// ---------------------------------------------------------------------------
// Destination validation

var (
	sftpUserRE    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	hostRE        = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	repoPathRE    = regexp.MustCompile(`^/[A-Za-z0-9._/@+=,~-]*$`)
	s3EndpointRE  = regexp.MustCompile(`^(https?://)?[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)
	s3BucketRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,62}$`)
	s3PrefixRE    = regexp.MustCompile(`^[A-Za-z0-9._/-]{0,200}$`)
	s3RegionRE    = regexp.MustCompile(`^[a-z0-9-]{0,40}$`)
	rcloneRepoRE  = regexp.MustCompile(`^rclone:[A-Za-z0-9._-]+:[A-Za-z0-9._/@+=,~-]*$`)
	backendTypes  = []string{"local", "sftp", "s3", "rclone"}
	bucketLookups = []string{"auto", "dns", "path"}
)

func cleanRepoPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("a path is required")
	}
	if slices.Contains(strings.Split(p, "/"), "..") {
		return "", errors.New("the path must not contain .. segments")
	}
	p = path.Clean(p)
	if !repoPathRE.MatchString(p) || p == "/" {
		return "", errors.New("use an absolute path made of letters, digits and . _ - / characters")
	}
	return p, nil
}

func validHost(h string) bool {
	if ip := net.ParseIP(strings.Trim(h, "[]")); ip != nil {
		return true
	}
	return hostRE.MatchString(h)
}

// buildRepository validates the destination fields and fills in
// cfg.Repository.
func buildRepository(cfg *database.BackupConfig, localPath, rcloneRepo string) error {
	switch cfg.BackendType {
	case "local":
		p, err := cleanRepoPath(localPath)
		if err != nil {
			return badRequest("Local path: %v.", err)
		}
		cfg.Repository = p
	case "sftp":
		if !sftpUserRE.MatchString(cfg.SFTPUsername) {
			return badRequest("Enter the SFTP user name.")
		}
		if !validHost(cfg.SFTPHost) {
			return badRequest("Enter a valid SFTP host name or IP address.")
		}
		if cfg.SFTPPort == 0 {
			cfg.SFTPPort = 22
		}
		if cfg.SFTPPort < 1 || cfg.SFTPPort > 65535 {
			return badRequest("The SFTP port must be between 1 and 65535.")
		}
		p, err := cleanRepoPath(cfg.SFTPPath)
		if err != nil {
			return badRequest("SFTP path: %v.", err)
		}
		cfg.SFTPPath = p
		host := cfg.SFTPHost
		if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
			host = "[" + host + "]"
		}
		cfg.Repository = fmt.Sprintf("sftp://%s@%s:%d//%s", cfg.SFTPUsername, host, cfg.SFTPPort, strings.TrimPrefix(p, "/"))
	case "s3":
		cfg.S3Endpoint = strings.TrimRight(strings.TrimSpace(cfg.S3Endpoint), "/")
		cfg.S3Bucket = strings.TrimSpace(cfg.S3Bucket)
		cfg.S3Prefix = strings.Trim(strings.TrimSpace(cfg.S3Prefix), "/")
		cfg.S3Region = strings.TrimSpace(cfg.S3Region)
		switch {
		case !s3EndpointRE.MatchString(cfg.S3Endpoint):
			return badRequest("Enter the S3 endpoint, for example https://s3.amazonaws.com.")
		case !s3BucketRE.MatchString(cfg.S3Bucket):
			return badRequest("Enter a valid S3 bucket name.")
		case !s3PrefixRE.MatchString(cfg.S3Prefix) || strings.Contains(cfg.S3Prefix, ".."):
			return badRequest("The S3 prefix may only contain letters, digits and . _ - / characters.")
		case !s3RegionRE.MatchString(cfg.S3Region):
			return badRequest("Enter a valid S3 region, such as us-east-1.")
		}
		if cfg.S3BucketLookup == "" {
			cfg.S3BucketLookup = "auto"
		}
		if !slices.Contains(bucketLookups, cfg.S3BucketLookup) {
			return badRequest("Bucket lookup must be auto, dns or path.")
		}
		repo := "s3:" + cfg.S3Endpoint + "/" + cfg.S3Bucket
		if cfg.S3Prefix != "" {
			repo += "/" + cfg.S3Prefix
		}
		cfg.Repository = repo
	case "rclone":
		r := strings.TrimSpace(rcloneRepo)
		if !rcloneRepoRE.MatchString(r) {
			return badRequest("Use the form rclone:remote:path.")
		}
		cfg.Repository = r
	default:
		return badRequest("Choose a destination type: local, sftp, s3 or rclone.")
	}
	return nil
}

func describeExecErr(err error) string {
	var ee *execError
	if errors.As(err, &ee) {
		return ee.output
	}
	_, msg := classify(err)
	return msg
}
