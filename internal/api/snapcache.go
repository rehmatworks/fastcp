package api

import (
	"context"
	"sync"
	"time"
)

// snapshotFresh is how long a snapshot list is served without listing the
// repository again. An older list is still served while a new one loads in
// the background: snapshots change through FastCP's own jobs, which replace
// the list as they finish.
const snapshotFresh = time.Minute

// snapshotCache keeps each account's snapshot list. Listing opens the
// repository, which takes seconds on a remote destination.
type snapshotCache struct {
	mu      sync.Mutex
	entries map[string]*snapshotEntry
}

type snapshotEntry struct {
	repository string
	list       []resticSnapshot
	at         time.Time // zero until a list is loaded
	gen        uint64    // bumped when the list is dropped
	loading    *snapshotLoad
}

type snapshotLoad struct {
	done chan struct{}
	list []resticSnapshot
	err  error
}

// entry returns username's entry, emptied if the repository changed.
// c.mu is held.
func (c *snapshotCache) entry(username, repository string) *snapshotEntry {
	if c.entries == nil {
		c.entries = map[string]*snapshotEntry{}
	}
	e := c.entries[username]
	if e == nil {
		e = &snapshotEntry{repository: repository}
		c.entries[username] = e
	} else if e.repository != repository {
		e.drop()
		e.repository = repository
	}
	return e
}

func (e *snapshotEntry) drop() {
	e.gen++
	e.list, e.at, e.loading = nil, time.Time{}, nil
}

// cachedSnapshots returns the user's snapshots, newest first.
func (s *Server) cachedSnapshots(ctx context.Context, repo *resticRepo) ([]resticSnapshot, error) {
	c := &s.snapCache
	c.mu.Lock()
	e := c.entry(repo.username, repo.env["RESTIC_REPOSITORY"])
	if !e.at.IsZero() {
		list := e.list
		if time.Since(e.at) > snapshotFresh && e.loading == nil {
			s.loadSnapshots(e, repo)
		}
		c.mu.Unlock()
		return list, nil
	}
	l := e.loading
	if l == nil {
		l = s.loadSnapshots(e, repo)
	}
	c.mu.Unlock()
	select {
	case <-l.done:
		return l.list, l.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// loadSnapshots lists the repository in the background, so the result is
// kept even if the request that asked for it goes away. c.mu is held.
func (s *Server) loadSnapshots(e *snapshotEntry, repo *resticRepo) *snapshotLoad {
	l := &snapshotLoad{done: make(chan struct{})}
	e.loading = l
	gen := e.gen
	s.goBackground(func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		l.list, l.err = s.snapshots(ctx, repo)
		c := &s.snapCache
		c.mu.Lock()
		if e.loading == l {
			e.loading = nil
		}
		if l.err == nil && e.gen == gen {
			e.list, e.at = l.list, time.Now()
		}
		c.mu.Unlock()
		close(l.done)
	})
	return l
}

// snapshotsChanged drops the user's snapshot list. With a repository, the
// list is loaded again and snapshotsChanged waits up to a minute for it, so
// the view that follows a finished job does not.
func (s *Server) snapshotsChanged(ctx context.Context, username string, repo *resticRepo) {
	c := &s.snapCache
	c.mu.Lock()
	if e := c.entries[username]; e != nil {
		e.drop()
	}
	if repo == nil {
		c.mu.Unlock()
		return
	}
	l := s.loadSnapshots(c.entry(username, repo.env["RESTIC_REPOSITORY"]), repo)
	c.mu.Unlock()
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	select {
	case <-l.done:
	case <-t.C:
	case <-ctx.Done():
	}
}
