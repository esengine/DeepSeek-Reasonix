package plugin

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
)

func TestDisabledToolsUseExactRawNamesInLiveAndCachedTools(t *testing.T) {
	for _, tc := range []struct {
		name     string
		disabled []string
		want     []string
		strip    string
	}{
		{"nil", nil, []string{"mcp__mock__echo", "mcp__mock__zed"}, ""},
		{"empty", []string{}, []string{"mcp__mock__echo", "mcp__mock__zed"}, ""},
		{"raw", []string{"echo"}, []string{"mcp__mock__zed"}, ""},
		{"namespaced does not match", []string{"mcp__mock__echo"}, []string{"mcp__mock__echo", "mcp__mock__zed"}, ""},
		{"case sensitive", []string{"Echo"}, []string{"mcp__mock__echo", "mcp__mock__zed"}, ""},
		{"before prefix stripping", []string{"echo"}, []string{"mcp__mock__zed"}, "e"},
		{"all", []string{"echo", "zed"}, []string{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redirectCache(t)
			spec := helperSpec()
			spec.DisabledTools, spec.StripRawPrefix = tc.disabled, tc.strip
			host, tools, err := StartAll(t.Context(), []Spec{spec})
			if err != nil {
				t.Fatal(err)
			}
			defer host.Close()
			names := make([]string, len(tools))
			for i, tool := range tools {
				names[i] = tool.Name()
			}
			if !slices.Equal(names, tc.want) {
				t.Fatalf("live names=%v, want %v", names, tc.want)
			}
			host.Close()
			cs, ok := LoadCachedSchemaForSpec(spec)
			if !ok || len(cs.Tools) != len(tc.want) {
				t.Fatalf("cached schema=%+v, ok=%v", cs, ok)
			}
			for _, ct := range cs.Tools {
				if !spec.ToolEnabled(ct.Name) {
					t.Fatalf("disabled tool cached: %s", ct.Name)
				}
			}
			if len(tc.disabled) != 0 {
				spec.DisabledTools = nil
				if _, ok := LoadCachedSchemaForSpec(spec); ok {
					t.Fatal("reenabling tools reused a filtered schema")
				}
			}
		})
	}
}

func TestDisabledToolsFilterOldLazySnapshotAndCacheMissSwap(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	writeMockCache(t, spec)
	cs, ok := LoadCachedSchemaForSpec(spec)
	if !ok {
		t.Fatal("cache not loaded")
	}
	spec.DisabledTools = []string{"echo"}
	for _, snapshot := range []*CachedSchema{cs, nil} {
		host := NewHost()
		t.Cleanup(host.Close)
		reg := tool.NewRegistry()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		t.Cleanup(cancel)
		for _, lt := range LazyToolset(spec, snapshot, host, reg, ctx, true) {
			reg.Add(lt)
		}
		if _, found := reg.Get("mcp__mock__echo"); found {
			t.Fatal("disabled tool registered from old snapshot")
		}
		waitForServer(t, host, "mock", 5*time.Second)
		deadline := time.Now().Add(time.Second)
		for {
			if _, found := reg.Get("mcp__mock__zed"); found {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("allowed tool missing after handshake")
			}
			time.Sleep(time.Millisecond)
		}
		allowed, _ := reg.Get("mcp__mock__zed")
		if _, err := allowed.Execute(ctx, json.RawMessage(`{}`)); err != nil {
			t.Fatal(err)
		}
		if _, found := reg.Get("mcp__mock__echo"); found {
			t.Fatal("disabled tool returned after handshake")
		}
		host.Close()
		cancel()
	}
}

func TestDisabledToolsBindCacheAndSharedRuntimeIdentity(t *testing.T) {
	a := helperSpec()
	b := a
	b.DisabledTools = []string{"echo", "zed", "echo"}
	c := b
	c.DisabledTools = []string{"zed", "echo"}
	if SchemaCacheKey(a) == SchemaCacheKey(b) || MCPRuntimeSpecMatches(a, b) {
		t.Fatal("disabled policy missing from identity")
	}
	if SchemaCacheKey(b) != SchemaCacheKey(c) || !MCPRuntimeSpecMatches(b, c) {
		t.Fatal("policy order/duplicates should not affect identity")
	}
	c.DisabledTools = []string{}
	if SchemaCacheKey(a) != SchemaCacheKey(c) || !MCPRuntimeSpecMatches(a, c) {
		t.Fatal("nil and empty must have the same identity")
	}
	redirectCache(t)
	host := NewHost()
	defer host.Close()
	if _, err := host.EnsureConnected(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := host.EnsureConnected(t.Context(), b); err == nil {
		t.Fatal("shared host returned tools under a different disabled policy")
	}
}
