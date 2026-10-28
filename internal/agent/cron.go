package agent

import (
	"github.com/rehmatworks/fastcp/internal/validate"
)

func validateCronJob(j *CronJob) error {
	if err := validate.CronExpression(j.Expression); err != nil {
		return err
	}
	if err := validate.CronCommand(j.Command); err != nil {
		return err
	}
	return validate.Label("name", j.Name, 128)
}

func escapeCron(cmd string) string { return validate.EscapeCronPercent(cmd) }
