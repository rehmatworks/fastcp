package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	maxJobs      = 50
	maxJobLog    = 64 << 10
	jobQueued    = "queued"
	jobRunning   = "running"
	jobSucceeded = "succeeded"
	jobFailed    = "failed"
)

// jobStore keeps long-running operations and persists them so their
// outcome is still visible after an agent restart.
type jobStore struct {
	mu   sync.Mutex
	path string
	jobs map[string]*Job
	// active maps a job key (kind+target) to the running job ID, so the
	// same operation is not started twice.
	active map[string]string
}

// errDetached is returned by a job function that handed its work to another
// process; the job stays running until Finish is called.
var errDetached = errors.New("job detached")

// newJobStore loads persisted jobs. Jobs that were running when the agent
// stopped are marked failed unless keepRunning says they continue
// elsewhere.
func newJobStore(path string, keepRunning func(*Job) bool) *jobStore {
	s := &jobStore{path: path, jobs: map[string]*Job{}, active: map[string]string{}}
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var list []*Job
	if err := json.Unmarshal(data, &list); err != nil {
		slog.Warn("ignoring unreadable job store", "path", path, "error", err)
		return s
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, j := range list {
		if j.Status == jobRunning && keepRunning != nil && keepRunning(j) {
			s.jobs[j.ID] = j
			continue
		}
		if j.Status == jobRunning || j.Status == jobQueued {
			j.Status = jobFailed
			j.Message = "interrupted by an agent restart"
			j.FinishedAt = now
		}
		s.jobs[j.ID] = j
	}
	s.saveLocked()
	return s
}

func (s *jobStore) saveLocked() {
	if s.path == "" {
		return
	}
	list := s.listLocked("")
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}

// listLocked returns jobs newest first, optionally filtered by kind. A kind
// ending in "." matches every kind with that prefix.
func (s *jobStore) listLocked(kind string) []*Job {
	out := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		if kind == "" || j.Kind == kind || (strings.HasSuffix(kind, ".") && strings.HasPrefix(j.Kind, kind)) {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].StartedAt > out[k].StartedAt })
	return out
}

func (s *jobStore) pruneLocked() {
	list := s.listLocked("")
	for _, j := range list[min(len(list), maxJobs):] {
		if j.Status != jobRunning && j.Status != jobQueued {
			delete(s.jobs, j.ID)
		}
	}
}

// Get returns a copy of a job.
func (s *jobStore) Get(id string) (*Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	c := *j
	return &c, true
}

// List returns copies of the newest jobs of a kind ("" for all).
func (s *jobStore) List(kind string, limit int) []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.listLocked(kind)
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	out := make([]Job, len(list))
	for i, j := range list {
		out[i] = *j
	}
	return out
}

// Latest returns the newest job of a kind and target.
func (s *jobStore) Latest(kind, target string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.listLocked(kind) {
		if target == "" || j.Target == target {
			c := *j
			return &c
		}
	}
	return nil
}

// Finish records the outcome of a detached job.
func (s *jobStore) Finish(id string, ok bool, message, log string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, found := s.jobs[id]
	if !found {
		return
	}
	j.Status = jobFailed
	if ok {
		j.Status = jobSucceeded
	}
	j.Message = message
	if log != "" {
		j.Log += log
		if len(j.Log) > maxJobLog {
			j.Log = "…\n" + j.Log[len(j.Log)-maxJobLog:]
		}
	}
	j.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	for k, id2 := range s.active {
		if id2 == id {
			delete(s.active, k)
		}
	}
	s.saveLocked()
}

// JobLog collects output for a running job.
type JobLog struct {
	s  *jobStore
	id string
}

// ID returns the job's ID.
func (l *JobLog) ID() string { return l.id }

func (l *JobLog) Printf(format string, args ...any) {
	l.Write([]byte(fmt.Sprintf(format, args...) + "\n"))
}

func (l *JobLog) Write(p []byte) (int, error) {
	l.s.mu.Lock()
	defer l.s.mu.Unlock()
	j, ok := l.s.jobs[l.id]
	if !ok {
		return len(p), nil
	}
	j.Log += string(p)
	if len(j.Log) > maxJobLog {
		cut := len(j.Log) - maxJobLog
		if i := strings.IndexByte(j.Log[cut:], '\n'); i >= 0 && i < 1024 {
			cut += i + 1
		}
		j.Log = "…\n" + j.Log[cut:]
	}
	return len(p), nil
}

// Start runs fn in the background as a new job. If a job with the same
// kind and target is still running, that job is returned instead.
func (s *jobStore) Start(ctx context.Context, kind, target string, fn func(ctx context.Context, log *JobLog) (string, error)) *Job {
	s.mu.Lock()
	key := kind + "\x00" + target
	if id, ok := s.active[key]; ok {
		if j, ok := s.jobs[id]; ok {
			c := *j
			s.mu.Unlock()
			return &c
		}
	}
	j := &Job{
		ID:        uuid.NewString(),
		Kind:      kind,
		Target:    target,
		Status:    jobRunning,
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	s.jobs[j.ID] = j
	s.active[key] = j.ID
	s.pruneLocked()
	s.saveLocked()
	c := *j
	s.mu.Unlock()

	go func() {
		log := &JobLog{s: s, id: j.ID}
		msg, err := func() (msg string, err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("internal error: %v", r)
					slog.Error("job panicked", "kind", kind, "panic", r)
				}
			}()
			return fn(context.WithoutCancel(ctx), log)
		}()
		s.mu.Lock()
		defer s.mu.Unlock()
		if errors.Is(err, errDetached) {
			j.Message = msg
			s.saveLocked()
			return
		}
		delete(s.active, key)
		j.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err != nil {
			j.Status = jobFailed
			j.Message = err.Error()
			slog.Warn("job failed", "kind", kind, "target", target, "error", err)
		} else {
			j.Status = jobSucceeded
			j.Message = msg
		}
		s.saveLocked()
	}()
	return &c
}

func (s *jobStore) handleGet(_ context.Context, params json.RawMessage) (any, error) {
	req, err := decode[JobRequest](params)
	if err != nil {
		return nil, err
	}
	j, ok := s.Get(req.ID)
	if !ok {
		return nil, notFoundf("job not found")
	}
	return j, nil
}
