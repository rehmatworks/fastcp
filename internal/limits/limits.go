package limits

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/rehmatworks/fastcp/internal/models"
)

// Manager handles resource limits for users
type Manager struct {
	logger     *slog.Logger
	cgroupPath string
}

// NewManager creates a new limits manager
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		logger:     logger,
		cgroupPath: "/sys/fs/cgroup",
	}
}

// ApplyLimits applies resource limits to a user
func (m *Manager) ApplyLimits(limits *models.UserLimits) error {
	if runtime.GOOS != "linux" {
		m.logger.Debug("resource limits only supported on Linux")
		return nil
	}

	// Apply cgroup limits (CPU, RAM, processes)
	if err := m.applyCgroupLimits(limits); err != nil {
		return fmt.Errorf("failed to apply cgroup limits: %w", err)
	}

	m.logger.Info("applied resource limits",
		"user", limits.Username,
		"ram_mb", limits.MaxRAMMB,
		"cpu_percent", limits.MaxCPUPercent,
		"processes", limits.MaxProcesses,
	)

	return nil
}

// applyCgroupLimits creates/updates cgroup for user
func (m *Manager) applyCgroupLimits(limits *models.UserLimits) error {
	// Check if cgroup v2 is available
	if !m.isCgroupV2() {
		m.logger.Warn("cgroup v2 not available, resource limits may not work")
		return nil
	}

	cgroupName := fmt.Sprintf("fastcp-%s", limits.Username)
	cgroupDir := filepath.Join(m.cgroupPath, cgroupName)

	// Create cgroup directory if it doesn't exist
	if err := os.MkdirAll(cgroupDir, 0755); err != nil {
		return fmt.Errorf("failed to create cgroup: %w", err)
	}

	// Enable controllers
	controllers := "+cpu +memory +pids"
	if err := os.WriteFile(filepath.Join(m.cgroupPath, "cgroup.subtree_control"), []byte(controllers), 0644); err != nil {
		m.logger.Warn("failed to enable cgroup controllers", "error", err)
	}

	// Apply memory limit
	if limits.MaxRAMMB > 0 {
		memBytes := limits.MaxRAMMB * 1024 * 1024
		memFile := filepath.Join(cgroupDir, "memory.max")
		if err := os.WriteFile(memFile, []byte(strconv.FormatInt(memBytes, 10)), 0644); err != nil {
			m.logger.Warn("failed to set memory limit", "error", err)
		}
	}

	// Apply CPU limit
	if limits.MaxCPUPercent > 0 {
		// cpu.max format: "$MAX $PERIOD" (microseconds)
		// 100000 = 100ms period, so for 50% = "50000 100000"
		period := 100000
		quota := (limits.MaxCPUPercent * period) / 100
		cpuMax := fmt.Sprintf("%d %d", quota, period)
		cpuFile := filepath.Join(cgroupDir, "cpu.max")
		if err := os.WriteFile(cpuFile, []byte(cpuMax), 0644); err != nil {
			m.logger.Warn("failed to set CPU limit", "error", err)
		}
	}

	// Apply process limit
	if limits.MaxProcesses > 0 {
		pidsFile := filepath.Join(cgroupDir, "pids.max")
		if err := os.WriteFile(pidsFile, []byte(strconv.Itoa(limits.MaxProcesses)), 0644); err != nil {
			m.logger.Warn("failed to set process limit", "error", err)
		}
	}

	return nil
}


// GetUsage returns current resource usage for a user
func (m *Manager) GetUsage(username string) (*ResourceUsage, error) {
	usage := &ResourceUsage{
		Username: username,
	}

	if runtime.GOOS != "linux" {
		return usage, nil
	}

	// Get cgroup usage
	cgroupDir := filepath.Join(m.cgroupPath, fmt.Sprintf("fastcp-%s", username))

	// Memory usage
	if data, err := os.ReadFile(filepath.Join(cgroupDir, "memory.current")); err == nil {
		if bytes, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
			usage.RAMUsedMB = bytes / (1024 * 1024)
		}
	}

	// CPU usage (this is cumulative, would need to calculate rate)
	if data, err := os.ReadFile(filepath.Join(cgroupDir, "cpu.stat")); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "usage_usec") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					if usec, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
						usage.CPUUsageMicros = usec
					}
				}
			}
		}
	}

	// Process count
	if data, err := os.ReadFile(filepath.Join(cgroupDir, "pids.current")); err == nil {
		if count, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			usage.ProcessCount = count
		}
	}

	// Disk usage
	diskUsage, err := m.getDiskUsage(username)
	if err == nil {
		usage.DiskUsedMB = diskUsage
	}

	return usage, nil
}

// ResourceUsage represents current resource usage
type ResourceUsage struct {
	Username       string `json:"username"`
	RAMUsedMB      int64  `json:"ram_used_mb"`
	CPUUsageMicros int64  `json:"cpu_usage_micros"`
	DiskUsedMB     int64  `json:"disk_used_mb"`
	ProcessCount   int    `json:"process_count"`
}

// AddProcessToCgroup adds a process to user's cgroup
func (m *Manager) AddProcessToCgroup(username string, pid int) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	cgroupDir := filepath.Join(m.cgroupPath, fmt.Sprintf("fastcp-%s", username))
	procsFile := filepath.Join(cgroupDir, "cgroup.procs")

	return os.WriteFile(procsFile, []byte(strconv.Itoa(pid)), 0644)
}

// RemoveLimits removes cgroup for a user
func (m *Manager) RemoveLimits(username string) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	cgroupDir := filepath.Join(m.cgroupPath, fmt.Sprintf("fastcp-%s", username))

	// Move all processes out first
	procsFile := filepath.Join(cgroupDir, "cgroup.procs")
	if data, err := os.ReadFile(procsFile); err == nil {
		pids := strings.Fields(string(data))
		for _, pid := range pids {
			// Move to root cgroup
			_ = os.WriteFile(filepath.Join(m.cgroupPath, "cgroup.procs"), []byte(pid), 0644)
		}
	}

	return os.RemoveAll(cgroupDir)
}

// Helper methods

func (m *Manager) isCgroupV2() bool {
	_, err := os.Stat(filepath.Join(m.cgroupPath, "cgroup.controllers"))
	return err == nil
}

type userInfo struct {
	Username string
	UID      int
	GID      int
}

func (m *Manager) lookupUser(username string) (*userInfo, error) {
	cmd := exec.Command("id", "-u", username)
	uidOutput, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	uid, _ := strconv.Atoi(strings.TrimSpace(string(uidOutput)))

	cmd = exec.Command("id", "-g", username)
	gidOutput, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	gid, _ := strconv.Atoi(strings.TrimSpace(string(gidOutput)))

	return &userInfo{
		Username: username,
		UID:      uid,
		GID:      gid,
	}, nil
}

func (m *Manager) getDiskUsage(username string) (int64, error) {
	// Use du to calculate disk usage
	userDir := filepath.Join("/var/www", username)
	cmd := exec.Command("du", "-sm", userDir)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	fields := strings.Fields(string(output))
	if len(fields) >= 1 {
		mb, _ := strconv.ParseInt(fields[0], 10, 64)
		return mb, nil
	}

	return 0, nil
}

