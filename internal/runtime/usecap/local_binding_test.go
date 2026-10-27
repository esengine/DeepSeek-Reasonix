package usecap_test

import (
	"encoding/json"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/usecap"
)

func TestLocalToolBindingDoesNotLeakAcrossFrontendClones(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeRegistryTool{name: "fixture", readOnly: true})
	parent := usecap.NewUseCapabilityTool(t.Context(), nil, nil, reg, nil, nil, nil)
	parentCalls, childCalls := 0, 0
	parent.BindLocalTools(func(tl tool.Tool) tool.Tool { parentCalls++; return tl })
	child := parent.CloneForAgent(nil, nil)
	child.BindLocalTools(func(tl tool.Tool) tool.Tool { childCalls++; return tl })
	grandchild := child.CloneForAgent(nil, nil)
	args := json.RawMessage(`{"action":"call","capability_id":"tool:fixture"}`)
	for _, proxy := range []*usecap.UseCapabilityTool{parent, child, grandchild} {
		resolved, err := proxy.ResolveCall(t.Context(), args)
		if err != nil || resolved.Target == nil || !resolved.ReadOnly {
			t.Fatalf("resolve: %+v; %v", resolved, err)
		}
	}
	if parentCalls != 1 || childCalls != 1 {
		t.Fatalf("frontend inherited another actor's binding: parent=%d child=%d", parentCalls, childCalls)
	}
}

func TestLocalToolBindingCannotRestoreADisabledTool(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeRegistryTool{name: "fixture", readOnly: true})
	reg.DisableTools([]string{"fixture"})
	proxy := usecap.NewUseCapabilityTool(t.Context(), nil, nil, reg, nil, nil, nil)
	proxy.BindLocalTools(func(tl tool.Tool) tool.Tool {
		t.Error("binding ran before the disabled-tool boundary")
		return tl
	})
	resolved, err := proxy.ResolveCall(t.Context(), json.RawMessage(`{"action":"call","capability_id":"tool:fixture"}`))
	if err != nil || !resolved.SkipExecute || !resolved.Unavailable || resolved.Target != nil {
		t.Fatalf("disabled tool restored: %+v; %v", resolved, err)
	}
}
