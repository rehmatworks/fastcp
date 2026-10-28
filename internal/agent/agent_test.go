package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rehmatworks/fastcp/internal/safefs"
)

const ufwNumbered = `Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 22/tcp                     ALLOW IN    Anywhere
[ 2] 80/tcp                     ALLOW IN    Anywhere
[ 3] 8080/tcp                   DENY IN     Anywhere
[ 4] 3306                       ALLOW IN    10.0.0.0/8
[ 5] OpenSSH                    ALLOW IN    Anywhere
[ 6] 22/tcp (v6)                ALLOW IN    Anywhere (v6)
[ 7] 8080/tcp (v6)              DENY IN     Anywhere (v6)
[ 8] 53/udp                     ALLOW OUT   Anywhere
`

func TestParseUFWNumbered(t *testing.T) {
	rules := parseUFWNumbered(ufwNumbered)
	if len(rules) != 8 {
		t.Fatalf("got %d rules", len(rules))
	}
	want := ufwRule{Num: 4, To: "3306", Action: "ALLOW", Direction: "IN", From: "10.0.0.0/8", Port: 3306, Protocol: "any", IPVersion: "ipv4"}
	if rules[3] != want {
		t.Fatalf("rule 4 = %+v", rules[3])
	}
	if rules[4].Port != 0 || rules[5].IPVersion != "ipv6" || rules[7].Direction != "OUT" {
		t.Fatalf("unexpected parse: %+v", rules)
	}
	if !ufwStatusActive(ufwNumbered) || ufwStatusActive("Status: inactive") {
		t.Fatal("status detection")
	}
	if got := matchingRules(rules, 8080, "tcp", "", "both"); !reflect.DeepEqual(got, []int{7, 3}) {
		t.Fatalf("matching 8080 = %v", got)
	}
	if got := matchingRules(rules, 8080, "tcp", "DENY", "ipv6"); !reflect.DeepEqual(got, []int{7}) {
		t.Fatalf("matching 8080 v6 = %v", got)
	}
	if got := matchingRules(rules, 3306, "udp", "", "both"); !reflect.DeepEqual(got, []int{4}) {
		t.Fatalf("any-protocol rule should match udp: %v", got)
	}
	if got := matchingRules(rules, 53, "udp", "", "both"); len(got) != 0 {
		t.Fatalf("outgoing rules must not match: %v", got)
	}
}

func TestParseAptPolicyAll(t *testing.T) {
	out := `php8.3-fpm:
  Installed: (none)
  Candidate: 8.3.27-1+ubuntu24.04.1+deb.sury.org+1
  Version table:
     8.3.27-1+ubuntu24.04.1+deb.sury.org+1 500
php8.4-fpm:
  Installed: 8.4.13-1+ubuntu24.04.1+deb.sury.org+1
  Candidate: 8.4.14-1+ubuntu24.04.1+deb.sury.org+1
`
	got := parseAptPolicyAll(out)
	if got["php8.3-fpm"][0] != "" || upstreamVersion(got["php8.3-fpm"][1]) != "8.3.27" {
		t.Fatalf("8.3 = %v", got["php8.3-fpm"])
	}
	if upstreamVersion(got["php8.4-fpm"][0]) != "8.4.13" || upstreamVersion(got["php8.4-fpm"][1]) != "8.4.14" {
		t.Fatalf("8.4 = %v", got["php8.4-fpm"])
	}
}

func waitJob(t *testing.T, s *jobStore, id string) *Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := s.Get(id)
		if j.Status != jobRunning {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return nil
}

func TestJobStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	s := newJobStore(path, nil)
	release := make(chan struct{})
	j1 := s.Start(context.Background(), "php.install", "8.4", func(ctx context.Context, log *JobLog) (string, error) {
		log.Printf("step one")
		<-release
		return "done", nil
	})
	dup := s.Start(context.Background(), "php.install", "8.4", func(context.Context, *JobLog) (string, error) {
		t.Error("duplicate job ran")
		return "", nil
	})
	if dup.ID != j1.ID {
		t.Fatal("a running job with the same kind and target should be reused")
	}
	close(release)
	if j := waitJob(t, s, j1.ID); j.Status != jobSucceeded || j.Message != "done" || !strings.Contains(j.Log, "step one") {
		t.Fatalf("job = %+v", j)
	}

	failed := s.Start(context.Background(), "php.maintenance", "", func(context.Context, *JobLog) (string, error) {
		return "", errors.New("boom")
	})
	if j := waitJob(t, s, failed.ID); j.Status != jobFailed || j.Message != "boom" {
		t.Fatalf("job = %+v", j)
	}
	if got := s.List("php.", 0); len(got) != 2 {
		t.Fatalf("prefix list = %d jobs", len(got))
	}

	detached := s.Start(context.Background(), "update", "v3.0.1", func(context.Context, *JobLog) (string, error) {
		return "installing", errDetached
	})
	time.Sleep(50 * time.Millisecond)
	if j, _ := s.Get(detached.ID); j.Status != jobRunning || j.Message != "installing" {
		t.Fatalf("detached job = %+v", j)
	}

	// After a restart, running jobs are interrupted unless they continue elsewhere.
	reloaded := newJobStore(path, func(j *Job) bool { return j.Kind == "update" })
	if j, _ := reloaded.Get(detached.ID); j.Status != jobRunning {
		t.Fatalf("update job should keep running, got %+v", j)
	}
	reloaded.Finish(detached.ID, true, "Updated.", "log line\n")
	if j, _ := reloaded.Get(detached.ID); j.Status != jobSucceeded || j.Message != "Updated." {
		t.Fatalf("finished job = %+v", j)
	}
	interrupted := newJobStore(path, nil)
	if j, _ := interrupted.Get(j1.ID); j.Status != jobSucceeded {
		t.Fatalf("finished jobs must be kept: %+v", j)
	}
}

func TestJobLogIsTailTrimmed(t *testing.T) {
	s := newJobStore("", nil)
	done := s.Start(context.Background(), "x", "", func(_ context.Context, log *JobLog) (string, error) {
		for i := 0; i < 5000; i++ {
			log.Printf("line %05d %s", i, strings.Repeat("x", 40))
		}
		return "", nil
	})
	j := waitJob(t, s, done.ID)
	if len(j.Log) > maxJobLog+16 || !strings.Contains(j.Log, "line 04999") || strings.Contains(j.Log, "line 00000") {
		t.Fatalf("log was not tail-trimmed (len %d)", len(j.Log))
	}
}

func TestUnitsAreHardened(t *testing.T) {
	u := units()
	panel := u["fastcp.service"]
	for _, want := range []string{"User=fastcp-panel", "NoNewPrivileges=yes", "ProtectSystem=strict", "ProtectHome=yes", "CapabilityBoundingSet=\n", "ReadWritePaths=/opt/fastcp/data /opt/fastcp/spool"} {
		if !strings.Contains(panel, want) {
			t.Errorf("panel unit lacks %q", want)
		}
	}
	caddy := u["fastcp-caddy.service"]
	for _, want := range []string{"User=caddy", "AmbientCapabilities=CAP_NET_BIND_SERVICE", "ProtectHome=read-only", "RuntimeDirectory=fastcp-caddy"} {
		if !strings.Contains(caddy, want) {
			t.Errorf("caddy unit lacks %q", want)
		}
	}
	if !strings.Contains(u["fastcp-agent.service"], "KillMode=process") {
		t.Error("agent unit must keep package installs alive across restarts")
	}
}

func TestParseMySQLVersion(t *testing.T) {
	for out, want := range map[string]string{
		"mysql  Ver 8.0.46-0ubuntu0.24.04.4 for Linux on aarch64 ((Ubuntu))":                              "MySQL 8.0.46",
		"mysql  Ver 9.1.0 for Linux on x86_64 (MySQL Community Server - GPL)":                             "MySQL 9.1.0",
		"mysql  Ver 15.1 Distrib 10.11.6-MariaDB, for debian-linux-gnu (aarch64) using  EditLine wrapper": "MariaDB 10.11.6",
		"mysql from 11.4.2-MariaDB, client 15.2 for debian-linux-gnu (x86_64) using  EditLine wrapper":    "MariaDB 11.4.2",
		"": "",
	} {
		if got := parseMySQLVersion(out); got != want {
			t.Errorf("parseMySQLVersion(%q) = %q, want %q", out, got, want)
		}
	}
}

func TestRPCErrorCodes(t *testing.T) {
	dir, err := os.MkdirTemp("", "fcp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	s := NewRPCServer()
	s.Handle("symlink", func(context.Context, json.RawMessage) (any, error) {
		return nil, &fs.PathError{Op: "open", Path: "/home/alice/apps", Err: safefs.ErrSymlink}
	})
	s.Handle("invalid", func(context.Context, json.RawMessage) (any, error) { return nil, invalidf("bad") })
	s.Handle("plain", func(context.Context, json.RawMessage) (any, error) { return nil, errors.New("disk on fire") })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.Serve(ctx, ln)

	c := NewClient(sock)
	for method, want := range map[string]string{"symlink": CodeConflict, "invalid": CodeInvalid, "plain": CodeInternal, "missing": CodeNotFound} {
		if got := ErrorCode(c.Call(ctx, method, nil, nil)); got != want {
			t.Errorf("%s: code %q, want %q", method, got, want)
		}
	}
}
