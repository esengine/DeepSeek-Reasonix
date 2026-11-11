package control

import (
	"time"

	"reasonix/internal/contract/process"
)

// Processes lists the work this session has running now, across owners. It
// reads in-memory state only and costs what is running, not what ran.
func (c *Controller) Processes() ([]process.Process, error) {
	var out []process.Process
	for _, j := range c.Jobs() {
		out = append(out, process.Process{
			ID:      process.ID(process.KindJob, j.ID),
			Kind:    process.KindJob,
			Name:    firstNonBlank(j.Description, j.Label),
			State:   process.Running,
			Started: time.UnixMilli(j.StartedAt),
		})
	}
	parent := c.parentSessionID()
	if c.subagents == nil || parent == "" {
		return out, nil
	}
	runs, err := c.subagents.LiveRuns(parent, c.WorkspaceRoot())
	if err != nil {
		return out, err
	}
	for _, r := range runs {
		class, ok := r.State.ProcessClass()
		if !ok || class.State.Terminal() {
			continue
		}
		p := process.Process{
			ID:      process.ID(process.KindSubagent, r.Ref),
			Kind:    process.KindSubagent,
			Name:    r.Name,
			State:   class.State,
			Cause:   class.Cause,
			Started: r.StartedAt,
		}
		if r.ParentTurn != "" {
			p.Parent = "turn:" + r.ParentTurn
		}
		out = append(out, p)
	}
	return out, nil
}

func firstNonBlank(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
