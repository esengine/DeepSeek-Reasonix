package agent

import (
	"context"
	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/sessionstore"
	"sync/atomic"
)

// windowHost is what the context window asks of the loop around it. It is the
// whole of that dependency: anything else the window reads is its own state,
// the session, the configuration or the services it was built with.
type windowHost interface {
	withTodoIdentityTail(visible []provider.Message) []provider.Message
	withHostContextTail(visible []provider.Message) []provider.Message
	toolFactsFor(name string) evidence.ToolFacts
	providerProjectionMessages(msgs []provider.Message) []provider.Message
	prepareSamplingRequest(ctx context.Context) (samplingRequest, error)
	interceptCompactionPrepare(ctx context.Context, fold []provider.Message, guidance string) ([]provider.Message, string, error)
	interceptCompactionComplete(ctx context.Context, summary string) (string, error)
	annotateFailureDiagnostics(ctx context.Context, region []provider.Message, protected []bool) []provider.Message
	Session() *sessionstore.Session
}

// contextWindow is the context window's view of one agent: its own state and
// the few things it borrows from the loop, through windowHost.
type contextWindow struct {
	*agentConfig
	windowHost
	sess                   *sessionRuntime
	svc                    *agentServices
	ablation               ablation.Set
	keepPolicy             KeepPolicy
	strictAlternatingRoles bool
	activeTurnCreatedAt    *atomic.Int64
	windowProbe            *contextWindowProbe
}

// window is the agent's context window, built per entry so it never holds a
// stale view of a swapped session.
func (a *Agent) window() *contextWindow {
	return &contextWindow{
		agentConfig: &a.agentConfig, windowHost: a, sess: &a.sess, svc: &a.svc,
		ablation: a.ablation, keepPolicy: a.keepPolicy, strictAlternatingRoles: a.strictAlternatingRoles,
		activeTurnCreatedAt: &a.activeTurnCreatedAt, windowProbe: &a.windowProbe,
	}
}
