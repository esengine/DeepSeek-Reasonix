package agent

import (
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/dispatch"
	"reasonix/internal/state/sessionstore"
)

func modAgent(t *testing.T, point extension.InterceptorPoint, mods map[string]bool) *Agent {
	t.Helper()
	sidecar := extension.Contribution{Kind: extension.KindInterceptor, ID: "x", Source: extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: "p"}}
	d := dispatch.New(map[extension.InterceptorPoint][]extension.Contribution{point: {sidecar}}, nil,
		func(string) dispatch.Client { return nil }, nil, dispatch.Options{Mods: mods})
	return New(nil, tool.NewRegistry(), sessionstore.NewSession(""), Options{Extensions: d, CheckTargetAccess: allowTestFileAccess}, event.Discard)
}

// A mod is a full-trust process with no sandbox: where it subscribes does not
// bound what it can write, so a live mod is an unseen writer on any point.
func TestALiveModIsAnUnseenWriterWhereverItSubscribes(t *testing.T) {
	for _, point := range []extension.InterceptorPoint{extension.PointInputReceive, extension.PointSessionStart, extension.PointToolBefore} {
		a := modAgent(t, point, map[string]bool{"p": true})
		if !a.unseenToolWriter() || !a.checkContract().UnseenWriter() {
			t.Errorf("a mod on %s is not an unseen writer", point)
		}
	}
	if a := modAgent(t, extension.PointInputReceive, nil); a.unseenToolWriter() {
		t.Error("an ordinary sidecar on input.receive became an unseen writer; that boundary is pinned elsewhere")
	}
}
