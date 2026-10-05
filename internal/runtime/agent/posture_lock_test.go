package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension/dispatch"
	"reasonix/internal/state/sessionstore"
)

type lockProbe struct{ name string }

func (p lockProbe) Name() string                                             { return p.name }
func (p lockProbe) Description() string                                      { return "probe" }
func (p lockProbe) Schema() json.RawMessage                                  { return json.RawMessage(`{"type":"object"}`) }
func (p lockProbe) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
func (p lockProbe) ReadOnly() bool                                           { return true }

type lockGate struct{}

func (lockGate) Check(context.Context, string, json.RawMessage, bool) (bool, string, error) {
	return true, "", nil
}

func TestLockPostureAndSurfaceFreezeWhatAssemblyInstalled(t *testing.T) {
	first := tool.NewRegistry()
	first.Add(lockProbe{"a"})
	a := New(&fakeProvider{}, first, sessionstore.NewSession(""), Options{}, event.Discard)
	a.SetGate(lockGate{})

	a.LockPosture()
	a.SetGate(nil)
	a.SetAsker(nil)
	if a.svc.gate == nil {
		t.Fatal("SetGate replaced the gate after LockPosture")
	}

	other := tool.NewRegistry()
	other.Add(lockProbe{"b"})
	dispatcher := &dispatch.Dispatcher{}
	a.SetTools(other)
	a.SetExtensions(dispatcher)
	if a.svc.tools == first && a.svc.extensions == dispatcher {
		t.Fatal("test cannot tell: neither setter ran before the lock")
	}

	a.LockSurface()
	a.SetTools(first)
	a.SetExtensions(nil)
	if a.svc.tools != other || a.svc.extensions != dispatcher {
		t.Fatal("SetTools or SetExtensions changed the surface after LockSurface")
	}
}
