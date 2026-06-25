package sandbox

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type SandboxContext struct {
	MaxSteps      int `json:"max_steps"`
	MaxTimeMs     int `json:"max_time_ms"`
	MemoryLimit   int `json:"memory_limit"`
	ToolCallLimit int `json:"tool_call_limit"`
}

type Execution struct {
	cfg          SandboxContext
	startedAt    time.Time
	steps        int
	toolCalls    int
	killReason   string
	terminatedAt time.Time
}

func DefaultContext() SandboxContext {
	return SandboxContext{
		MaxSteps:      12,
		MaxTimeMs:     10 * 60 * 1000,
		MemoryLimit:   300,
		ToolCallLimit: 20,
	}
}

func Start(cfg SandboxContext, now time.Time) *Execution {
	cfg = normalize(cfg)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return &Execution{cfg: cfg, startedAt: now.UTC()}
}

func (e *Execution) Step(now time.Time) error {
	if err := e.Check(now); err != nil {
		return err
	}
	e.steps++
	if e.steps > e.cfg.MaxSteps {
		e.kill("max steps exceeded", now)
		return fmt.Errorf("sandbox blocked execution: max steps exceeded (%d>%d)", e.steps, e.cfg.MaxSteps)
	}
	return nil
}

func (e *Execution) AddToolCalls(n int, now time.Time) error {
	if n <= 0 {
		return e.Check(now)
	}
	if err := e.Check(now); err != nil {
		return err
	}
	e.toolCalls += n
	if e.toolCalls > e.cfg.ToolCallLimit {
		e.kill("tool call limit exceeded", now)
		return fmt.Errorf("sandbox blocked execution: tool call limit exceeded (%d>%d)", e.toolCalls, e.cfg.ToolCallLimit)
	}
	return nil
}

func (e *Execution) Check(now time.Time) error {
	if e == nil {
		return errors.New("sandbox execution is nil")
	}
	if e.killReason != "" {
		return fmt.Errorf("sandbox execution terminated: %s", e.killReason)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if elapsed := now.UTC().Sub(e.startedAt); elapsed > time.Duration(e.cfg.MaxTimeMs)*time.Millisecond {
		e.kill("max time exceeded", now)
		return fmt.Errorf("sandbox blocked execution: max time exceeded (%s>%dms)", elapsed, e.cfg.MaxTimeMs)
	}
	return nil
}

func (e *Execution) Kill(reason string, now time.Time) {
	if reason == "" {
		reason = "manual kill switch"
	}
	e.kill(reason, now)
}

func (e *Execution) Snapshot() ExecutionSnapshot {
	if e == nil {
		return ExecutionSnapshot{}
	}
	return ExecutionSnapshot{
		Context:      e.cfg,
		StartedAt:    e.startedAt,
		Steps:        e.steps,
		ToolCalls:    e.toolCalls,
		KillReason:   e.killReason,
		TerminatedAt: e.terminatedAt,
	}
}

type ExecutionSnapshot struct {
	Context      SandboxContext    `json:"context"`
	StartedAt    time.Time         `json:"started_at"`
	Steps        int               `json:"steps"`
	ToolCalls    int               `json:"tool_calls"`
	KillReason   string            `json:"kill_reason,omitempty"`
	TerminatedAt time.Time         `json:"terminated_at,omitempty"`
	Isolation    IsolationSnapshot `json:"isolation,omitempty"`
}

type IsolationPolicy struct {
	GoroutineContainment  bool `json:"goroutine_containment"`
	StrictContextClone    bool `json:"strict_context_clone"`
	NoSharedPointerEscape bool `json:"no_shared_pointer_escape"`
	OSProcessIsolation    bool `json:"os_process_isolation,omitempty"`
}

type IsolationSnapshot struct {
	Policy        IsolationPolicy `json:"policy,omitempty"`
	Completed     bool            `json:"completed,omitempty"`
	TimedOut      bool            `json:"timed_out,omitempty"`
	Panic         string          `json:"panic,omitempty"`
	PotentialLeak bool            `json:"potential_leak,omitempty"`
	EscapeReport  EscapeReport    `json:"escape_report,omitempty"`
}

type EscapeReport struct {
	Active        []EscapeFinding `json:"active,omitempty"`
	ResidualRisks []EscapeFinding `json:"residual_risks,omitempty"`
}

type EscapeFinding struct {
	Class    string `json:"class"`
	Severity string `json:"severity"`
	Evidence string `json:"evidence"`
}

func DefaultIsolationPolicy() IsolationPolicy {
	return IsolationPolicy{
		GoroutineContainment:  true,
		StrictContextClone:    true,
		NoSharedPointerEscape: true,
		OSProcessIsolation:    false,
	}
}

func RunIsolated(parent context.Context, cfg SandboxContext, now time.Time, fn func(context.Context) error) (ExecutionSnapshot, error) {
	if parent == nil {
		parent = context.Background()
	}
	exec := Start(cfg, now)
	if err := parent.Err(); err != nil {
		exec.Kill("parent context canceled", now)
		snap := exec.Snapshot()
		snap.Isolation = IsolationSnapshot{Policy: DefaultIsolationPolicy(), Completed: true}
		return snap, err
	}
	if fn == nil {
		snap := exec.Snapshot()
		snap.Isolation = IsolationSnapshot{Policy: DefaultIsolationPolicy(), Completed: true}
		return snap, nil
	}
	ctx, cancel := isolatedContext(parent, exec.cfg, exec.startedAt)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				done <- fmt.Errorf("sandbox panic: %v", recovered)
			}
		}()
		done <- fn(ctx)
	}()
	var err error
	isolation := IsolationSnapshot{Policy: DefaultIsolationPolicy()}
	select {
	case err = <-done:
		isolation.Completed = true
	case <-ctx.Done():
		err = ctx.Err()
		isolation.TimedOut = true
		exec.Kill("isolated context canceled", time.Now().UTC())
		select {
		case <-done:
			isolation.Completed = true
		case <-time.After(10 * time.Millisecond):
			isolation.PotentialLeak = true
		}
	}
	if err != nil && stringsHasPrefix(err.Error(), "sandbox panic:") {
		isolation.Panic = err.Error()
		exec.Kill("panic", time.Now().UTC())
	}
	isolation.EscapeReport = ClassifyEscapeRisks(isolation)
	snap := exec.Snapshot()
	snap.Isolation = isolation
	return snap, err
}

func ClassifyEscapeRisks(isolation IsolationSnapshot) EscapeReport {
	report := EscapeReport{}
	if isolation.PotentialLeak {
		report.Active = append(report.Active, EscapeFinding{
			Class:    "goroutine_leak",
			Severity: "high",
			Evidence: "isolated execution did not terminate after cancellation",
		})
	}
	if isolation.Panic != "" {
		report.Active = append(report.Active, EscapeFinding{
			Class:    "panic_escape",
			Severity: "high",
			Evidence: isolation.Panic,
		})
	}
	if isolation.TimedOut {
		report.Active = append(report.Active, EscapeFinding{
			Class:    "deadline_escape",
			Severity: "medium",
			Evidence: "isolated execution reached sandbox deadline",
		})
	}
	if !isolation.Policy.OSProcessIsolation {
		report.ResidualRisks = append(report.ResidualRisks, EscapeFinding{
			Class:    "process_boundary_absent",
			Severity: "medium",
			Evidence: "sandbox is enforced inside the Go runtime, not an OS process boundary",
		})
	}
	return report
}

func HasActiveEscape(report EscapeReport) bool {
	return len(report.Active) > 0
}

func isolatedContext(parent context.Context, cfg SandboxContext, now time.Time) (context.Context, context.CancelFunc) {
	deadline := now.Add(time.Duration(normalize(cfg).MaxTimeMs) * time.Millisecond)
	if parentDeadline, ok := parent.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func (e *Execution) kill(reason string, now time.Time) {
	if e.killReason != "" {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	e.killReason = reason
	e.terminatedAt = now.UTC()
}

func normalize(cfg SandboxContext) SandboxContext {
	def := DefaultContext()
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = def.MaxSteps
	}
	if cfg.MaxTimeMs <= 0 {
		cfg.MaxTimeMs = def.MaxTimeMs
	}
	if cfg.MemoryLimit <= 0 {
		cfg.MemoryLimit = def.MemoryLimit
	}
	if cfg.ToolCallLimit <= 0 {
		cfg.ToolCallLimit = def.ToolCallLimit
	}
	return cfg
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
