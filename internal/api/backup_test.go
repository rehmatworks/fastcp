package api

import (
	"testing"

	"github.com/rehmatworks/fastcp/internal/database"
)

func TestBuildRepository(t *testing.T) {
	good := []struct {
		cfg    database.BackupConfig
		local  string
		rclone string
		want   string
	}{
		{database.BackupConfig{BackendType: "local"}, "/srv/backups//alice/", "", "/srv/backups/alice"},
		{database.BackupConfig{BackendType: "sftp", SFTPUsername: "u1", SFTPHost: "backup.example.com", SFTPPath: "/data/repo"}, "", "", "sftp://u1@backup.example.com:22//data/repo"},
		{database.BackupConfig{BackendType: "sftp", SFTPUsername: "u1", SFTPHost: "2001:db8::1", SFTPPort: 2222, SFTPPath: "/r"}, "", "", "sftp://u1@[2001:db8::1]:2222//r"},
		{database.BackupConfig{BackendType: "s3", S3Endpoint: "https://s3.amazonaws.com/", S3Bucket: "my-bucket", S3Prefix: "/fastcp/"}, "", "", "s3:https://s3.amazonaws.com/my-bucket/fastcp"},
		{database.BackupConfig{BackendType: "rclone"}, "", "rclone:gdrive:fastcp", "rclone:gdrive:fastcp"},
	}
	for _, c := range good {
		cfg := c.cfg
		if err := buildRepository(&cfg, c.local, c.rclone); err != nil {
			t.Errorf("%+v: %v", c.cfg, err)
			continue
		}
		if cfg.Repository != c.want {
			t.Errorf("repository = %q, want %q", cfg.Repository, c.want)
		}
	}

	bad := []struct {
		cfg           database.BackupConfig
		local, rclone string
	}{
		{database.BackupConfig{BackendType: "local"}, "relative/path", ""},
		{database.BackupConfig{BackendType: "local"}, "/srv/../etc", ""},
		{database.BackupConfig{BackendType: "sftp", SFTPUsername: "u -oProxyCommand=x", SFTPHost: "h.example.com", SFTPPath: "/r"}, "", ""},
		{database.BackupConfig{BackendType: "sftp", SFTPUsername: "u", SFTPHost: "-oProxyCommand=sh", SFTPPath: "/r"}, "", ""},
		{database.BackupConfig{BackendType: "s3", S3Endpoint: "https://s3.amazonaws.com", S3Bucket: "b", S3Prefix: "../x"}, "", ""},
		{database.BackupConfig{BackendType: "s3", S3Endpoint: "https://s3.amazonaws.com", S3Bucket: "Bad Bucket"}, "", ""},
		{database.BackupConfig{BackendType: "rclone"}, "", "rclone:remote:path; rm -rf /"},
		{database.BackupConfig{BackendType: "ftp"}, "", ""},
	}
	for _, c := range bad {
		cfg := c.cfg
		if err := buildRepository(&cfg, c.local, c.rclone); err == nil {
			t.Errorf("accepted %+v local=%q rclone=%q -> %q", c.cfg, c.local, c.rclone, cfg.Repository)
		}
	}
}

func TestParseSnapshotID(t *testing.T) {
	out := `{"message_type":"status","percent_done":0.5}
not json
{"message_type":"summary","files_new":3,"snapshot_id":"4f2a9c1d8e"}
@@PRUNED`
	if got := parseSnapshotID(out); got != "4f2a9c1d8e" {
		t.Fatalf("parseSnapshotID = %q", got)
	}
	if got := parseSnapshotID(`{"message_type":"status"}`); got != "" {
		t.Fatalf("parseSnapshotID without summary = %q", got)
	}
}

func TestPrefixLocation(t *testing.T) {
	for in, want := range map[string]string{
		"/index.php?route=/":                  "/phpmyadmin/index.php?route=/",
		"/phpmyadmin/index.php":               "/phpmyadmin/index.php",
		"http://127.0.0.1:2088/index.php?x=1": "/phpmyadmin/index.php?x=1",
		"https://evil.example/":               "/phpmyadmin/",
		"index.php":                           "index.php",
	} {
		if got := prefixLocation(in); got != want {
			t.Errorf("prefixLocation(%q) = %q, want %q", in, got, want)
		}
	}
}
