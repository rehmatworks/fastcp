//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These tests create system accounts and need root. They run in the
// development container: FASTCP_INTEGRATION=1 go test ./internal/agent/
func integrationTenant(t *testing.T) *tenant {
	t.Helper()
	if os.Geteuid() != 0 || os.Getenv("FASTCP_INTEGRATION") != "1" {
		t.Skip("set FASTCP_INTEGRATION=1 and run as root")
	}
	name := "fcpt" + strconv.Itoa(int(time.Now().UnixNano()%100000))
	if out, err := exec.Command("useradd", "--create-home", "--user-group", "--shell", "/bin/bash", name).CombinedOutput(); err != nil {
		t.Fatalf("useradd: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("userdel", "--remove", name).Run() })
	tn, err := lookupTenant(name)
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

func testAgent(t *testing.T, users ...string) *Agent {
	a := &Agent{bootstrapped: map[string]bool{}, ctx: context.Background(), jobs: newJobStore("", nil)}
	st := &State{}
	for _, u := range users {
		st.Users = append(st.Users, StateUser{Username: u})
	}
	a.state = st
	return a
}

func TestExecAsDropsPrivileges(t *testing.T) {
	tn := integrationTenant(t)
	a := testAgent(t, tn.Name)
	params, _ := json.Marshal(UserExecRequest{Username: tn.Name, Script: "id -u; echo \"$HOME\"; echo \"$FOO\"", Env: map[string]string{"FOO": "bar"}, TimeoutSec: 10})
	res, err := a.handleUserExec(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	r := res.(*UserExecResult)
	want := strconv.Itoa(tn.UID) + "\n" + tn.Home + "\nbar\n"
	if r.ExitCode != 0 || r.Output != want {
		t.Fatalf("exec = %+v, want %q", r, want)
	}

	for _, bad := range []map[string]string{{"LD_PRELOAD": "/tmp/x.so"}, {"BASH_ENV": "/tmp/x"}, {"PATH": "/tmp"}, {"lower": "x"}} {
		params, _ := json.Marshal(UserExecRequest{Username: tn.Name, Script: "true", Env: bad})
		if _, err := a.handleUserExec(context.Background(), params); err == nil {
			t.Errorf("env %v accepted", bad)
		}
	}
	params, _ = json.Marshal(UserExecRequest{Username: "root", Script: "id"})
	if _, err := a.handleUserExec(context.Background(), params); err == nil {
		t.Fatal("exec as root accepted")
	}
	params, _ = json.Marshal(UserExecRequest{Username: tn.Name, Script: "sleep 30", TimeoutSec: 1})
	start := time.Now()
	res, err = a.handleUserExec(context.Background(), params)
	if err != nil || res.(*UserExecResult).ExitCode != -1 || time.Since(start) > 15*time.Second {
		t.Fatalf("timeout not enforced: %+v %v", res, err)
	}
}

func TestSSHKeyRefusesSymlinkedAuthorizedKeys(t *testing.T) {
	tn := integrationTenant(t)
	a := testAgent(t, tn.Name)
	shadow, _ := os.ReadFile("/etc/shadow")

	ssh := filepath.Join(tn.Home, ".ssh")
	os.Mkdir(ssh, 0o700)
	os.Chown(ssh, tn.UID, tn.GID)
	os.Symlink("/etc/shadow", filepath.Join(ssh, "authorized_keys"))

	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test"
	params, _ := json.Marshal(AddSSHKeyRequest{Username: tn.Name, KeyID: "6f1c2c1e-5a4b-4c3d-8e9f-0a1b2c3d4e5f", PublicKey: key})
	_, _ = a.handleAddSSHKey(context.Background(), params)
	after, _ := os.ReadFile("/etc/shadow")
	if string(after) != string(shadow) {
		t.Fatal("/etc/shadow was modified through a symlink")
	}

	// A symlinked .ssh directory is refused outright.
	os.RemoveAll(ssh)
	target := t.TempDir()
	os.Symlink(target, ssh)
	if _, err := a.handleAddSSHKey(context.Background(), params); err == nil {
		t.Fatal("symlinked .ssh accepted")
	}
	if entries, _ := os.ReadDir(target); len(entries) != 0 {
		t.Fatal("wrote into the symlink target")
	}
}

func TestSiteLayoutRefusesSymlinks(t *testing.T) {
	tn := integrationTenant(t)
	if err := bootstrapTenant(tn); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(tn.Home, "apps", "evil")); err != nil {
		t.Fatal(err)
	}
	if site, err := ensureSiteLayout(tn, "evil", false); err == nil {
		site.Close()
		t.Fatal("symlinked site directory accepted")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("created directories through a symlink")
	}

	site, err := ensureSiteLayout(tn, "good", true)
	if err != nil {
		t.Fatal(err)
	}
	site.Close()
	fi, err := os.Stat(filepath.Join(tn.Home, "apps", "good", "public"))
	if err != nil || fi.Mode().Perm() != 0o750 {
		t.Fatalf("public = %v %v", fi, err)
	}
	if _, err := ensureSiteLayout(tn, "good", true); err == nil {
		t.Fatal("creating an existing site should conflict")
	}

	// Deleting a site never follows links out of the home directory.
	keep := filepath.Join(outside, "keep")
	os.WriteFile(keep, []byte("x"), 0o644)
	os.Symlink(outside, filepath.Join(tn.Home, "apps", "good", "public", "link"))
	a := testAgent(t, tn.Name)
	a.state = &State{}
	params, _ := json.Marshal(DeleteSiteRequest{Username: tn.Name, Slug: "good"})
	if _, err := a.handleDeleteSite(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("site delete followed a symlink")
	}
	if _, err := os.Stat(filepath.Join(tn.Home, "apps", "good")); !os.IsNotExist(err) {
		t.Fatal("site directory still exists")
	}
}

func TestRenderedCrontabInstalls(t *testing.T) {
	if _, err := exec.LookPath("crontab"); err != nil {
		t.Skip("crontab not installed")
	}
	tn := integrationTenant(t)
	tab, _ := renderCrontab([]CronJob{{ID: "a", Name: "x", Expression: "*/5 * * * *", Command: "echo 50% done", Enabled: true}})
	if _, err := runEnv(context.Background(), 10*time.Second, nil, []byte(tab), "crontab", "-u", tn.Name, "-"); err != nil {
		t.Fatal(err)
	}
	out, _ := run(context.Background(), 10*time.Second, "crontab", "-l", "-u", tn.Name)
	if !strings.Contains(out, `echo 50\% done`) {
		t.Fatalf("crontab = %q", out)
	}
}
