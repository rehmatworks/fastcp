package api

import (
	"context"

	"github.com/rehmatworks/fastcp/internal/agent"
)

// SystemService handles system operations
type SystemService struct {
	agent *agent.Client
}

// NewSystemService creates a new system service
func NewSystemService(agent *agent.Client) *SystemService {
	return &SystemService{agent: agent}
}

// GetStatus returns system status
func (s *SystemService) GetStatus(ctx context.Context) (*SystemStatus, error) {
	status, err := s.agent.GetSystemStatus(ctx)
	if err != nil {
		return nil, err
	}

	return &SystemStatus{
		Hostname:     status.Hostname,
		OS:           status.OS,
		Uptime:       status.Uptime,
		LoadAverage:  status.LoadAverage,
		MemoryTotal:  status.MemoryTotal,
		MemoryUsed:   status.MemoryUsed,
		DiskTotal:    status.DiskTotal,
		DiskUsed:     status.DiskUsed,
		PHPVersion:   status.PHPVersion,
		MySQLVersion: status.MySQLVersion,
	}, nil
}

// GetServices returns system services status
func (s *SystemService) GetServices(ctx context.Context) ([]*Service, error) {
	services, err := s.agent.GetServices(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*Service, len(services))
	for i, svc := range services {
		result[i] = &Service{
			Name:    svc.Name,
			Status:  svc.Status,
			Enabled: svc.Enabled,
		}
	}
	return result, nil
}
