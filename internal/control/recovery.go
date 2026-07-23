package control

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"reasonix/internal/agent"
	"reasonix/internal/recovery"
)

// ResolveRecovery applies a user decision on an Auto Guard card.
// action is continue|continue_task|revise. For revise, feedback is returned in the
// blocked tool result so the same agent sees it exactly once before retrying.
func (c *Controller) ResolveRecovery(id string, action agent.RecoveryAction, feedback string) error {
	if c == nil {
		return fmt.Errorf("controller is nil")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("empty recovery approval id")
	}
	switch action {
	case agent.RecoveryActionContinue, agent.RecoveryActionContinueTask, agent.RecoveryActionRevise:
	default:
		// Accept plain strings from wire clients.
		switch strings.ToLower(strings.TrimSpace(string(action))) {
		case "continue":
			action = agent.RecoveryActionContinue
		case "continue_task":
			action = agent.RecoveryActionContinueTask
		case "revise":
			action = agent.RecoveryActionRevise
		case "stop":
			// Compatibility for older clients: cancel this proposed mutation.
			// Whole-task cancellation remains on the app's ordinary Stop control.
			action = agent.RecoveryActionRevise
			if strings.TrimSpace(feedback) == "" {
				feedback = "cancel this proposed action"
			}
		default:
			return fmt.Errorf("unknown recovery action %q", action)
		}
	}

	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate == nil {
		return fmt.Errorf("Auto Guard is not active")
	}
	// Host hard-caps free-text feedback; empty revise is filled by the gate.
	// Clip on a UTF-8 boundary so multi-byte runes are never split.
	feedback = clipUTF8(feedback, 4*1024)
	// Validate and resolve the gate first. In particular, an unsupported
	// continue_task must leave the live approval intact so the frontend can
	// recover and offer a one-shot decision instead.
	if err := gate.Resolve(id, recovery.Action(action), feedback); err != nil {
		return err
	}

	// Also resolve any matching approvalManager entry so legacy Approve paths
	// and ReplayPending do not keep a stale prompt.
	pending := c.approval.resolve(id)
	if pending.reply != nil {
		switch action {
		case agent.RecoveryActionContinue, agent.RecoveryActionContinueTask:
			pending.reply <- approvalReply{allow: true}
		default:
			pending.reply <- approvalReply{allow: false}
		}
	}
	return nil
}

func clipUTF8(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	// Walk back to a rune start so the slice stays valid UTF-8.
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// initRecoveryGate constructs the shared recovery gate and attaches it to the
// executor. Called from New when recovery is available.
func (c *Controller) initRecoveryGate(reviewer recovery.Reviewer, headless bool) {
	if c == nil || c.executor == nil {
		return
	}
	gate := recovery.NewGate(recovery.Options{
		Headless: headless,
		Mode: func() string {
			return c.ToolApprovalMode()
		},
		EmitPrompt:     c.emitRecoveryPrompt,
		Reviewer:       reviewer,
		PersistenceKey: c.SessionPath,
		Persist: func(path string, snap recovery.Snapshot) {
			c.persistRecoverySnapshot(path, snap)
		},
		TaskSummary: func() string {
			if c.executor == nil || c.executor.Session() == nil {
				return ""
			}
			msgs := c.executor.Session().Snapshot()
			for i := len(msgs) - 1; i >= 0; i-- {
				if string(msgs[i].Role) == "user" && strings.TrimSpace(msgs[i].Content) != "" {
					text := strings.TrimSpace(msgs[i].Content)
					if len(text) > 800 {
						return text[:800] + "…"
					}
					return text
				}
			}
			return ""
		},
	})
	c.mu.Lock()
	c.recoveryGate = gate
	c.mu.Unlock()
	c.executor.SetRecoveryGate(gate)
}

func (c *Controller) persistRecoverySnapshot(path string, snap recovery.Snapshot) {
	if c == nil {
		return
	}
	if strings.TrimSpace(path) == "" {
		return
	}
	if err := recovery.SaveSnapshot(path, snap); err != nil {
		slog.Warn("controller: recovery snapshot", "err", err)
	}
}

// loadRecoveryState restores the recovery gate sidecar for a session path.
func (c *Controller) loadRecoveryState(path string) {
	if c == nil {
		return
	}
	c.approval.clearKind(recovery.ApprovalKindRecovery)
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate != nil {
		snap := recovery.Snapshot{}
		if strings.TrimSpace(path) != "" {
			loaded, err := recovery.LoadSnapshot(path)
			if err != nil {
				slog.Warn("controller: load recovery snapshot", "err", err)
			} else {
				snap = loaded
			}
		}
		// Missing, empty, or unreadable sidecars must still replace the old
		// in-memory state; otherwise a session switch carries its checkpoint.
		gate.Restore(snap)
	}
}

// resetRecoveryForNewSession clears any failure checkpoint inherited from the
// previous path. Metadata is not created here: richer frontends still need to
// attach topic/scope ownership before the first sidecar write.
func (c *Controller) resetRecoveryForNewSession(path string) {
	if c == nil {
		return
	}
	c.loadRecoveryState(path)
}

// carryRecoveryState moves a tip branch onto a new session identity without
// carrying live approval channels or task-local grants across the boundary.
func (c *Controller) carryRecoveryState(path string) {
	if c == nil {
		return
	}
	c.approval.clearKind(recovery.ApprovalKindRecovery)
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate == nil {
		return
	}
	gate.Restore(gate.Snapshot())
	c.saveRecoveryState(path)
}

func (c *Controller) flushRecoveryPersistence(path string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate != nil {
		gate.FlushPersistence(path)
	}
}

// saveRecoveryState persists the recovery gate sidecar. The independent
// reviewer resets to its fixed system prompt before every review, so persisting
// its transient conversation adds no cache warmth and only creates a second
// transcript-shaped file beside the real session.
func (c *Controller) saveRecoveryState(path string) {
	if c == nil || strings.TrimSpace(path) == "" {
		return
	}
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate != nil {
		// Persist evidence-only projection; never write active Episode locks.
		if err := recovery.SaveSnapshot(path, gate.PersistenceSnapshot()); err != nil {
			slog.Warn("controller: recovery snapshot", "err", err)
		}
	}
}

// RecoveryMetrics returns content-free recovery counters for export/observation.
func (c *Controller) RecoveryMetrics() recovery.Metrics {
	if c == nil {
		return recovery.Metrics{}
	}
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate == nil {
		return recovery.Metrics{}
	}
	return gate.Metrics()
}

// DrainRecoveryMetrics returns only counters recorded since the previous
// drain. Desktop calls this once per completed turn to avoid re-emitting the
// controller's cumulative lifetime totals.
func (c *Controller) DrainRecoveryMetrics() recovery.Metrics {
	if c == nil {
		return recovery.Metrics{}
	}
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate == nil {
		return recovery.Metrics{}
	}
	return gate.DrainMetrics()
}

// ReplayUnresolvedRecoveries is retained for frontend/API compatibility.
// Live prompts are replayed by the ordinary approval manager. After process
// death, the next proposed action is classified again instead of replaying a
// stale one-call authorization.
func (c *Controller) ReplayUnresolvedRecoveries() {
}

func (c *Controller) emitRecoveryPrompt(ctx context.Context, taskID string, pending recovery.PendingProposal, failure *recovery.FailureEvent) (string, error) {
	if c == nil {
		return "", fmt.Errorf("controller is nil")
	}
	// Strict fresh decision: never session/persist grants, never auto-drain on
	// mode switch.
	c.approval.promptMu.Lock()
	// Hold promptMu for the duration of registration+emit only; waiting happens
	// in the recovery gate on its own channel. We deliberately do not block here
	// on the approval reply — ResolveRecovery unblocks the gate.
	ev := recovery.ToEventApproval("", pending, failure)
	id, reply := c.approval.registerDecisionKind(
		pending.Tool,
		recoveryFirstNonEmpty(pending.Subject, pending.Tool),
		recoveryFirstNonEmpty(pending.Rationale, "Auto Guard"),
		true,
		recovery.ApprovalKindRecovery,
		ev.Recovery,
	)
	ev.ID = id
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate != nil {
		// Bind before Emit: some sinks synchronously resolve the event from
		// inside Emit, so binding afterwards loses that decision.
		gate.BindApprovalID(taskID, id)
	}
	// Drain the ordinary approval reply when ResolveRecovery/Approve fires so
	// the channel never leaks; the gate is the real waiter.
	go func() {
		select {
		case <-reply:
		case <-ctx.Done():
			c.approval.cancel(id)
		}
	}()

	c.sink.Emit(c.approvalRequestEvent(ev))
	c.approval.promptMu.Unlock()

	if c.hooks != nil {
		go c.hooks.Notification(ctx, "Auto Guard: confirm the next action", "permission_prompt")
	}
	return id, nil
}

func recoveryFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// beginRecoveryEpisode opens a fresh host-owned Recovery Episode. Failure,
// reviewer, and stop budgets clear; explicit task grants survive. Safe to call
// when recovery is disabled.
func (c *Controller) beginRecoveryEpisode() {
	if c == nil {
		return
	}
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate == nil {
		return
	}
	if ctrl, ok := any(gate).(agent.RecoveryEpisodeControl); ok {
		ctrl.BeginEpisode()
	}
}
