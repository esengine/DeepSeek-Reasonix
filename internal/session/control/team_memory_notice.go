package control

import (
	"context"
	"encoding/json"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/session/team"
)

// NotifyTeamMemory shows a host-observed memory change without starting a turn.
func (c *Controller) NotifyTeamMemory(note string) {
	c.mu.Lock()
	sink := c.sink
	c.mu.Unlock()
	if sink != nil {
		sink.Emit(event.Event{Kind: event.Notice, Text: note})
	}
}

type teamMemoryWriter struct {
	tool.Tool
	controller   *Controller
	service      *team.Service
	agent, group string
}

func (t teamMemoryWriter) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	result, err := t.Tool.Execute(ctx, args)
	if err != nil {
		return result, err
	}
	note := "Shared memory updated. Review the saved entry in Memory settings."
	t.controller.NotifyTeamMemory(note)
	if err := t.service.RecordMemoryChange(ctx, t.agent, t.group, note); err != nil {
		return result, err
	}
	return result, nil
}
