package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/observe"
	"reasonix/internal/safety/permission"
)

// ErrObservePosture is what a controller built under the read-only posture
// answers to an operation that would widen it: a live MCP connection, or a
// gate or asker that a person could answer through.
var ErrObservePosture = errors.New("control: not available under the read-only posture")

// ObserveRun makes a Controller a read-only, unattended run. It is set once at
// construction by the assembly and cannot be changed or lifted afterwards; no
// flag or configuration value reaches it.
type ObserveRun struct {
	Posture observe.Posture
	Pending observe.PendingSink
	Context observe.RunContext
	// stopped is set once the run parked more than it may; see ObserveStopped.
	stopped *atomic.Bool
	// sealed is set once assembly has installed the tools and extensions.
	sealed *atomic.Bool
}

// ObserveStopped reports the identity of what ended the run: observe.ErrParkLimit
// once it asked for a person too many times, nil otherwise.
func (c *Controller) ObserveStopped() error {
	if c == nil || c.observe == nil || !c.observe.stopped.Load() {
		return nil
	}
	return observe.ErrParkLimit
}

// ObservePosture reports the posture this controller runs under. ok is false
// for an ordinary controller.
func (c *Controller) ObservePosture() (observe.Posture, bool) {
	if c == nil || c.observe == nil {
		return observe.Posture{}, false
	}
	return c.observe.Posture, true
}

func cloneObserveRun(o *ObserveRun) *ObserveRun {
	if o == nil {
		return nil
	}
	cp := *o
	cp.stopped, cp.sealed = new(atomic.Bool), new(atomic.Bool)
	return &cp
}

func (c *Controller) scheduledRunBlock() string {
	if c.observe == nil {
		return ""
	}
	return wrapTurnBlock("scheduled-run", c.observe.Context.Block(c.observe.Posture))
}

// bindObserve installs the parking gate and asker on the executor and seals
// the paths a frontend could use to replace them.
func (c *Controller) bindObserve() {
	if c.observe == nil {
		return
	}
	c.mcp.seal(ErrObservePosture)
	c.approval.setMode(ToolApprovalReadOnly)
	if c.executor == nil {
		return
	}
	stop := func() {
		c.observe.stopped.Store(true)
		c.Cancel()
	}
	c.executor.SetGate(newObserveGate(c.policy, c.observe.Pending, stop))
	c.executor.SetAsker(parkingAsker{sink: c.observe.Pending, stop: stop})
	c.executor.LockPosture()
}

// observeGate answers every call for a run nobody watches. Whatever the inner
// gate would have put to a person is recorded for one and refused; nothing is
// ever approved here, by a mode, a rule or a session grant.
type observeGate struct {
	inner *permission.Gate
	sink  observe.PendingSink
	stop  func()
}

func newObserveGate(policy permission.Policy, sink observe.PendingSink, stop func()) *observeGate {
	policy.Mode, policy.ReadOnly, policy.SessionAllow = permission.Deny, true, nil
	return &observeGate{inner: permission.NewGate(policy, denyPermissionApprover{}), sink: sink, stop: stop}
}

func (g *observeGate) Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (bool, string, error) {
	v, err := g.Verdict(ctx, toolName, args, readOnly)
	return v.Allow, v.Reason, err
}

func (g *observeGate) Verdict(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (permission.Verdict, error) {
	if RequiresFreshHumanApprovalTool(toolName) {
		return g.park(toolName, args, observe.RiskHigh), nil
	}
	v, err := g.inner.Verdict(ctx, toolName, args, readOnly)
	if err != nil || v.Allow || v.Code != permission.RefusalUnattended {
		return v, err
	}
	risk := observe.RiskMedium
	if readOnly {
		risk = observe.RiskLow
	}
	return g.park(toolName, args, risk), nil
}

func (g *observeGate) DeniesWriters() bool { return true }

func (g *observeGate) ExplicitlyDenies(toolName string, args json.RawMessage) bool {
	return g.inner.Policy.ExplicitlyDenies(toolName, args)
}

func (g *observeGate) park(toolName string, args json.RawMessage, risk observe.Risk) permission.Verdict {
	stored, err := g.sink.Park(observe.Pending{
		Kind:      observe.KindApproval,
		Source:    toolName,
		Summary:   observe.Sanitize("The run asked to use " + toolName + "."),
		Detail:    observe.Sanitize(clipUTF8(strings.TrimSpace(toolName+" "+permission.Subject(args)), 400)),
		Untrusted: true,
		Risk:      risk,
		Digest:    observe.DigestOf(observe.KindApproval, toolName, string(args)),
	})
	if errors.Is(err, observe.ErrParkLimit) && g.stop != nil {
		g.stop()
	}
	if err != nil {
		return permission.Verdict{Reason: "this call needs a person and this run is unattended; recording it for one failed, so it was refused and did not run. Do the part that needs no approval, then conclude and name what was refused.", Code: permission.RefusalUnattended}
	}
	return permission.Verdict{
		Reason: fmt.Sprintf("this call needs a person and this run is unattended, so it was parked as pending decision %s and did not run. Nobody declined it, and retrying or rewriting it cannot change that. Do the part that needs no approval, then conclude and name what is parked.", stored.ID),
		Code:   permission.RefusalParked,
	}
}

// parkingAsker records a question for a person and reports it unanswered. It
// never returns an answer: an empty selection would read to the model as the
// user declining, and a chosen option as the user deciding.
type parkingAsker struct {
	sink observe.PendingSink
	stop func()
}

func (a parkingAsker) Ask(_ context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error) {
	stored, err := a.sink.Park(observe.Pending{
		Kind:      observe.KindAsk,
		Source:    "ask",
		Summary:   "The run asked a question that only a person can answer.",
		Detail:    observe.Sanitize(clipUTF8(renderQuestions(questions), 4000)),
		Untrusted: true,
		Risk:      observe.RiskLow,
		Digest:    observe.DigestOf(observe.KindAsk, "ask", renderQuestions(questions)),
	})
	if errors.Is(err, observe.ErrParkLimit) && a.stop != nil {
		a.stop()
	}
	if err != nil {
		return nil, errors.New("this run is unattended and the question could not be recorded for a person, so it is unanswered; do not choose for them. Do the part the task allows without it, then conclude and name the decision that is missing")
	}
	return nil, fmt.Errorf("%w as pending decision %s: this run is unattended and the answer is the user's to give, so nothing here can supply it, including you. Do not choose on their behalf. Do whatever the task allows without it, then conclude and name the decision that is parked", observe.ErrParked, stored.ID)
}

func renderQuestions(questions []event.AskQuestion) string {
	var b strings.Builder
	for i, q := range questions {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if h := strings.TrimSpace(q.Header); h != "" {
			b.WriteString(h + ": ")
		}
		b.WriteString(strings.TrimSpace(q.Prompt))
		for _, o := range q.Options {
			b.WriteString("\n- " + strings.TrimSpace(o.Label))
		}
	}
	return b.String()
}

// SealObserveSurface is called by assembly after it has installed the tool
// registry and the extension dispatcher. From then on neither the agent nor the
// controller replaces them. It does nothing for an ordinary controller.
func (c *Controller) SealObserveSurface() {
	if c == nil || c.observe == nil {
		return
	}
	c.observe.sealed.Store(true)
	if c.executor != nil {
		c.executor.LockSurface()
	}
}

func (c *Controller) observeSealed() bool {
	return c.observe != nil && c.observe.sealed.Load()
}
