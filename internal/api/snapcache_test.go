package api

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
)

func snapID(c byte) string { return strings.Repeat(string(c), 64) }

// snapshotHarness logs in as the admin, gives the account a backup
// destination and returns the token and the opened repository.
func snapshotHarness(t *testing.T) (*harness, string, *resticRepo) {
	t.Helper()
	h := newHarness(t)
	token := h.login("fastcp", "admin-pass")
	ctx := context.Background()
	pw, err := h.srv.box.Seal(crypto.PurposeBackup, "repo-pass")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &database.BackupConfig{Username: "fastcp", BackendType: "local", Repository: "/srv/backups/fastcp", PasswordEnc: pw, KeepLast: 7}
	if err := h.db.SaveBackupConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	repo, err := h.srv.userRepo(ctx, "fastcp")
	if err != nil {
		t.Fatal(err)
	}
	return h, token, repo
}

func (f *fakeAgent) setSnapshots(ids ...string) {
	f.mu.Lock()
	f.snapshots = ids
	f.mu.Unlock()
}

func (f *fakeAgent) listingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listings
}

func (h *harness) snapshotIDs(token string) []string {
	h.t.Helper()
	r := h.do("GET", "/api/backups/snapshots", token, nil)
	expect(h.t, r, 200, "list snapshots")
	var ids []string
	for _, v := range r.list {
		ids = append(ids, v.(map[string]any)["id"].(string))
	}
	return ids
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func TestSnapshotListIsCached(t *testing.T) {
	h, token, repo := snapshotHarness(t)
	a, b := snapID('a'), snapID('b')
	h.agent.setSnapshots(a)

	if ids := h.snapshotIDs(token); len(ids) != 1 || ids[0] != a {
		t.Fatalf("first list = %v", ids)
	}
	h.snapshotIDs(token)
	if n := h.agent.listingCount(); n != 1 {
		t.Fatalf("two views listed the repository %d times, want once", n)
	}

	// A job that changed the repository replaces the list before it
	// reports back.
	h.agent.setSnapshots(a, b)
	h.srv.snapshotsChanged(context.Background(), "fastcp", repo)
	if ids := h.snapshotIDs(token); len(ids) != 2 || ids[0] != b {
		t.Fatalf("after a change = %v, want [b a]", ids)
	}
	if n := h.agent.listingCount(); n != 2 {
		t.Fatalf("listings = %d, want 2", n)
	}

	// A list past snapshotFresh is served at once and refreshed behind.
	h.agent.setSnapshots(a)
	h.srv.snapCache.mu.Lock()
	h.srv.snapCache.entries["fastcp"].at = time.Now().Add(-2 * snapshotFresh)
	h.srv.snapCache.mu.Unlock()
	if ids := h.snapshotIDs(token); len(ids) != 2 {
		t.Fatalf("stale list = %v, want the cached two", ids)
	}
	waitFor(t, "the refresh", func() bool { return len(h.snapshotIDs(token)) == 1 })
	if n := h.agent.listingCount(); n != 3 {
		t.Fatalf("listings = %d, want 3", n)
	}
}

func TestSnapshotListLoadsOnce(t *testing.T) {
	h, token, _ := snapshotHarness(t)
	h.agent.setSnapshots(snapID('a'))
	hold := make(chan struct{})
	h.agent.mu.Lock()
	h.agent.hold = hold
	h.agent.mu.Unlock()

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if r := h.do("GET", "/api/backups/snapshots", token, nil); r.status != 200 || len(r.list) != 1 {
				t.Errorf("list: %d %v", r.status, r.list)
			}
		})
	}
	waitFor(t, "the listing", func() bool { return h.agent.listingCount() == 1 })
	time.Sleep(50 * time.Millisecond)
	close(hold)
	wg.Wait()
	if n := h.agent.listingCount(); n != 1 {
		t.Fatalf("concurrent views listed the repository %d times, want once", n)
	}
}

func TestSnapshotListIgnoresLoadBeforeChange(t *testing.T) {
	h, token, _ := snapshotHarness(t)
	a, b := snapID('a'), snapID('b')
	h.agent.setSnapshots(a)
	hold := make(chan struct{})
	h.agent.mu.Lock()
	h.agent.hold = hold
	h.agent.mu.Unlock()

	done := make(chan []string)
	go func() { done <- h.snapshotIDs(token) }()
	waitFor(t, "the listing", func() bool { return h.agent.listingCount() == 1 })

	// The repository changes while the listing runs.
	h.srv.snapshotsChanged(context.Background(), "fastcp", nil)
	h.agent.mu.Lock()
	h.agent.snapshots = []string{a, b}
	h.agent.hold = nil
	h.agent.mu.Unlock()
	close(hold)
	if ids := <-done; len(ids) != 1 {
		t.Fatalf("the waiting view got %v, want the listing it waited for", ids)
	}
	if ids := h.snapshotIDs(token); len(ids) != 2 {
		t.Fatalf("after the change = %v, want the new list", ids)
	}
}

func TestFindSnapshotNewerThanList(t *testing.T) {
	h, token, repo := snapshotHarness(t)
	a, b := snapID('a'), snapID('b')
	h.agent.setSnapshots(a)
	h.snapshotIDs(token)

	h.agent.setSnapshots(a, b)
	sn, err := h.srv.findSnapshot(context.Background(), repo, b[:8])
	if err != nil || sn.ID != b {
		t.Fatalf("findSnapshot = %v, %v", sn, err)
	}
	if _, err := h.srv.findSnapshot(context.Background(), repo, snapID('c')); err == nil {
		t.Fatal("found a snapshot that does not exist")
	}
}

func TestSnapshotListFollowsDestination(t *testing.T) {
	h, token, _ := snapshotHarness(t)
	h.agent.setSnapshots(snapID('a'))
	h.snapshotIDs(token)

	cfg, _, err := h.srv.backupConfig(context.Background(), "fastcp")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Repository = "/srv/backups/elsewhere"
	if err := h.db.SaveBackupConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	h.agent.setSnapshots()
	if ids := h.snapshotIDs(token); len(ids) != 0 {
		t.Fatalf("new destination lists %v, want none", ids)
	}
}
