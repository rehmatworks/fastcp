package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/rehmatworks/fastcp/internal/validate"
	"github.com/robfig/cron/v3"
)

const maxCronJobs = 50

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

type cronView struct {
	*database.CronJob
	Description string     `json:"description"`
	NextRun     *time.Time `json:"next_run,omitempty"`
}

func viewCron(j *database.CronJob) cronView {
	v := cronView{CronJob: j, Description: describeCron(j.Expression)}
	if j.Enabled {
		if next := nextRuns(j.Expression, 1); len(next) > 0 {
			v.NextRun = &next[0]
		}
	}
	return v
}

func nextRuns(expr string, n int) []time.Time {
	sched, err := cronParser.Parse(strings.TrimSpace(expr))
	if err != nil {
		return nil
	}
	var out []time.Time
	t := time.Now()
	for range n {
		t = sched.Next(t)
		if t.IsZero() {
			break
		}
		out = append(out, t)
	}
	return out
}

var weekdays = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

func clock(h, m int) string {
	return fmt.Sprintf("%02d:%02d", h, m)
}

func num(s string, lo, hi int) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= lo && n <= hi
}

// describeCron explains common schedules in words. Unusual ones fall back
// to the expression itself.
func describeCron(expr string) string {
	expr = strings.TrimSpace(expr)
	switch expr {
	case "@reboot":
		return "At server startup"
	case "@hourly":
		return "Every hour"
	case "@daily", "@midnight":
		return "Every day at 00:00"
	case "@weekly":
		return "Every Sunday at 00:00"
	case "@monthly":
		return "On the 1st of every month at 00:00"
	case "@yearly", "@annually":
		return "Every year on January 1 at 00:00"
	}
	f := strings.Fields(expr)
	if len(f) != 5 {
		return expr
	}
	minute, hour, dom, month, dow := f[0], f[1], f[2], f[3], f[4]
	if month != "*" {
		return expr
	}
	everyDay := dom == "*" && dow == "*"
	if everyDay && hour == "*" {
		switch {
		case minute == "*":
			return "Every minute"
		case strings.HasPrefix(minute, "*/"):
			if n, ok := num(minute[2:], 1, 59); ok {
				return fmt.Sprintf("Every %d minutes", n)
			}
		default:
			if m, ok := num(minute, 0, 59); ok {
				return fmt.Sprintf("Every hour at minute %d", m)
			}
		}
		return expr
	}
	m, okM := num(minute, 0, 59)
	if everyDay && strings.HasPrefix(hour, "*/") && okM {
		if n, ok := num(hour[2:], 1, 23); ok {
			return fmt.Sprintf("Every %d hours at minute %d", n, m)
		}
	}
	h, okH := num(hour, 0, 23)
	if !okM || !okH {
		return expr
	}
	at := clock(h, m)
	switch {
	case everyDay:
		return "Every day at " + at
	case dom == "*" && dow == "1-5":
		return "Weekdays at " + at
	case dom == "*" && dow == "0,6":
		return "Weekends at " + at
	case dom == "*":
		if d, ok := num(dow, 0, 7); ok {
			return fmt.Sprintf("Every %s at %s", weekdays[d%7], at)
		}
	case dow == "*":
		if d, ok := num(dom, 1, 31); ok {
			return fmt.Sprintf("On day %d of every month at %s", d, at)
		}
	}
	return expr
}

type cronRequest struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Command    string `json:"command"`
	Enabled    *bool  `json:"enabled"`
}

func (req *cronRequest) validate() error {
	req.Name = strings.TrimSpace(req.Name)
	req.Expression = strings.Join(strings.Fields(req.Expression), " ")
	req.Command = strings.TrimSpace(req.Command)
	if err := validate.Label("name", req.Name, 100); err != nil {
		return validation(err)
	}
	if err := validate.CronJobSchedule(req.Expression); err != nil {
		return validation(err)
	}
	if err := validate.CronCommand(req.Command); err != nil {
		return validation(err)
	}
	return nil
}

func (s *Server) ownedCron(r *http.Request) (*database.CronJob, error) {
	j, err := s.db.GetCronJob(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, database.ErrNotFound) || (err == nil && j.Username != current(r).User.Username) {
		return nil, notFound("Cron job")
	}
	return j, err
}

func (s *Server) listCron(w http.ResponseWriter, r *http.Request) error {
	jobs, err := s.db.ListCronJobs(r.Context(), current(r).User.Username)
	if err != nil {
		return err
	}
	out := make([]cronView, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, viewCron(j))
	}
	return ok(w, out)
}

func (s *Server) describeCron(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Expression string `json:"expression"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	expr := strings.Join(strings.Fields(req.Expression), " ")
	if err := validate.CronJobSchedule(expr); err != nil {
		return ok(w, map[string]any{"valid": false, "message": err.Error()})
	}
	return ok(w, map[string]any{"valid": true, "description": describeCron(expr), "next_runs": nextRuns(expr, 3)})
}

func (s *Server) createCron(w http.ResponseWriter, r *http.Request) error {
	p := current(r)
	var req cronRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	if err := req.validate(); err != nil {
		return err
	}
	ctx := r.Context()
	existing, err := s.db.ListCronJobs(ctx, p.User.Username)
	if err != nil {
		return err
	}
	if len(existing) >= maxCronJobs {
		return badRequest("An account can have at most %d cron jobs.", maxCronJobs)
	}
	j := &database.CronJob{
		ID: uuid.NewString(), Username: p.User.Username, Name: req.Name,
		Expression: req.Expression, Command: req.Command, Enabled: req.Enabled == nil || *req.Enabled,
	}
	err = s.change(ctx, func() (func(), error) {
		if err := s.db.CreateCronJob(ctx, j); err != nil {
			return nil, err
		}
		return func() { _ = s.db.DeleteCronJob(ctx, j.ID) }, nil
	})
	if err != nil {
		return err
	}
	return created(w, viewCron(j))
}

func (s *Server) updateCron(w http.ResponseWriter, r *http.Request) error {
	j, err := s.ownedCron(r)
	if err != nil {
		return err
	}
	var req cronRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	if err := req.validate(); err != nil {
		return err
	}
	ctx := r.Context()
	before := *j
	j.Name, j.Expression, j.Command = req.Name, req.Expression, req.Command
	if req.Enabled != nil {
		j.Enabled = *req.Enabled
	}
	err = s.change(ctx, func() (func(), error) {
		if err := s.db.UpdateCronJob(ctx, j); err != nil {
			return nil, err
		}
		return func() { _ = s.db.UpdateCronJob(ctx, &before) }, nil
	})
	if err != nil {
		return err
	}
	return ok(w, viewCron(j))
}

func (s *Server) deleteCron(w http.ResponseWriter, r *http.Request) error {
	j, err := s.ownedCron(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	err = s.change(ctx, func() (func(), error) {
		if err := s.db.DeleteCronJob(ctx, j.ID); err != nil {
			return nil, err
		}
		return func() {
			restored := *j
			_ = s.db.CreateCronJob(ctx, &restored)
		}, nil
	})
	if err != nil {
		return err
	}
	return noContent(w)
}
