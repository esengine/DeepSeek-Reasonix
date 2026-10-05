package usecap

import (
	"context"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
)

// serve builds a controller from r.Context() on a workspace switch, a model
// switch, and every hub pane. That context is done once the response is written,
// while the runtime it hands the MCP substrate owns children for the session:
// every later on-demand connect failed in milliseconds, having started nothing.
func TestTheMCPLifetimeOutlivesTheRequestThatBuiltIt(t *testing.T) {
	built, cancel := context.WithCancel(context.Background())
	rt := NewMCPCapabilityRuntime(context.WithoutCancel(built), plugin.NewHost(), nil, tool.NewRegistry(), nil)
	cancel() // the HTTP response is written

	if rt.lifeCtx == nil {
		t.Fatal("no lifetime context was kept")
	}
	select {
	case <-rt.lifeCtx.Done():
		t.Fatal("the session's MCP lifetime died with the request that built it")
	default:
	}
	// A frontend made from it inherits the same lifetime, which is what the
	// on-demand connect actually launches under.
	front := rt.NewFrontend(nil, nil)
	select {
	case <-front.lifeCtx.Done():
		t.Fatal("the use_capability frontend inherited a dead lifetime")
	default:
	}
}

// The unfixed shape, kept so the property above cannot quietly become vacuous.
func TestARequestScopedLifetimeIsDeadOnArrival(t *testing.T) {
	req, cancel := context.WithCancel(context.Background())
	rt := NewMCPCapabilityRuntime(req, plugin.NewHost(), nil, tool.NewRegistry(), nil)
	cancel()
	select {
	case <-rt.lifeCtx.Done():
	default:
		t.Fatal("fixture no longer reproduces the shape this guards against")
	}
}
