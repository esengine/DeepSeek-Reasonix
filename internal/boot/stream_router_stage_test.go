package boot

import (
	"fmt"
	"testing"

	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/providerext"
	"reasonix/internal/extension/sidecar"
	"reasonix/internal/provider"
)

type baseResolver struct{}

func (baseResolver) Catalog() []provider.Descriptor { return nil }
func (baseResolver) Resolve(provider.Selection) (provider.Provider, error) {
	return nil, fmt.Errorf("no model")
}

// namedRouter is a distinct StreamRouter identity for before/after assertions.
type namedRouter struct{ name string }

func (n *namedRouter) RouteStreamChunk(protocol.StreamChunkParams) {}
func (n *namedRouter) RouteStreamEnd(protocol.StreamEndParams)     {}

func TestMergeDoesNotInstallStreamRouterCommitDoes(t *testing.T) {
	old := &namedRouter{name: "old-generation"}
	client := sidecar.NewProbeClient("demo", []protocol.ProviderDescriptor{{
		Ref:   "plugin/demo/fake/x",
		Model: "x",
	}}, old)
	if client.StreamRouter() != old {
		t.Fatal("precondition: client must start with old router")
	}

	mgr := &sidecar.Manager{}
	if err := mgr.Adopt("demo", client); err != nil {
		t.Fatal(err)
	}

	// Stage merge: construct resolver only — must not touch client router.
	merged, err := mergeSidecarProviders(baseResolver{}, mgr, nil)
	if err != nil {
		t.Fatalf("mergeSidecarProviders: %v", err)
	}
	if merged == nil {
		t.Fatal("expected merged resolver when client declares providers")
	}
	if got := client.StreamRouter(); got != old {
		t.Fatalf("after merge router = %T, want old generation router", got)
	}

	// Commit install: only then switch away from the old router.
	installSidecarStreamRouters(mgr, merged)
	got := client.StreamRouter()
	if got == old {
		t.Fatal("after commit install, router must not still be the old generation")
	}
	newRouter, ok := any(merged).(sidecar.StreamRouter)
	if !ok {
		t.Fatal("merged resolver must implement sidecar.StreamRouter")
	}
	if got != newRouter {
		t.Fatalf("after commit router = %T, want merged providerext resolver", got)
	}
}

func TestFailedStageLeavesOldRouterForAdoptedClient(t *testing.T) {
	// Stage merge + fail before commit: router stays pre-stage. Real wire
	// chunk/end after rollback: sidecar.TestRollbackAfterStageKeepsOldRouterConsumingRealStream.
	old := &namedRouter{name: "old-gen"}
	client := sidecar.NewProbeClient("keep", []protocol.ProviderDescriptor{{
		Ref: "plugin/keep/fake/x", Model: "x",
	}}, old)

	prev := &sidecar.Manager{}
	_ = prev.Adopt("keep", client)

	next := &sidecar.Manager{}
	// Adopt unchanged (as StartPackagesWithPlan does).
	if c := prev.Detach("keep"); c != nil {
		_ = next.Adopt("keep", c)
		// record as planAdopted via Rollback path: set by StartPackagesWithPlan;
		// for unit test call merge on next then rollback without install.
	}
	if _, err := mergeSidecarProviders(baseResolver{}, next, nil); err != nil {
		t.Fatal(err)
	}
	if client.StreamRouter() != old {
		t.Fatal("stage merge must not rewrite adopted client router")
	}
	// Failure: reattach without ever calling installSidecarStreamRouters.
	if c := next.Detach("keep"); c != nil {
		_ = prev.Adopt("keep", c)
	}
	if prev.Client("keep") != client {
		t.Fatal("client should be back on previous manager")
	}
	if client.StreamRouter() != old {
		t.Fatal("after rollback, router must still be old generation")
	}
}

func TestMergeSidecarProvidersDoesNotInstallRouter(t *testing.T) {
	installSidecarStreamRouters(nil, nil)
	installSidecarStreamRouters(&sidecar.Manager{}, nil)
	installSidecarStreamRouters(nil, baseResolver{})
}

func TestInstallSidecarStreamRoutersAcceptsProviderext(t *testing.T) {
	r, err := providerext.New(baseResolver{}, func() []providerext.ProviderClient { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	installSidecarStreamRouters(&sidecar.Manager{}, r)
	if _, ok := any(r).(sidecar.StreamRouter); !ok {
		t.Fatal("providerext.Resolver must implement sidecar.StreamRouter")
	}
}
