//go:build linux

package agent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	unlock, err := lockFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	if _, err := lockFile(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second lock while held: %v, want deadline exceeded", err)
	}

	got := make(chan error, 1)
	go func() {
		unlock2, err := lockFile(context.Background(), path)
		if err == nil {
			unlock2()
		}
		got <- err
	}()
	time.Sleep(300 * time.Millisecond)
	unlock()
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting lock was not granted after release")
	}
}
