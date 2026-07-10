package control

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/nilutil"
	"reasonix/internal/provider"
)

var (
	ErrSideUnavailable = errors.New("side conversation unavailable")
	ErrSideActive      = errors.New("side conversation already active")
)

const sideIdleTimeoutNotice = "BTW side conversation closed after being idle."

// SideFactory builds a side agent. ctx covers construction only; the agent gets
// a separate per-turn context when SubmitSide calls Run.
type SideFactory func(ctx context.Context, sess *agent.Session, sink event.Sink) (*agent.Agent, error)

type SideState struct {
	Active  bool
	Runtime RuntimeStatus
}

type sideState struct {
	agent     *agent.Agent
	cancel    context.CancelFunc
	running   bool
	canceling bool
	idleTimer *time.Timer
	idleGen   uint64
}

const sideBoundaryPrompt = `You are now in a side conversation.

The inherited parent history is reference context only. Only messages after this boundary are active instructions for this side conversation.
This side conversation is separate from the main conversation.
This side conversation is read-only: do not modify files, run mutating tools, change configuration, or perform other side effects.
Mutation requests must be refused with guidance to return to the main conversation.`

func (c *Controller) StartSide(input string) error {
	input = strings.TrimSpace(input)

	c.mu.Lock()
	if c.side != nil {
		c.mu.Unlock()
		return ErrSideActive
	}
	factory := c.sideFactory
	executor := c.executor
	sink := c.sideSink

	if factory == nil || executor == nil {
		c.mu.Unlock()
		return ErrSideUnavailable
	}
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	var state *sideState
	scopedSink := event.FuncSink(func(e event.Event) {
		c.mu.Lock()
		allow := state != nil && c.side == state
		c.mu.Unlock()
		if allow {
			sink.Emit(e)
		}
	})
	mainSess := executor.Session()
	if mainSess == nil {
		c.mu.Unlock()
		return ErrSideUnavailable
	}
	mainHistory := mainSess.Snapshot()
	if !sideHistoryHasContent(mainHistory) {
		c.mu.Unlock()
		return ErrSideUnavailable
	}

	sideSess := agent.NewSession("")
	for _, msg := range mainHistory {
		sideSess.Add(msg)
	}
	sideSess.Add(provider.Message{Role: provider.RoleUser, Content: sideBoundaryPrompt})

	child, err := factory(context.Background(), sideSess, scopedSink)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	if nilutil.IsNil(child) {
		c.mu.Unlock()
		return ErrSideUnavailable
	}
	child.SetSideReadOnly(true)

	candidate := &sideState{
		agent: child,
	}
	state = candidate
	c.side = state
	if input == "" {
		c.armSideIdleTimerLocked(state)
	}
	c.mu.Unlock()

	if input != "" {
		c.SubmitSide(input)
	}
	return nil
}

func (c *Controller) SubmitSide(input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}

	c.mu.Lock()
	state := c.side
	if state == nil || state.running || nilutil.IsNil(state.agent) {
		c.mu.Unlock()
		return
	}
	c.stopSideIdleTimerLocked(state)
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	state.running = true
	state.canceling = false
	child := state.agent
	sink := c.sideSink
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	c.mu.Unlock()

	go func() {
		var err error
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("internal error: %v", r)
			}
			c.mu.Lock()
			emit := c.side == state
			if emit {
				state.running = false
				state.cancel = nil
				state.canceling = false
			}
			c.mu.Unlock()
			cancel()
			if emit {
				sink.Emit(event.Event{Kind: event.TurnDone, Err: explainError(err)})
				c.mu.Lock()
				if c.side == state && !state.running {
					c.armSideIdleTimerLocked(state)
				}
				c.mu.Unlock()
			}
		}()
		err = child.Run(ctx, input)
	}()
}

func (c *Controller) ReturnFromSide() {
	c.mu.Lock()
	state := c.side
	c.side = nil
	if state != nil && state.cancel != nil {
		state.canceling = true
	}
	if state != nil {
		c.stopSideIdleTimerLocked(state)
	}
	c.mu.Unlock()

	if state != nil && state.cancel != nil {
		state.cancel()
	}
}

// SetSideIdleTimeout updates the idle cleanup timeout for the current and
// future side conversations. A zero or negative timeout disables automatic
// cleanup.
func (c *Controller) SetSideIdleTimeout(timeout time.Duration) {
	if timeout < 0 {
		timeout = 0
	}
	c.mu.Lock()
	c.sideIdleTimeout = timeout
	if state := c.side; state != nil {
		if state.running {
			c.stopSideIdleTimerLocked(state)
		} else {
			c.armSideIdleTimerLocked(state)
		}
	}
	c.mu.Unlock()
}

func (c *Controller) SideState() SideState {
	c.mu.Lock()
	state := c.side
	if state == nil {
		c.mu.Unlock()
		return SideState{}
	}
	running := state.running
	canceling := state.canceling
	c.mu.Unlock()

	return SideState{
		Active: true,
		Runtime: RuntimeStatus{
			Running:         running,
			CancelRequested: canceling,
			Cancellable:     running,
		},
	}
}

func (c *Controller) armSideIdleTimerLocked(state *sideState) {
	if state == nil || c.side != state {
		return
	}
	if state.idleTimer != nil {
		state.idleTimer.Stop()
		state.idleTimer = nil
	}
	state.idleGen++
	timeout := c.sideIdleTimeout
	if timeout <= 0 || state.running {
		return
	}
	gen := state.idleGen
	state.idleTimer = time.AfterFunc(timeout, func() {
		c.expireSideIdle(state, gen)
	})
}

func (c *Controller) stopSideIdleTimerLocked(state *sideState) {
	if state == nil {
		return
	}
	state.idleGen++
	if state.idleTimer != nil {
		state.idleTimer.Stop()
		state.idleTimer = nil
	}
}

func (c *Controller) expireSideIdle(state *sideState, gen uint64) {
	sink := c.sideSink
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	c.mu.Lock()
	if c.side != state || state.running || state.idleGen != gen {
		c.mu.Unlock()
		return
	}
	state.idleTimer = nil
	state.idleGen++
	c.side = nil
	c.mu.Unlock()

	sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: sideIdleTimeoutNotice})
}

func sideHistoryHasContent(msgs []provider.Message) bool {
	for _, msg := range msgs {
		if msg.Role != provider.RoleSystem {
			return true
		}
	}
	return false
}
