package control

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

const maxSubagentEvents = 200

// SubagentEvent is a compact controller-owned execution detail record for one
// slash-subagent run.
type SubagentEvent struct {
	Time  time.Time
	Kind  event.Kind
	Text  string
	Tool  string
	Error string
}

// SubagentRun is a controller-owned record of one slash-subagent lifecycle.
type SubagentRun struct {
	ID        string
	Skill     string
	Alias     string
	Prompt    string // raw slash command shown in transcript
	Task      string // composed child task
	StartedAt time.Time
	EndedAt   time.Time
	State     event.SubagentState
	Answer    string
	Err       string
	Events    []SubagentEvent
	cancel    context.CancelFunc
}

// SubagentSummary is a frontend-safe snapshot returned by ListSubagents.
type SubagentSummary struct {
	ID            string
	Skill         string
	Alias         string
	State         event.SubagentState
	StartedAt     time.Time
	EndedAt       time.Time
	PromptPreview string
	Cancelable    bool
}

// SubagentDetail is an immutable frontend-safe snapshot of a retained run.
type SubagentDetail struct {
	ID         string
	Skill      string
	Alias      string
	Prompt     string
	Task       string
	StartedAt  time.Time
	EndedAt    time.Time
	State      event.SubagentState
	Answer     string
	Err        string
	Events     []SubagentEvent
	Cancelable bool
}

func (c *Controller) registerSubagent(id, skillName, alias, prompt, task string, cancel context.CancelFunc) *SubagentRun {
	c.subagentMu.Lock()
	defer c.subagentMu.Unlock()
	if c.subagentReg == nil {
		c.subagentReg = make(map[string]*SubagentRun)
	}
	run := &SubagentRun{
		ID:        id,
		Skill:     skillName,
		Alias:     alias,
		Prompt:    prompt,
		Task:      task,
		StartedAt: time.Now(),
		State:     event.SubagentRunning,
		cancel:    cancel,
	}
	c.subagentReg[id] = run
	return run
}

func (c *Controller) finalizeSubagent(run *SubagentRun, answer string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subagentMu.Lock()
	defer c.subagentMu.Unlock()
	c.finalizeSubagentStateLocked(run, answer, err)
	if c.running {
		c.pendingCompletions = append(c.pendingCompletions, run)
		return
	}
	c.mergeSubagentTranscript(run)
}

func (c *Controller) finalizeSubagentStateLocked(run *SubagentRun, answer string, err error) {
	if run.State == event.SubagentCanceled {
		if run.Err == "" {
			run.Err = i18n.M.SubagentCanceledByUser
		}
	} else if reason, blocked := subagentHookBlockedReason(err); blocked {
		run.State = event.SubagentCanceled
		run.Err = reason
	} else if errors.Is(err, context.Canceled) {
		run.State = event.SubagentCanceled
		run.Err = i18n.M.SubagentCanceledByUser
	} else if err != nil {
		run.State = event.SubagentFailed
		run.Err = err.Error()
	} else {
		run.State = event.SubagentCompleted
		run.Answer = answer
		if strings.TrimSpace(answer) != "" && !subagentEventsContainMessage(run.Events, answer) {
			run.Events = append(run.Events, SubagentEvent{
				Time: time.Now(),
				Kind: event.Message,
				Text: answer,
			})
			if len(run.Events) > maxSubagentEvents {
				run.Events = run.Events[len(run.Events)-maxSubagentEvents:]
			}
		}
	}
	run.EndedAt = time.Now()
	run.cancel = nil
}

func (c *Controller) subagentState(id string) event.SubagentState {
	c.subagentMu.Lock()
	defer c.subagentMu.Unlock()
	if run, ok := c.subagentReg[id]; ok {
		return run.State
	}
	return ""
}

func (c *Controller) mergeSubagentTranscript(run *SubagentRun) {
	if c.executor == nil || run.State != event.SubagentCompleted {
		return
	}
	s := c.executor.Session()
	s.Add(provider.Message{Role: provider.RoleUser, Content: run.Prompt})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: run.Answer})
}

func (c *Controller) finishGuardedTurnLocked() {
	c.flushPendingCompletionsLocked()
	c.running = false
	c.cancel = nil
	c.canceling = false
}

func (c *Controller) flushPendingCompletionsLocked() {
	c.subagentMu.Lock()
	defer c.subagentMu.Unlock()
	for _, run := range c.pendingCompletions {
		c.mergeSubagentTranscript(run)
	}
	c.pendingCompletions = nil
}

func (c *Controller) ListSubagents() []SubagentSummary {
	c.subagentMu.Lock()
	defer c.subagentMu.Unlock()
	out := make([]SubagentSummary, 0, len(c.subagentReg))
	for _, run := range c.subagentReg {
		preview := run.Prompt
		runes := []rune(preview)
		if len(runes) > 80 {
			preview = string(runes[:80]) + "..."
		}
		out = append(out, SubagentSummary{
			ID:            run.ID,
			Skill:         run.Skill,
			Alias:         run.Alias,
			State:         run.State,
			StartedAt:     run.StartedAt,
			EndedAt:       run.EndedAt,
			PromptPreview: preview,
			Cancelable:    run.State == event.SubagentRunning,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

func (c *Controller) CancelSubagent(id string) {
	c.subagentMu.Lock()
	defer c.subagentMu.Unlock()
	run, ok := c.subagentReg[id]
	if !ok || run.State != event.SubagentRunning {
		return
	}
	run.State = event.SubagentCanceled
	run.EndedAt = time.Now()
	run.Err = i18n.M.SubagentCanceledByUser
	cancel := run.cancel
	run.cancel = nil
	if cancel != nil {
		cancel()
	}
}

func (c *Controller) SubagentDetail(id string) (SubagentDetail, bool) {
	c.subagentMu.Lock()
	defer c.subagentMu.Unlock()
	run, ok := c.subagentReg[id]
	if !ok {
		return SubagentDetail{}, false
	}
	return subagentDetailSnapshot(run), true
}

func (c *Controller) ClearSubagents(state string) {
	c.subagentMu.Lock()
	defer c.subagentMu.Unlock()
	for id, run := range c.subagentReg {
		remove := false
		switch state {
		case "all":
			remove = run.State != event.SubagentRunning
		case "completed":
			remove = run.State == event.SubagentCompleted
		case "failed":
			remove = run.State == event.SubagentFailed
		case "canceled":
			remove = run.State == event.SubagentCanceled
		}
		if remove {
			delete(c.subagentReg, id)
		}
	}
}

func (c *Controller) resolveSubagentRef(ref string) (SubagentDetail, error) {
	c.subagentMu.Lock()
	defer c.subagentMu.Unlock()
	if run, ok := c.subagentReg[ref]; ok {
		return subagentDetailSnapshot(run), nil
	}
	var matches []*SubagentRun
	for _, run := range c.subagentReg {
		if strings.EqualFold(run.Alias, ref) {
			matches = append(matches, run)
		}
	}
	switch len(matches) {
	case 0:
		return SubagentDetail{}, nil
	case 1:
		return subagentDetailSnapshot(matches[0]), nil
	default:
		return SubagentDetail{}, fmt.Errorf("ambiguous ref %q matches %d runs", ref, len(matches))
	}
}

func subagentDetailSnapshot(run *SubagentRun) SubagentDetail {
	events := append([]SubagentEvent(nil), run.Events...)
	return SubagentDetail{
		ID:         run.ID,
		Skill:      run.Skill,
		Alias:      run.Alias,
		Prompt:     run.Prompt,
		Task:       run.Task,
		StartedAt:  run.StartedAt,
		EndedAt:    run.EndedAt,
		State:      run.State,
		Answer:     run.Answer,
		Err:        run.Err,
		Events:     events,
		Cancelable: run.State == event.SubagentRunning,
	}
}

func (c *Controller) appendSubagentEvent(id string, detail SubagentEvent) {
	c.subagentMu.Lock()
	defer c.subagentMu.Unlock()
	run, ok := c.subagentReg[id]
	if !ok {
		return
	}
	if detail.Time.IsZero() {
		detail.Time = time.Now()
	}
	run.Events = append(run.Events, detail)
	if len(run.Events) > maxSubagentEvents {
		run.Events = run.Events[len(run.Events)-maxSubagentEvents:]
	}
}

func subagentEventsContainMessage(events []SubagentEvent, answer string) bool {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return true
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind != event.Message {
			continue
		}
		if strings.TrimSpace(events[i].Text) == answer {
			return true
		}
	}
	return false
}
