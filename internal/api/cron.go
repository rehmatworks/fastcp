package api

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/robfig/cron/v3"
)

// CronService handles cron job operations
type CronService struct {
	db    *database.DB
	agent *agent.Client
}

// NewCronService creates a new CronService
func NewCronService(db *database.DB, agent *agent.Client) *CronService {
	return &CronService{db: db, agent: agent}
}

// List returns all cron jobs for a user
func (s *CronService) List(ctx context.Context, username string) ([]*CronJob, error) {
	jobs, err := s.db.ListCronJobs(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("failed to list cron jobs: %w", err)
	}

	result := make([]*CronJob, len(jobs))
	for i, j := range jobs {
		result[i] = &CronJob{
			ID:          j.ID,
			Username:    j.Username,
			Name:        j.Name,
			Expression:  j.Expression,
			Command:     j.Command,
			Enabled:     j.Enabled,
			Description: describeCronExpression(j.Expression),
			CreatedAt:   j.CreatedAt,
			UpdatedAt:   j.UpdatedAt,
		}
	}

	return result, nil
}

// Create creates a new cron job
func (s *CronService) Create(ctx context.Context, req *CreateCronJobRequest) (*CronJob, error) {
	// Validate cron expression
	if err := validateCronExpression(req.Expression); err != nil {
		return nil, err
	}

	// Validate command
	if err := validateCronCommand(req.Command); err != nil {
		return nil, err
	}

	// Validate name
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}

	// Create in database
	job := &database.CronJob{
		ID:         uuid.New().String(),
		Username:   req.Username,
		Name:       strings.TrimSpace(req.Name),
		Expression: req.Expression,
		Command:    req.Command,
		Enabled:    true,
	}

	if err := s.db.CreateCronJob(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create cron job: %w", err)
	}

	// Sync to system crontab
	if err := s.syncCronJobs(ctx, req.Username); err != nil {
		return nil, fmt.Errorf("failed to sync cron jobs: %w", err)
	}

	return &CronJob{
		ID:          job.ID,
		Username:    job.Username,
		Name:        job.Name,
		Expression:  job.Expression,
		Command:     job.Command,
		Enabled:     job.Enabled,
		Description: describeCronExpression(job.Expression),
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
	}, nil
}

// Update updates an existing cron job
func (s *CronService) Update(ctx context.Context, req *UpdateCronJobRequest) (*CronJob, error) {
	// Get existing job
	job, err := s.db.GetCronJob(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("cron job not found: %w", err)
	}

	// Verify ownership
	if job.Username != req.Username {
		return nil, fmt.Errorf("access denied")
	}

	// Validate cron expression
	if err := validateCronExpression(req.Expression); err != nil {
		return nil, err
	}

	// Validate command
	if err := validateCronCommand(req.Command); err != nil {
		return nil, err
	}

	// Validate name
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}

	// Update job
	job.Name = strings.TrimSpace(req.Name)
	job.Expression = req.Expression
	job.Command = req.Command
	job.Enabled = req.Enabled

	if err := s.db.UpdateCronJob(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to update cron job: %w", err)
	}

	// Sync to system crontab
	if err := s.syncCronJobs(ctx, req.Username); err != nil {
		return nil, fmt.Errorf("failed to sync cron jobs: %w", err)
	}

	return &CronJob{
		ID:          job.ID,
		Username:    job.Username,
		Name:        job.Name,
		Expression:  job.Expression,
		Command:     job.Command,
		Enabled:     job.Enabled,
		Description: describeCronExpression(job.Expression),
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
	}, nil
}

// Toggle enables or disables a cron job
func (s *CronService) Toggle(ctx context.Context, username, id string, enabled bool) error {
	// Get existing job
	job, err := s.db.GetCronJob(ctx, id)
	if err != nil {
		return fmt.Errorf("cron job not found: %w", err)
	}

	// Verify ownership
	if job.Username != username {
		return fmt.Errorf("access denied")
	}

	// Toggle in database
	if err := s.db.ToggleCronJob(ctx, id, enabled); err != nil {
		return fmt.Errorf("failed to toggle cron job: %w", err)
	}

	// Sync to system crontab
	if err := s.syncCronJobs(ctx, username); err != nil {
		return fmt.Errorf("failed to sync cron jobs: %w", err)
	}

	return nil
}

// Delete deletes a cron job
func (s *CronService) Delete(ctx context.Context, username, id string) error {
	// Get existing job
	job, err := s.db.GetCronJob(ctx, id)
	if err != nil {
		return fmt.Errorf("cron job not found: %w", err)
	}

	// Verify ownership
	if job.Username != username {
		return fmt.Errorf("access denied")
	}

	// Delete from database
	if err := s.db.DeleteCronJob(ctx, id); err != nil {
		return fmt.Errorf("failed to delete cron job: %w", err)
	}

	// Sync to system crontab
	if err := s.syncCronJobs(ctx, username); err != nil {
		return fmt.Errorf("failed to sync cron jobs: %w", err)
	}

	return nil
}

// syncCronJobs syncs all cron jobs for a user to their system crontab
func (s *CronService) syncCronJobs(ctx context.Context, username string) error {
	jobs, err := s.db.ListCronJobs(ctx, username)
	if err != nil {
		return err
	}

	// Convert to agent format
	agentJobs := make([]agent.CronJob, len(jobs))
	for i, j := range jobs {
		agentJobs[i] = agent.CronJob{
			ID:         j.ID,
			Name:       j.Name,
			Expression: j.Expression,
			Command:    j.Command,
			Enabled:    j.Enabled,
		}
	}

	// Call agent to sync
	return s.agent.SyncCronJobs(ctx, &agent.SyncCronJobsRequest{
		Username: username,
		Jobs:     agentJobs,
	})
}

// validateCronExpression validates a cron expression
func validateCronExpression(expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return fmt.Errorf("cron expression is required")
	}

	// Parse with standard 5-field format
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(expr); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}

	return nil
}

// validateCronCommand validates a cron command
func validateCronCommand(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return fmt.Errorf("command is required")
	}

	// Basic security checks
	dangerousPatterns := []string{
		"rm -rf /",
		"dd if=",
		"> /dev/sd",
		"mkfs",
		":(){:|:&};:",
	}

	cmdLower := strings.ToLower(cmd)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(cmdLower, pattern) {
			return fmt.Errorf("command contains potentially dangerous pattern")
		}
	}

	return nil
}

// describeCronExpression returns a human-readable description of a cron expression
func describeCronExpression(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return expr
	}

	minute, hour, dom, month, dow := fields[0], fields[1], fields[2], fields[3], fields[4]

	// Common patterns
	if minute == "0" && hour == "0" && dom == "*" && month == "*" && dow == "*" {
		return "Every day at midnight"
	}
	if minute == "0" && dom == "*" && month == "*" && dow == "*" {
		if h, ok := parseSimpleNumber(hour); ok {
			ampm := "AM"
			displayHour := h
			if h >= 12 {
				ampm = "PM"
				if h > 12 {
					displayHour = h - 12
				}
			}
			if h == 0 {
				displayHour = 12
			}
			return fmt.Sprintf("Every day at %d:00 %s", displayHour, ampm)
		}
	}
	if dom == "*" && month == "*" && dow == "*" {
		if m, mok := parseSimpleNumber(minute); mok {
			if hour == "*" {
				return fmt.Sprintf("Every hour at minute %d", m)
			}
		}
	}
	if minute == "*" && hour == "*" && dom == "*" && month == "*" && dow == "*" {
		return "Every minute"
	}
	if minute == "*/5" && hour == "*" && dom == "*" && month == "*" && dow == "*" {
		return "Every 5 minutes"
	}
	if minute == "*/10" && hour == "*" && dom == "*" && month == "*" && dow == "*" {
		return "Every 10 minutes"
	}
	if minute == "*/15" && hour == "*" && dom == "*" && month == "*" && dow == "*" {
		return "Every 15 minutes"
	}
	if minute == "*/30" && hour == "*" && dom == "*" && month == "*" && dow == "*" {
		return "Every 30 minutes"
	}
	if minute == "0" && hour == "*/2" && dom == "*" && month == "*" && dow == "*" {
		return "Every 2 hours"
	}
	if minute == "0" && hour == "*/6" && dom == "*" && month == "*" && dow == "*" {
		return "Every 6 hours"
	}
	if minute == "0" && hour == "*/12" && dom == "*" && month == "*" && dow == "*" {
		return "Every 12 hours"
	}
	if minute == "0" && hour == "0" && dom == "*" && month == "*" && dow == "0" {
		return "Every Sunday at midnight"
	}
	if minute == "0" && hour == "0" && dom == "1" && month == "*" && dow == "*" {
		return "First day of every month at midnight"
	}

	// Default: show the expression
	return fmt.Sprintf("Schedule: %s", expr)
}

func parseSimpleNumber(s string) (int, bool) {
	match, _ := regexp.MatchString(`^\d+$`, s)
	if !match {
		return 0, false
	}
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n, true
}
