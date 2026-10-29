package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/rehmatworks/fastcp/internal/validate"
)

const (
	defaultBackupSchedule = "0 2 * * *"
	minBackupInterval     = time.Hour
	pruneInterval         = 24 * time.Hour
	exportTTL             = time.Hour
	downloadTTL           = time.Minute
	backupConcurrency     = 2
	keepJobs              = 100
)

type backupRunner struct {
	s   *Server
	sem chan struct{}
}

func newBackupRunner(s *Server) *backupRunner {
	return &backupRunner{s: s, sem: make(chan struct{}, backupConcurrency)}
}

type downloadGrant struct {
	Username string
	JobID    string
	Path     string
	Name     string
}

// ---------------------------------------------------------------------------
// Configuration

type backupConfigView struct {
	BackendType         string     `json:"backend_type"`
	Repository          string     `json:"repository"`
	LocalPath           string     `json:"local_path"`
	SFTPUsername        string     `json:"sftp_username"`
	SFTPHost            string     `json:"sftp_host"`
	SFTPPort            int        `json:"sftp_port"`
	SFTPPath            string     `json:"sftp_path"`
	S3Endpoint          string     `json:"s3_endpoint"`
	S3Bucket            string     `json:"s3_bucket"`
	S3Prefix            string     `json:"s3_prefix"`
	S3Region            string     `json:"s3_region"`
	S3BucketLookup      string     `json:"s3_bucket_lookup"`
	S3ListObjectsV1     bool       `json:"s3_list_objects_v1"`
	HasPassword         bool       `json:"has_password"`
	HasS3Keys           bool       `json:"has_s3_keys"`
	Enabled             bool       `json:"enabled"`
	ScheduleCron        string     `json:"schedule_cron"`
	ScheduleDescription string     `json:"schedule_description"`
	ExcludeSiteIDs      []string   `json:"exclude_site_ids"`
	ExcludeDatabaseIDs  []string   `json:"exclude_database_ids"`
	KeepLast            int        `json:"keep_last"`
	KeepDaily           int        `json:"keep_daily"`
	KeepWeekly          int        `json:"keep_weekly"`
	KeepMonthly         int        `json:"keep_monthly"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastStatus          string     `json:"last_status"`
	LastMessage         string     `json:"last_message"`
	RunningJobID        string     `json:"running_job_id,omitempty"`
}

func viewBackupConfig(c *database.BackupConfig) *backupConfigView {
	v := &backupConfigView{
		BackendType: c.BackendType, Repository: c.Repository,
		SFTPUsername: c.SFTPUsername, SFTPHost: c.SFTPHost, SFTPPort: c.SFTPPort, SFTPPath: c.SFTPPath,
		S3Endpoint: c.S3Endpoint, S3Bucket: c.S3Bucket, S3Prefix: c.S3Prefix, S3Region: c.S3Region,
		S3BucketLookup: c.S3BucketLookup, S3ListObjectsV1: c.S3ListObjectsV1,
		HasPassword: c.PasswordEnc != "", HasS3Keys: c.S3AccessKeyEnc != "" && c.S3SecretKeyEnc != "",
		Enabled: c.Enabled, ScheduleCron: c.ScheduleCron, ScheduleDescription: describeCron(c.ScheduleCron),
		ExcludeSiteIDs: c.ExcludeSiteIDs, ExcludeDatabaseIDs: c.ExcludeDatabaseIDs,
		KeepLast: c.KeepLast, KeepDaily: c.KeepDaily, KeepWeekly: c.KeepWeekly, KeepMonthly: c.KeepMonthly,
		LastRunAt: c.LastRunAt, NextRunAt: c.NextRunAt, LastStatus: c.LastStatus, LastMessage: c.LastMessage,
		RunningJobID: c.RunningJobID,
	}
	if c.BackendType == "local" {
		v.LocalPath = c.Repository
	}
	if v.ExcludeSiteIDs == nil {
		v.ExcludeSiteIDs = []string{}
	}
	if v.ExcludeDatabaseIDs == nil {
		v.ExcludeDatabaseIDs = []string{}
	}
	return v
}

func defaultBackupConfig(username string) *database.BackupConfig {
	return &database.BackupConfig{
		Username: username, BackendType: "sftp", SFTPPort: 22, S3BucketLookup: "auto",
		ScheduleCron: defaultBackupSchedule, KeepLast: 7, KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 6,
		LastStatus: "idle",
	}
}

func (s *Server) backupConfig(ctx context.Context, username string) (*database.BackupConfig, bool, error) {
	cfg, err := s.db.GetBackupConfig(ctx, username)
	if errors.Is(err, database.ErrNotFound) {
		return defaultBackupConfig(username), false, nil
	}
	return cfg, err == nil, err
}

func (s *Server) getBackups(w http.ResponseWriter, r *http.Request) error {
	p := current(r)
	ctx := r.Context()
	cfg, saved, err := s.backupConfig(ctx, p.User.Username)
	if err != nil {
		return err
	}
	sites, err := s.db.ListSites(ctx, p.User.Username)
	if err != nil {
		return err
	}
	dbs, err := s.db.ListDatabases(ctx, p.User.Username)
	if err != nil {
		return err
	}
	type item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	siteItems, dbItems := []item{}, []item{}
	for _, site := range sites {
		siteItems = append(siteItems, item{site.ID, site.Domain})
	}
	for _, d := range dbs {
		dbItems = append(dbItems, item{d.ID, d.DBName})
	}
	out := map[string]any{
		"configured": saved && cfg.Repository != "",
		"config":     viewBackupConfig(cfg),
		"sites":      siteItems,
		"databases":  dbItems,
	}
	if st, err := s.agent.RcloneStatus(ctx); err == nil {
		out["rclone_installed"] = st.Installed
	}
	return ok(w, out)
}

type backupConfigRequest struct {
	BackendType        string   `json:"backend_type"`
	LocalPath          string   `json:"local_path"`
	RcloneRepository   string   `json:"rclone_repository"`
	SFTPUsername       string   `json:"sftp_username"`
	SFTPHost           string   `json:"sftp_host"`
	SFTPPort           int      `json:"sftp_port"`
	SFTPPath           string   `json:"sftp_path"`
	S3Endpoint         string   `json:"s3_endpoint"`
	S3Bucket           string   `json:"s3_bucket"`
	S3Prefix           string   `json:"s3_prefix"`
	S3Region           string   `json:"s3_region"`
	S3BucketLookup     string   `json:"s3_bucket_lookup"`
	S3ListObjectsV1    bool     `json:"s3_list_objects_v1"`
	S3AccessKey        string   `json:"s3_access_key"`
	S3SecretKey        string   `json:"s3_secret_key"`
	S3SessionToken     string   `json:"s3_session_token"`
	RepositoryPassword string   `json:"repository_password"`
	Enabled            bool     `json:"enabled"`
	ScheduleCron       string   `json:"schedule_cron"`
	ExcludeSiteIDs     []string `json:"exclude_site_ids"`
	ExcludeDatabaseIDs []string `json:"exclude_database_ids"`
	KeepLast           int      `json:"keep_last"`
	KeepDaily          int      `json:"keep_daily"`
	KeepWeekly         int      `json:"keep_weekly"`
	KeepMonthly        int      `json:"keep_monthly"`
}

func (s *Server) seal(v string) (string, error) {
	return s.box.Seal(crypto.PurposeBackup, v)
}

// mergeBackupConfig applies a request on top of the stored configuration.
// Secrets left empty keep their stored values.
func (s *Server) mergeBackupConfig(ctx context.Context, username string, req *backupConfigRequest) (*database.BackupConfig, error) {
	cfg, _, err := s.backupConfig(ctx, username)
	if err != nil {
		return nil, err
	}
	backend := strings.ToLower(strings.TrimSpace(req.BackendType))
	if !slices.Contains(backendTypes, backend) {
		return nil, badRequest("Choose a destination type: local, sftp, s3 or rclone.")
	}
	if backend != cfg.BackendType {
		cfg.S3AccessKeyEnc, cfg.S3SecretKeyEnc, cfg.S3SessionTokenEnc = "", "", ""
	}
	cfg.BackendType = backend
	cfg.SFTPUsername, cfg.SFTPHost, cfg.SFTPPort, cfg.SFTPPath = strings.TrimSpace(req.SFTPUsername), strings.TrimSpace(req.SFTPHost), req.SFTPPort, req.SFTPPath
	cfg.S3Endpoint, cfg.S3Bucket, cfg.S3Prefix, cfg.S3Region = req.S3Endpoint, req.S3Bucket, req.S3Prefix, req.S3Region
	cfg.S3BucketLookup, cfg.S3ListObjectsV1 = strings.ToLower(strings.TrimSpace(req.S3BucketLookup)), req.S3ListObjectsV1
	if err := buildRepository(cfg, req.LocalPath, req.RcloneRepository); err != nil {
		return nil, err
	}

	if req.RepositoryPassword != "" {
		if len(req.RepositoryPassword) < 8 || validate.NoControl("password", req.RepositoryPassword) != nil || len(req.RepositoryPassword) > 256 {
			return nil, badRequest("The repository password must be 8 to 256 characters without line breaks.")
		}
		if cfg.PasswordEnc, err = s.seal(req.RepositoryPassword); err != nil {
			return nil, err
		}
	}
	if cfg.PasswordEnc == "" {
		return nil, badRequest("Enter a repository password. Keep a copy: without it the backups cannot be restored.")
	}
	if backend == "s3" {
		for _, f := range []struct {
			v   string
			dst *string
		}{{req.S3AccessKey, &cfg.S3AccessKeyEnc}, {req.S3SecretKey, &cfg.S3SecretKeyEnc}, {req.S3SessionToken, &cfg.S3SessionTokenEnc}} {
			v := strings.TrimSpace(f.v)
			if v == "" {
				continue
			}
			if validate.NoControl("credential", v) != nil || len(v) > 4096 {
				return nil, badRequest("S3 credentials must not contain line breaks.")
			}
			if *f.dst, err = s.seal(v); err != nil {
				return nil, err
			}
		}
		if cfg.S3AccessKeyEnc == "" || cfg.S3SecretKeyEnc == "" {
			return nil, badRequest("Enter the S3 access key and secret key.")
		}
	}

	cfg.Enabled = req.Enabled
	cfg.ScheduleCron = strings.Join(strings.Fields(req.ScheduleCron), " ")
	if cfg.ScheduleCron == "" {
		cfg.ScheduleCron = defaultBackupSchedule
	}
	if err := validate.CronExpression(cfg.ScheduleCron); err != nil {
		return nil, validation(err)
	}
	if runs := nextRuns(cfg.ScheduleCron, 3); len(runs) < 2 {
		return nil, badRequest("This schedule never runs.")
	} else if runs[1].Sub(runs[0]) < minBackupInterval || (len(runs) > 2 && runs[2].Sub(runs[1]) < minBackupInterval) {
		return nil, badRequest("Backups can run at most once an hour.")
	}
	for _, k := range []int{req.KeepLast, req.KeepDaily, req.KeepWeekly, req.KeepMonthly} {
		if k < 0 || k > 1000 {
			return nil, badRequest("Retention values must be between 0 and 1000.")
		}
	}
	cfg.KeepLast, cfg.KeepDaily, cfg.KeepWeekly, cfg.KeepMonthly = req.KeepLast, req.KeepDaily, req.KeepWeekly, req.KeepMonthly
	if cfg.KeepLast+cfg.KeepDaily+cfg.KeepWeekly+cfg.KeepMonthly == 0 {
		return nil, badRequest("Keep at least one snapshot.")
	}
	cfg.ExcludeSiteIDs = cleanIDs(req.ExcludeSiteIDs)
	cfg.ExcludeDatabaseIDs = cleanIDs(req.ExcludeDatabaseIDs)
	cfg.NextRunAt = nil
	if cfg.Enabled {
		if runs := nextRuns(cfg.ScheduleCron, 1); len(runs) == 1 {
			t := runs[0].UTC()
			cfg.NextRunAt = &t
		}
	}
	return cfg, nil
}

func cleanIDs(ids []string) []string {
	out := []string{}
	for _, id := range ids {
		if validate.UUID(id) == nil && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func (s *Server) saveBackupConfig(w http.ResponseWriter, r *http.Request) error {
	p := current(r)
	var req backupConfigRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	cfg, err := s.mergeBackupConfig(ctx, p.User.Username, &req)
	if err != nil {
		return err
	}
	if err := s.db.SaveBackupConfig(ctx, cfg); err != nil {
		return err
	}
	s.snapshotsChanged(ctx, p.User.Username, nil)
	return s.getBackups(w, r)
}

const testScript = `
if out=$(rr cat config 2>&1 >/dev/null); then
  echo "@@STATUS ready"
elif grep -qiE 'does not exist|no such file|Is there a repository|repository not found|NoSuchKey|config: not found|unable to open config' <<< "$out"; then
  echo "@@STATUS empty"
else
  printf '%s\n' "$out" >&2
  exit 1
fi
`

func (s *Server) testBackupConfig(w http.ResponseWriter, r *http.Request) error {
	p := current(r)
	var req backupConfigRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	cfg, err := s.mergeBackupConfig(ctx, p.User.Username, &req)
	if err != nil {
		return err
	}
	repo, err := s.openRepo(cfg)
	if err != nil {
		return err
	}
	out, err := s.runRestic(ctx, repo, testScript, nil, 2*time.Minute, 64<<10)
	if err != nil {
		var ee *execError
		if errors.As(err, &ee) {
			return ok(w, map[string]any{"ok": false, "message": "Could not open the repository: " + ee.output})
		}
		return err
	}
	if strings.Contains(out, "@@STATUS empty") {
		return ok(w, map[string]any{"ok": true, "message": "The destination is reachable. A new repository will be created on the first backup."})
	}
	return ok(w, map[string]any{"ok": true, "message": "Connected. The repository exists and the password is correct."})
}

const sftpKeyScript = `
k="$HOME/` + sftpKeyFile + `"
mkdir -p "$HOME/.ssh"
chmod 700 "$HOME/.ssh"
[ -f "$k" ] || ssh-keygen -q -t ed25519 -N '' -C "fastcp-backup@$(hostname)" -f "$k"
printf '@@FASTCP-BEGIN@@\n%s\n@@FASTCP-END@@\n' "$(cat "$k.pub")"
`

// sftpKey returns the public key backups use for SFTP, creating it on
// first use.
func (s *Server) sftpKey(w http.ResponseWriter, r *http.Request) error {
	out, err := s.runScript(r.Context(), current(r).User.Username, nil, sftpKeyScript, nil, time.Minute, 64<<10)
	if err != nil {
		return errf(http.StatusBadGateway, "Could not create the SSH key: %s", describeExecErr(err))
	}
	key, ok2 := payload(out)
	if !ok2 {
		return errf(http.StatusBadGateway, "Could not read the SSH key.")
	}
	return ok(w, map[string]string{"public_key": strings.TrimSpace(key)})
}

// ---------------------------------------------------------------------------
// Jobs

func (s *Server) listBackupJobs(w http.ResponseWriter, r *http.Request) error {
	jobs, err := s.db.ListBackupJobs(r.Context(), current(r).User.Username, queryInt(r, "limit", 30, 1, keepJobs))
	if err != nil {
		return err
	}
	if jobs == nil {
		jobs = []*database.BackupJob{}
	}
	return ok(w, jobs)
}

func (s *Server) ownedJob(r *http.Request) (*database.BackupJob, error) {
	j, err := s.db.GetBackupJob(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, database.ErrNotFound) || (err == nil && j.Username != current(r).User.Username) {
		return nil, notFound("Job")
	}
	return j, err
}

func (s *Server) getBackupJob(w http.ResponseWriter, r *http.Request) error {
	j, err := s.ownedJob(r)
	if err != nil {
		return err
	}
	return ok(w, j)
}

// background runs fn as a tracked job that survives the request.
func (b *backupRunner) background(name string, fn func(ctx context.Context)) {
	b.s.wg.Add(1)
	go func() {
		defer b.s.wg.Done()
		defer func() {
			if v := recover(); v != nil {
				slog.Error("backup job panicked", "job", name, "panic", v)
			}
		}()
		select {
		case b.sem <- struct{}{}:
		case <-b.s.ctx.Done():
			return
		}
		defer func() { <-b.sem }()
		fn(b.s.ctx)
	}()
}

// finishCtx is used to record results even while shutting down.
func finishCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
}

// startJob records a non-backup job and runs it in the background.
func (b *backupRunner) startJob(ctx context.Context, username, jobType, snapshotID, message string, fn func(ctx context.Context) (string, string, error)) (*database.BackupJob, error) {
	if n, err := b.s.db.CountRunningBackupJobs(ctx, username, jobType); err != nil {
		return nil, err
	} else if n > 0 {
		return nil, conflict("Wait for the running %s to finish.", strings.ReplaceAll(jobType, "_", " "))
	}
	job := &database.BackupJob{ID: uuid.NewString(), Username: username, JobType: jobType, SnapshotID: snapshotID, Message: message}
	if err := b.s.db.CreateBackupJob(ctx, job); err != nil {
		return nil, err
	}
	b.background(jobType, func(ctx context.Context) {
		msg, artifact, err := fn(ctx)
		status := "success"
		if err != nil {
			status, msg = "failed", describeExecErr(err)
			slog.Warn("backup job failed", "type", jobType, "user", username, "error", err)
		}
		fctx, cancel := finishCtx(ctx)
		defer cancel()
		if err := b.s.db.FinishBackupJob(fctx, job.ID, status, msg, artifact); err != nil {
			slog.Error("could not record job result", "job", job.ID, "error", err)
		}
	})
	return job, nil
}

// ---------------------------------------------------------------------------
// Backup runs

func (s *Server) runBackup(w http.ResponseWriter, r *http.Request) error {
	job, err := s.backups.start(r.Context(), current(r).User.Username, "manual")
	if err != nil {
		return err
	}
	return accepted(w, job)
}

func (b *backupRunner) start(ctx context.Context, username, jobType string) (*database.BackupJob, error) {
	job := &database.BackupJob{ID: uuid.NewString(), Username: username, JobType: jobType, Message: "Backing up…"}
	cfg, err := b.s.db.ClaimBackupRun(ctx, username, job)
	if errors.Is(err, database.ErrNotFound) {
		return nil, badRequest("Set up a backup destination first.")
	}
	if err != nil {
		return nil, err
	}
	job.Status = "running"
	b.background("backup", func(ctx context.Context) {
		snapshot, msg, err := b.backup(ctx, cfg, job.ID)
		// Before the run is recorded as finished, which makes the panel
		// list the snapshots again.
		repo, _ := b.s.openRepo(cfg)
		b.s.snapshotsChanged(ctx, username, repo)
		status := "success"
		if err != nil {
			status = "failed"
			if msg == "" {
				msg = describeExecErr(err)
			} else {
				msg += " " + describeExecErr(err)
			}
			slog.Warn("backup failed", "user", username, "error", err)
		}
		var next *time.Time
		if cfg.Enabled {
			if runs := nextRuns(cfg.ScheduleCron, 1); len(runs) == 1 {
				t := runs[0].UTC()
				next = &t
			}
		}
		fctx, cancel := finishCtx(ctx)
		defer cancel()
		if err := b.s.db.FinishBackupRun(fctx, username, job.ID, status, snapshot, msg, next); err != nil {
			slog.Error("could not record backup result", "user", username, "error", err)
		}
	})
	return job, nil
}

const dumpScript = `
D="$HOME/` + backupDir + `/mysql-dumps"
mkdir -p "$D"
find "$D" -maxdepth 1 -type f -name '*.sql.gz*' -delete
for i in $(seq 0 $((FASTCP_DB_COUNT - 1))); do
  n="FASTCP_DB_NAME_$i"; u="FASTCP_DB_USER_$i"; p="FASTCP_DB_PASS_$i"
  name="${!n}"
  if MYSQL_PWD="${!p}" mysqldump --single-transaction --quick --lock-tables=false --no-tablespaces \
      -h 127.0.0.1 -u "${!u}" "$name" 2>"$D/.err" | gzip -c > "$D/$name.sql.gz.part"; then
    mv -f "$D/$name.sql.gz.part" "$D/$name.sql.gz"
    echo "@@DUMPED $name"
  else
    rm -f "$D/$name.sql.gz.part"
    echo "@@FAILED $name $(head -c 300 "$D/.err" | tr '\n' ' ')"
  fi
done
rm -f "$D/.err"
`

const backupScript = `
B="$HOME/` + backupDir + `"
printf '%s' "$FASTCP_MANIFEST" > "$B/manifest.json"
PATHS=(); TAGS=(); EXCL=()
while IFS= read -r p; do [ -n "$p" ] && [ -e "$p" ] && PATHS+=("$p"); done <<< "$FASTCP_PATHS"
while IFS= read -r t; do [ -n "$t" ] && TAGS+=(--tag "$t"); done <<< "$FASTCP_TAGS"
while IFS= read -r e; do [ -n "$e" ] && EXCL+=(--exclude "$e"); done <<< "${FASTCP_EXCLUDES:-}"
rr cat config >/dev/null 2>&1 || rr init >/dev/null
rr backup --host fastcp --json --quiet "${TAGS[@]}" "${EXCL[@]}" "${PATHS[@]}"
if [ -n "${FASTCP_FORGET:-}" ]; then
  mapfile -t KEEP <<< "$FASTCP_FORGET"
  rr forget --group-by '' --tag "fastcp,user:$FASTCP_OWNER" "${KEEP[@]}" >/dev/null
  if [ "${FASTCP_PRUNE:-0}" = 1 ]; then rr prune >/dev/null; echo "@@PRUNED"; fi
fi
`

// backup dumps the user's databases and backs up sites and dumps.
func (b *backupRunner) backup(ctx context.Context, cfg *database.BackupConfig, jobID string) (snapshot, msg string, err error) {
	s := b.s
	repo, err := s.openRepo(cfg)
	if err != nil {
		return "", "", err
	}
	sites, err := s.db.ListSites(ctx, cfg.Username)
	if err != nil {
		return "", "", err
	}
	dbs, err := s.db.ListDatabases(ctx, cfg.Username)
	if err != nil {
		return "", "", err
	}

	// Databases first.
	env := map[string]string{}
	var wanted []*database.Database
	var skipped []string
	for _, d := range dbs {
		if slices.Contains(cfg.ExcludeDatabaseIDs, d.ID) {
			continue
		}
		pass, perr := s.box.Open(crypto.PurposeDatabasePassword, d.PasswordEnc)
		if perr != nil || pass == "" {
			skipped = append(skipped, d.DBName+" (stored password unreadable; reset it)")
			continue
		}
		i := strconv.Itoa(len(wanted))
		env["FASTCP_DB_NAME_"+i], env["FASTCP_DB_USER_"+i], env["FASTCP_DB_PASS_"+i] = d.DBName, d.DBUser, pass
		wanted = append(wanted, d)
	}
	dumped := map[string]bool{}
	if len(wanted) > 0 {
		env["FASTCP_DB_COUNT"] = strconv.Itoa(len(wanted))
		out, err := s.runScript(ctx, cfg.Username, nil, dumpScript, env, 3*time.Hour, 256<<10)
		if err != nil {
			return "", "", fmt.Errorf("database dumps failed: %w", err)
		}
		for _, line := range strings.Split(out, "\n") {
			if name, ok := strings.CutPrefix(line, "@@DUMPED "); ok {
				dumped[strings.TrimSpace(name)] = true
			} else if rest, ok := strings.CutPrefix(line, "@@FAILED "); ok {
				skipped = append(skipped, strings.TrimSpace(rest))
			}
		}
	}

	manifest := map[string]any{"version": 1, "created_at": time.Now().UTC().Format(time.RFC3339), "username": cfg.Username}
	type mSite struct {
		Domain   string `json:"domain"`
		RootPath string `json:"root_path"`
	}
	type mDB struct {
		DBName   string `json:"db_name"`
		DumpPath string `json:"dump_path"`
	}
	mSites, mDBs := []mSite{}, []mDB{}
	var paths, tags, excludes []string
	tags = append(tags, "fastcp", "user:"+cfg.Username)
	for _, site := range sites {
		if slices.Contains(cfg.ExcludeSiteIDs, site.ID) || validate.Slug(site.Slug) != nil {
			continue
		}
		root := path.Join(repo.home, "apps", site.Slug)
		paths = append(paths, root)
		excludes = append(excludes, root+"/temp", root+"/logs")
		mSites = append(mSites, mSite{site.Domain, root})
		tags = append(tags, "site:"+site.ID, "site-domain:"+site.Domain)
	}
	dumpDir := path.Join(repo.home, backupDir, "mysql-dumps")
	for _, d := range wanted {
		if dumped[d.DBName] {
			mDBs = append(mDBs, mDB{d.DBName, path.Join(dumpDir, d.DBName+".sql.gz")})
			tags = append(tags, "db:"+d.ID, "db-name:"+d.DBName)
		}
	}
	if len(mSites) == 0 && len(mDBs) == 0 {
		if len(skipped) > 0 {
			return "", "", errors.New("nothing was backed up: " + strings.Join(skipped, "; "))
		}
		return "", "", errors.New("there is nothing to back up; every site and database is excluded")
	}
	manifest["sites"], manifest["databases"] = mSites, mDBs
	mj, _ := json.Marshal(manifest)
	paths = append(paths, path.Join(repo.home, backupDir, "manifest.json"))
	if len(mDBs) > 0 {
		paths = append(paths, dumpDir)
	}

	env = map[string]string{
		"FASTCP_MANIFEST": string(mj), "FASTCP_PATHS": strings.Join(paths, "\n"),
		"FASTCP_TAGS": strings.Join(tags, "\n"), "FASTCP_EXCLUDES": strings.Join(excludes, "\n"),
		"FASTCP_OWNER": cfg.Username,
	}
	var keep []string
	for _, k := range []struct {
		flag string
		n    int
	}{{"--keep-last", cfg.KeepLast}, {"--keep-daily", cfg.KeepDaily}, {"--keep-weekly", cfg.KeepWeekly}, {"--keep-monthly", cfg.KeepMonthly}} {
		if k.n > 0 {
			keep = append(keep, k.flag, strconv.Itoa(k.n))
		}
	}
	env["FASTCP_FORGET"] = strings.Join(keep, "\n")
	prune := len(keep) > 0 && (cfg.LastPruneAt == nil || time.Since(*cfg.LastPruneAt) > pruneInterval)
	if prune {
		env["FASTCP_PRUNE"] = "1"
	}
	out, err := s.runRestic(ctx, repo, backupScript, env, 6*time.Hour, 512<<10)
	if err != nil {
		return "", "", err
	}
	if strings.Contains(out, "@@PRUNED") {
		if err := s.db.SetBackupPruned(ctx, cfg.Username, time.Now()); err != nil {
			slog.Warn("could not record prune time", "user", cfg.Username, "error", err)
		}
	}
	snapshot = parseSnapshotID(out)
	msg = fmt.Sprintf("Backed up %s and %s.", plural(len(mSites), "site"), plural(len(mDBs), "database"))
	if len(skipped) > 0 {
		return snapshot, msg, fmt.Errorf("some databases were not backed up: %s", strings.Join(skipped, "; "))
	}
	return snapshot, msg, nil
}

func parseSnapshotID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var m struct {
			MessageType string `json:"message_type"`
			SnapshotID  string `json:"snapshot_id"`
		}
		if json.Unmarshal([]byte(line), &m) == nil && m.MessageType == "summary" && m.SnapshotID != "" {
			return m.SnapshotID
		}
	}
	return ""
}

func (b *backupRunner) schedulerLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		due, err := b.s.db.DueBackups(ctx, time.Now())
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("could not list due backups", "error", err)
			}
			continue
		}
		for _, username := range due {
			if _, err := b.start(ctx, username, "scheduled"); err != nil && !errors.Is(err, database.ErrBusy) {
				slog.Warn("could not start scheduled backup", "user", username, "error", err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Snapshots

func (s *Server) userRepo(ctx context.Context, username string) (*resticRepo, error) {
	cfg, saved, err := s.backupConfig(ctx, username)
	if err != nil {
		return nil, err
	}
	if !saved || cfg.Repository == "" {
		return nil, badRequest("Set up a backup destination first.")
	}
	return s.openRepo(cfg)
}

func (s *Server) listSnapshots(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	cfg, saved, err := s.backupConfig(ctx, current(r).User.Username)
	if err != nil {
		return err
	}
	if !saved || cfg.Repository == "" {
		return ok(w, []snapshotView{})
	}
	repo, err := s.openRepo(cfg)
	if err != nil {
		return err
	}
	list, err := s.cachedSnapshots(ctx, repo)
	if err != nil {
		return errf(http.StatusBadGateway, "Could not list snapshots: %s", describeExecErr(err))
	}
	limit := queryInt(r, "limit", 100, 1, 1000)
	out := make([]snapshotView, 0, min(len(list), limit))
	for i, sn := range list {
		if i >= limit {
			break
		}
		out = append(out, viewSnapshot(sn))
	}
	return ok(w, out)
}

func snapshotParam(r *http.Request) (string, error) {
	id := chi.URLParam(r, "snapshotID")
	if err := validate.SnapshotID(id); err != nil {
		return "", notFound("Snapshot")
	}
	return id, nil
}

func (s *Server) deleteSnapshot(w http.ResponseWriter, r *http.Request) error {
	username := current(r).User.Username
	id, err := snapshotParam(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	repo, err := s.userRepo(ctx, username)
	if err != nil {
		return err
	}
	sn, err := s.findSnapshot(ctx, repo, id)
	if err != nil {
		return err
	}
	job, err := s.backups.startJob(ctx, username, "delete_snapshot", sn.ID, "Deleting snapshot "+sn.ShortID+"…", func(ctx context.Context) (string, string, error) {
		_, err := s.runRestic(ctx, repo, `rr forget "$FASTCP_SNAPSHOT" >/dev/null`, map[string]string{"FASTCP_SNAPSHOT": sn.ID}, 30*time.Minute, 0)
		s.snapshotsChanged(ctx, username, repo)
		return "Deleted snapshot " + sn.ShortID + ". Its data is freed at the next prune.", "", err
	})
	if err != nil {
		return err
	}
	return accepted(w, job)
}

const restoreSiteScript = `
case "$FASTCP_DST" in "$HOME"/apps/?*) ;; *) echo "Invalid restore target." >&2; exit 2;; esac
T="$HOME/` + backupDir + `/restore-$FASTCP_JOB"
rm -rf "$T"; mkdir -p "$T"
trap 'rm -rf "$T"' EXIT
rr restore "$FASTCP_SNAPSHOT" --target "$T" --include "$FASTCP_SRC" >/dev/null
[ -d "$T$FASTCP_SRC" ] || { echo "The snapshot does not contain $FASTCP_SRC." >&2; exit 3; }
mkdir -p "$FASTCP_DST"
rsync -a --delete --exclude /logs --exclude /temp "$T$FASTCP_SRC/" "$FASTCP_DST/"
`

const restoreDBScript = `
MYSQL_PWD="$FASTCP_DB_PASS" ; export MYSQL_PWD
rr dump "$FASTCP_SNAPSHOT" "$FASTCP_SRC" | gunzip -c | mysql -h 127.0.0.1 -u "$FASTCP_DB_USER" "$FASTCP_DB_NAME"
`

func (s *Server) restoreSnapshot(w http.ResponseWriter, r *http.Request) error {
	username := current(r).User.Username
	id, err := snapshotParam(r)
	if err != nil {
		return err
	}
	var req struct {
		Type       string `json:"type"` // site | database
		SiteID     string `json:"site_id"`
		DatabaseID string `json:"database_id"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	repo, err := s.userRepo(ctx, username)
	if err != nil {
		return err
	}

	switch req.Type {
	case "site":
		site, err := s.db.GetSite(ctx, req.SiteID)
		if err != nil || site.Username != username {
			return notFound("Site")
		}
		sn, err := s.findSnapshot(ctx, repo, id)
		if err != nil {
			return err
		}
		job, err := s.backups.startJob(ctx, username, "restore_site", sn.ID, "Restoring "+site.Domain+"…", func(ctx context.Context) (string, string, error) {
			m, err := s.loadManifest(ctx, repo, sn.ID)
			if err != nil {
				return "", "", err
			}
			src := ""
			for _, ms := range m.Sites {
				if strings.EqualFold(ms.Domain, site.Domain) || path.Base(ms.RootPath) == site.Slug {
					src = ms.RootPath
					break
				}
			}
			if src == "" {
				return "", "", fmt.Errorf("snapshot %s does not include %s", sn.ShortID, site.Domain)
			}
			env := map[string]string{"FASTCP_SNAPSHOT": sn.ID, "FASTCP_SRC": src,
				"FASTCP_DST": path.Join(repo.home, "apps", site.Slug), "FASTCP_JOB": uuid.NewString()}
			if _, err := s.runRestic(ctx, repo, restoreSiteScript, env, 6*time.Hour, 0); err != nil {
				return "", "", err
			}
			return fmt.Sprintf("Restored %s from snapshot %s.", site.Domain, sn.ShortID), "", nil
		})
		if err != nil {
			return err
		}
		return accepted(w, job)

	case "database":
		d, err := s.db.GetDatabase(ctx, req.DatabaseID)
		if err != nil || d.Username != username {
			return notFound("Database")
		}
		pass, err := s.box.Open(crypto.PurposeDatabasePassword, d.PasswordEnc)
		if err != nil || pass == "" {
			return conflict("The stored password for %s cannot be read. Reset the password, then try again.", d.DBName)
		}
		sn, err := s.findSnapshot(ctx, repo, id)
		if err != nil {
			return err
		}
		job, err := s.backups.startJob(ctx, username, "restore_database", sn.ID, "Restoring "+d.DBName+"…", func(ctx context.Context) (string, string, error) {
			m, err := s.loadManifest(ctx, repo, sn.ID)
			if err != nil {
				return "", "", err
			}
			src := ""
			for _, md := range m.Databases {
				if md.DBName == d.DBName {
					src = md.DumpPath
				}
			}
			if src == "" {
				return "", "", fmt.Errorf("snapshot %s does not include %s", sn.ShortID, d.DBName)
			}
			env := map[string]string{"FASTCP_SNAPSHOT": sn.ID, "FASTCP_SRC": src, "FASTCP_DB_USER": d.DBUser,
				"FASTCP_DB_NAME": d.DBName, "FASTCP_DB_PASS": pass}
			if _, err := s.runRestic(ctx, repo, restoreDBScript, env, 6*time.Hour, 0); err != nil {
				return "", "", err
			}
			return fmt.Sprintf("Restored %s from snapshot %s.", d.DBName, sn.ShortID), "", nil
		})
		if err != nil {
			return err
		}
		return accepted(w, job)
	}
	return badRequest("Choose what to restore: site or database.")
}

// ---------------------------------------------------------------------------
// Exports

const statsScript = `capture rr stats --json "$FASTCP_SNAPSHOT"`

const exportScript = `
rr dump "$FASTCP_SNAPSHOT" / | "$FASTCP_AGENT" export-zip --out "$FASTCP_OUT"
`

type exportEntry struct {
	Src string `json:"src"`
	Dst string `json:"dst"`
}

var unsafeName = strings.NewReplacer("/", "_", "\\", "_", "..", "_", " ", "_")

func (s *Server) exportSnapshot(w http.ResponseWriter, r *http.Request) error {
	username := current(r).User.Username
	id, err := snapshotParam(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	repo, err := s.userRepo(ctx, username)
	if err != nil {
		return err
	}
	sn, err := s.findSnapshot(ctx, repo, id)
	if err != nil {
		return err
	}
	job, err := s.backups.startJob(ctx, username, "export", sn.ID, "Preparing a download of snapshot "+sn.ShortID+"…", func(ctx context.Context) (string, string, error) {
		m, err := s.loadManifest(ctx, repo, sn.ID)
		if err != nil {
			return "", "", err
		}
		home := strings.TrimPrefix(repo.home, "/")
		entries := []exportEntry{{Src: path.Join(home, backupDir, "manifest.json"), Dst: "manifest.json"}}
		for _, ms := range m.Sites {
			entries = append(entries, exportEntry{Src: strings.TrimPrefix(ms.RootPath, "/"), Dst: "websites/" + unsafeName.Replace(ms.Domain)})
		}
		for _, md := range m.Databases {
			entries = append(entries, exportEntry{Src: strings.TrimPrefix(md.DumpPath, "/"), Dst: "databases/" + unsafeName.Replace(md.DBName) + ".sql.gz"})
		}
		mapJSON, _ := json.Marshal(entries)

		dir, err := s.agent.PrepareSpool(ctx, username)
		if err != nil {
			return "", "", err
		}
		if out, err := s.runRestic(ctx, repo, statsScript, map[string]string{"FASTCP_SNAPSHOT": sn.ID}, 30*time.Minute, 64<<10); err == nil {
			if body, ok := payload(out); ok {
				var st struct {
					TotalSize uint64 `json:"total_size"`
				}
				if json.Unmarshal([]byte(body), &st) == nil && st.TotalSize > 0 {
					if free, ok := freeBytes(dir); ok && st.TotalSize+512<<20 > free {
						return "", "", fmt.Errorf("not enough free disk space for the export (needs about %s, %s free)", humanBytes(st.TotalSize), humanBytes(free))
					}
				}
			}
		}
		name := fmt.Sprintf("fastcp-%s-%s-%s.zip", username, sn.Time.UTC().Format("20060102-150405"), sn.ShortID)
		out := filepath.Join(dir, name)
		env := map[string]string{"FASTCP_SNAPSHOT": sn.ID, "FASTCP_OUT": out, "FASTCP_AGENT": s.cfg.AgentBinary,
			"FASTCP_EXPORT_MAP": string(mapJSON)}
		if _, err := s.runRestic(ctx, repo, exportScript, env, 6*time.Hour, 0); err != nil {
			_ = os.Remove(out)
			return "", "", err
		}
		return "The download is ready for one hour.", out, nil
	})
	if err != nil {
		return err
	}
	return accepted(w, job)
}

func (s *Server) exportDownloadLink(w http.ResponseWriter, r *http.Request) error {
	j, err := s.ownedJob(r)
	if err != nil {
		return err
	}
	if j.JobType != "export" || j.Status != "success" || j.Artifact == "" {
		return notFound("Download")
	}
	token := s.downloads.put(downloadGrant{Username: j.Username, JobID: j.ID, Path: j.Artifact, Name: filepath.Base(j.Artifact)}, downloadTTL)
	return ok(w, map[string]string{"url": "/api/backups/download?token=" + token})
}

// downloadExport streams an export file. The single-use token is the
// credential because browsers cannot attach headers to a download.
func (s *Server) downloadExport(w http.ResponseWriter, r *http.Request) {
	g, valid := s.downloads.take(r.URL.Query().Get("token"))
	if !valid {
		s.fail(w, r, errf(http.StatusForbidden, "This download link has expired. Create a new one from the Backups page."))
		return
	}
	clean := filepath.Clean(g.Path)
	if filepath.Dir(filepath.Dir(clean)) != agent.ExportSpoolDir || filepath.Base(filepath.Dir(clean)) != g.Username {
		s.fail(w, r, notFound("Download"))
		return
	}
	f, err := os.OpenFile(clean, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		s.fail(w, r, notFound("Download"))
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		s.fail(w, r, notFound("Download"))
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(12 * time.Hour))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": g.Name}))
	http.ServeContent(w, r, g.Name, st.ModTime(), f)
}

// cleanExports removes export files after exportTTL and trims job history.
func (b *backupRunner) cleanExports(ctx context.Context) {
	jobs, err := b.s.db.ExportsWithArtifacts(ctx)
	if err != nil {
		return
	}
	for _, j := range jobs {
		if j.FinishedAt == nil || time.Since(*j.FinishedAt) < exportTTL {
			continue
		}
		clean := filepath.Clean(j.Artifact)
		if filepath.Dir(filepath.Dir(clean)) == agent.ExportSpoolDir {
			if err := os.Remove(clean); err != nil && !os.IsNotExist(err) {
				slog.Warn("could not remove expired export", "file", clean, "error", err)
				continue
			}
		}
		_ = b.s.db.ClearBackupArtifact(ctx, j.ID)
	}
	_ = b.s.db.PruneBackupJobs(ctx, keepJobs)
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func freeBytes(dir string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true
}
