package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// BackupConfig is a user's backup destination, schedule and retention.
type BackupConfig struct {
	Username           string
	BackendType        string // local | sftp | s3 | rclone
	Repository         string
	PasswordEnc        string
	SFTPUsername       string
	SFTPHost           string
	SFTPPort           int
	SFTPPath           string
	S3Endpoint         string
	S3Bucket           string
	S3Prefix           string
	S3Region           string
	S3BucketLookup     string
	S3ListObjectsV1    bool
	S3AccessKeyEnc     string
	S3SecretKeyEnc     string
	S3SessionTokenEnc  string
	Enabled            bool
	ScheduleCron       string
	ExcludeSiteIDs     []string
	ExcludeDatabaseIDs []string
	KeepLast           int
	KeepDaily          int
	KeepWeekly         int
	KeepMonthly        int
	LastRunAt          *time.Time
	NextRunAt          *time.Time
	LastPruneAt        *time.Time
	LastStatus         string
	LastMessage        string
	RunningJobID       string
	RunningStartedAt   *time.Time
}

const backupConfigColumns = `username, COALESCE(backend_type, 'sftp'), repository, password_enc,
	COALESCE(sftp_username, ''), COALESCE(sftp_host, ''), COALESCE(sftp_port, 22), COALESCE(sftp_path, ''),
	COALESCE(s3_endpoint, ''), COALESCE(s3_bucket, ''), COALESCE(s3_prefix, ''), COALESCE(s3_region, ''),
	COALESCE(s3_bucket_lookup, 'auto'), COALESCE(s3_list_objects_v1, 0),
	COALESCE(s3_access_key_enc, ''), COALESCE(s3_secret_key_enc, ''), COALESCE(s3_session_token_enc, ''),
	COALESCE(enabled, 0), schedule_cron, COALESCE(exclude_site_ids, '[]'), COALESCE(exclude_database_ids, '[]'),
	keep_last, keep_daily, keep_weekly, keep_monthly,
	last_run_at, next_run_at, last_prune_at, COALESCE(last_status, 'idle'), COALESCE(last_message, ''),
	COALESCE(running_job_id, ''), running_started_at`

func timePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	u := t.Time.UTC()
	return &u
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}

func scanBackupConfig(sc scanner) (*BackupConfig, error) {
	var c BackupConfig
	var sites, dbs string
	var lastRun, nextRun, lastPrune, runningStart sql.NullTime
	err := sc.Scan(&c.Username, &c.BackendType, &c.Repository, &c.PasswordEnc,
		&c.SFTPUsername, &c.SFTPHost, &c.SFTPPort, &c.SFTPPath,
		&c.S3Endpoint, &c.S3Bucket, &c.S3Prefix, &c.S3Region,
		&c.S3BucketLookup, &c.S3ListObjectsV1,
		&c.S3AccessKeyEnc, &c.S3SecretKeyEnc, &c.S3SessionTokenEnc,
		&c.Enabled, &c.ScheduleCron, &sites, &dbs,
		&c.KeepLast, &c.KeepDaily, &c.KeepWeekly, &c.KeepMonthly,
		&lastRun, &nextRun, &lastPrune, &c.LastStatus, &c.LastMessage,
		&c.RunningJobID, &runningStart)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(sites), &c.ExcludeSiteIDs)
	_ = json.Unmarshal([]byte(dbs), &c.ExcludeDatabaseIDs)
	c.LastRunAt, c.NextRunAt, c.LastPruneAt, c.RunningStartedAt = timePtr(lastRun), timePtr(nextRun), timePtr(lastPrune), timePtr(runningStart)
	return &c, nil
}

func (db *DB) GetBackupConfig(ctx context.Context, username string) (*BackupConfig, error) {
	c, err := scanBackupConfig(db.QueryRowContext(ctx, "SELECT "+backupConfigColumns+" FROM backup_configs WHERE username = ?", username))
	return c, notFound(err)
}

func jsonList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// SaveBackupConfig stores the destination and policy. Run state is kept.
func (db *DB) SaveBackupConfig(ctx context.Context, c *BackupConfig) error {
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `INSERT INTO backup_configs (
			username, backend_type, repository, password_enc,
			sftp_username, sftp_host, sftp_port, sftp_path,
			s3_endpoint, s3_bucket, s3_prefix, s3_region, s3_bucket_lookup, s3_list_objects_v1,
			s3_access_key_enc, s3_secret_key_enc, s3_session_token_enc,
			enabled, schedule_cron, exclude_site_ids, exclude_database_ids,
			keep_last, keep_daily, keep_weekly, keep_monthly, next_run_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET
			backend_type = excluded.backend_type, repository = excluded.repository, password_enc = excluded.password_enc,
			sftp_username = excluded.sftp_username, sftp_host = excluded.sftp_host, sftp_port = excluded.sftp_port, sftp_path = excluded.sftp_path,
			s3_endpoint = excluded.s3_endpoint, s3_bucket = excluded.s3_bucket, s3_prefix = excluded.s3_prefix, s3_region = excluded.s3_region,
			s3_bucket_lookup = excluded.s3_bucket_lookup, s3_list_objects_v1 = excluded.s3_list_objects_v1,
			s3_access_key_enc = excluded.s3_access_key_enc, s3_secret_key_enc = excluded.s3_secret_key_enc,
			s3_session_token_enc = excluded.s3_session_token_enc,
			enabled = excluded.enabled, schedule_cron = excluded.schedule_cron,
			exclude_site_ids = excluded.exclude_site_ids, exclude_database_ids = excluded.exclude_database_ids,
			keep_last = excluded.keep_last, keep_daily = excluded.keep_daily, keep_weekly = excluded.keep_weekly,
			keep_monthly = excluded.keep_monthly, next_run_at = excluded.next_run_at, updated_at = excluded.updated_at`,
		c.Username, c.BackendType, c.Repository, c.PasswordEnc,
		c.SFTPUsername, c.SFTPHost, c.SFTPPort, c.SFTPPath,
		c.S3Endpoint, c.S3Bucket, c.S3Prefix, c.S3Region, c.S3BucketLookup, c.S3ListObjectsV1,
		c.S3AccessKeyEnc, c.S3SecretKeyEnc, c.S3SessionTokenEnc,
		c.Enabled, c.ScheduleCron, jsonList(c.ExcludeSiteIDs), jsonList(c.ExcludeDatabaseIDs),
		c.KeepLast, c.KeepDaily, c.KeepWeekly, c.KeepMonthly, nullTime(c.NextRunAt), now, now)
	return err
}

// DueBackups lists users whose scheduled backup is due.
func (db *DB) DueBackups(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT username FROM backup_configs
		WHERE enabled = 1 AND repository <> '' AND running_job_id = '' AND next_run_at IS NOT NULL
		  AND datetime(next_run_at) <= datetime(?)
		ORDER BY next_run_at LIMIT 50`, now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ErrBusy is returned when a backup is already running for the user.
var ErrBusy = errors.New("a backup is already running")

// ClaimBackupRun marks a backup as running and records the job. It fails
// with ErrBusy if another run holds the claim.
func (db *DB) ClaimBackupRun(ctx context.Context, username string, job *BackupJob) (*BackupConfig, error) {
	var cfg *BackupConfig
	err := db.InTx(ctx, func(tx *sql.Tx) error {
		c, err := scanBackupConfig(tx.QueryRowContext(ctx, "SELECT "+backupConfigColumns+" FROM backup_configs WHERE username = ?", username))
		if err != nil {
			return notFound(err)
		}
		if c.RunningJobID != "" {
			return ErrBusy
		}
		job.StartedAt = time.Now().UTC()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO backup_jobs (id, username, job_type, status, message, started_at, created_at) VALUES (?, ?, ?, 'running', ?, ?, ?)`,
			job.ID, username, job.JobType, job.Message, job.StartedAt, job.StartedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE backup_configs SET running_job_id = ?, running_started_at = ?, last_status = 'running', last_message = ?, updated_at = ?
			 WHERE username = ?`, job.ID, job.StartedAt, job.Message, job.StartedAt, username); err != nil {
			return err
		}
		cfg = c
		return nil
	})
	return cfg, err
}

// FinishBackupRun records the outcome of a claimed run.
func (db *DB) FinishBackupRun(ctx context.Context, username, jobID, status, snapshotID, message string, nextRun *time.Time) error {
	now := time.Now().UTC()
	return db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			"UPDATE backup_jobs SET status = ?, snapshot_id = ?, message = ?, finished_at = ? WHERE id = ?",
			status, snapshotID, message, now, jobID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`UPDATE backup_configs SET running_job_id = '', running_started_at = NULL, last_status = ?, last_message = ?,
				last_run_at = ?, next_run_at = ?, updated_at = ?
			 WHERE username = ? AND running_job_id = ?`,
			status, message, now, nullTime(nextRun), now, username, jobID)
		return err
	})
}

func (db *DB) SetBackupPruned(ctx context.Context, username string, at time.Time) error {
	_, err := db.ExecContext(ctx, "UPDATE backup_configs SET last_prune_at = ? WHERE username = ?", at.UTC(), username)
	return err
}

// ResetInterruptedBackups fails jobs that were running when the panel
// stopped and releases their claims.
func (db *DB) ResetInterruptedBackups(ctx context.Context) error {
	now := time.Now().UTC()
	return db.InTx(ctx, func(tx *sql.Tx) error {
		msg := "Interrupted because the control panel restarted."
		if _, err := tx.ExecContext(ctx,
			"UPDATE backup_jobs SET status = 'failed', message = ?, finished_at = ? WHERE status = 'running'", msg, now); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`UPDATE backup_configs SET running_job_id = '', running_started_at = NULL, last_status = 'failed', last_message = ?
			 WHERE running_job_id <> ''`, msg)
		return err
	})
}

// BackupJob is a backup, restore, delete or export operation.
type BackupJob struct {
	ID         string     `json:"id"`
	Username   string     `json:"username"`
	JobType    string     `json:"job_type"`
	Status     string     `json:"status"` // running | success | failed
	SnapshotID string     `json:"snapshot_id,omitempty"`
	Message    string     `json:"message,omitempty"`
	Artifact   string     `json:"artifact,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

const backupJobColumns = "id, username, job_type, status, COALESCE(snapshot_id, ''), COALESCE(message, ''), COALESCE(artifact, ''), started_at, finished_at"

func scanBackupJob(sc scanner) (*BackupJob, error) {
	var j BackupJob
	var started, finished sql.NullTime
	if err := sc.Scan(&j.ID, &j.Username, &j.JobType, &j.Status, &j.SnapshotID, &j.Message, &j.Artifact, &started, &finished); err != nil {
		return nil, err
	}
	j.StartedAt = started.Time.UTC()
	j.FinishedAt = timePtr(finished)
	return &j, nil
}

func (db *DB) CreateBackupJob(ctx context.Context, j *BackupJob) error {
	j.StartedAt = time.Now().UTC()
	j.Status = "running"
	_, err := db.ExecContext(ctx,
		`INSERT INTO backup_jobs (id, username, job_type, status, snapshot_id, message, started_at, created_at) VALUES (?, ?, ?, 'running', ?, ?, ?, ?)`,
		j.ID, j.Username, j.JobType, j.SnapshotID, j.Message, j.StartedAt, j.StartedAt)
	return err
}

func (db *DB) FinishBackupJob(ctx context.Context, id, status, message, artifact string) error {
	_, err := db.ExecContext(ctx,
		"UPDATE backup_jobs SET status = ?, message = ?, artifact = ?, finished_at = ? WHERE id = ?",
		status, message, artifact, time.Now().UTC(), id)
	return err
}

func (db *DB) GetBackupJob(ctx context.Context, id string) (*BackupJob, error) {
	j, err := scanBackupJob(db.QueryRowContext(ctx, "SELECT "+backupJobColumns+" FROM backup_jobs WHERE id = ?", id))
	return j, notFound(err)
}

func (db *DB) ListBackupJobs(ctx context.Context, username string, limit int) ([]*BackupJob, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+backupJobColumns+" FROM backup_jobs WHERE username = ? ORDER BY started_at DESC LIMIT ?", username, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BackupJob
	for rows.Next() {
		j, err := scanBackupJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// CountRunningBackupJobs counts a user's running jobs of one type.
func (db *DB) CountRunningBackupJobs(ctx context.Context, username, jobType string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM backup_jobs WHERE username = ? AND job_type = ? AND status = 'running'",
		username, jobType).Scan(&n)
	return n, err
}

// ClearBackupArtifact forgets an export file that has been removed.
func (db *DB) ClearBackupArtifact(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, "UPDATE backup_jobs SET artifact = '' WHERE id = ?", id)
	return err
}

// ExportsWithArtifacts lists finished export jobs that still have a file.
func (db *DB) ExportsWithArtifacts(ctx context.Context) ([]*BackupJob, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+backupJobColumns+" FROM backup_jobs WHERE artifact <> ''")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BackupJob
	for rows.Next() {
		j, err := scanBackupJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// PruneBackupJobs keeps the newest keep jobs per user.
func (db *DB) PruneBackupJobs(ctx context.Context, keep int) error {
	_, err := db.ExecContext(ctx, `DELETE FROM backup_jobs WHERE status <> 'running' AND artifact = '' AND id IN (
		SELECT id FROM (
			SELECT id, ROW_NUMBER() OVER (PARTITION BY username ORDER BY started_at DESC) AS rn FROM backup_jobs
		) WHERE rn > ?)`, keep)
	return err
}
