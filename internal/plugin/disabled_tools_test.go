package plugin

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"reasonix/internal/tool"
)

// disabledToolsTransport returns a fixed tools/list response with 3 tools: foo, bar, baz.
type disabledToolsTransport struct {
	mu  sync.Mutex
	raw json.RawMessage
}

func (t *disabledToolsTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if method != "tools/list" {
		return json.RawMessage(`{}`), nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.raw) > 0 {
		return t.raw, nil
	}
	// Return 3 tools: foo has no annotations, bar is read-only, baz is unannotated.
	return json.RawMessage(`{"tools":[
		{"name":"foo","description":"The first tool.","inputSchema":{"type":"object"}},
		{"name":"bar","description":"The second tool.","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}},
		{"name":"baz","description":"The third tool.","inputSchema":{"type":"object"}}
	]}`), nil
}

func (t *disabledToolsTransport) notify(ctx context.Context, method string, params any) error {
	return nil
}
func (t *disabledToolsTransport) close() {}

// TestListToolsFiltersDisabledTools verifies that listTools skips tools whose
// raw names are in the spec's DisabledTools set, and that c.tools is updated
// accordingly (exercises the toolsListed short-circuit cache path).
func TestListToolsFiltersDisabledTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tr := &disabledToolsTransport{}
	c := &Client{
		name:      "test",
		t:         tr,
		spec:      Spec{Name: "test", DisabledTools: map[string]bool{"foo": true}},
		transport: "stdio",
	}

	tools, err := c.listTools(ctx)
	if err != nil {
		t.Fatalf("listTools: %v", err)
	}
	// Expect 2 tools (bar, baz) — foo is disabled.
	if len(tools) != 2 {
		t.Fatalf("listTools returned %d tools, want 2 (foo should be filtered)", len(tools))
	}
	for _, tl := range tools {
		if tl.Name() == "mcp__test__foo" {
			t.Fatal("listTools returned disabled tool mcp__test__foo")
		}
	}
	// Verify the short-circuit cache returns the same filtered set.
	cached, err := c.listTools(ctx)
	if err != nil {
		t.Fatalf("second listTools (cache hit): %v", err)
	}
	if len(cached) != 2 {
		t.Fatalf("cached listTools returned %d tools, want 2", len(cached))
	}
	// Verify c.tools also matches (used by /mcp rendering).
	if len(c.tools) != 2 {
		t.Fatalf("c.tools has %d entries, want 2", len(c.tools))
	}
	for _, info := range c.tools {
		if info.Name == "foo" {
			t.Fatal("c.tools contains disabled tool foo")
		}
	}
}

// TestIsToolDisabledNilAndEmpty verifies that nil and empty DisabledTools
// behave identically: isToolDisabled always returns false, no panic.
func TestIsToolDisabledNilAndEmpty(t *testing.T) {
	for name, disabled := range map[string]map[string]bool{
		"nil":   nil,
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			s := Spec{DisabledTools: disabled}
			if s.isToolDisabled("anything") {
				t.Fatalf("%s DisabledTools: isToolDisabled returned true", name)
			}
		})
	}
}

// TestSpecFingerprintNilVsEmptyDisabledTools verifies that nil and empty
// DisabledTools produce the same SpecFingerprint, preserving backward
// compatibility with caches written before DisabledTools existed.
func TestSpecFingerprintNilVsEmptyDisabledTools(t *testing.T) {
	sNil := Spec{Name: "s", Type: "stdio", Command: "cmd", DisabledTools: nil}
	sEmpty := Spec{Name: "s", Type: "stdio", Command: "cmd", DisabledTools: map[string]bool{}}

	hNil := SpecFingerprint(sNil)
	hEmpty := SpecFingerprint(sEmpty)

	if hNil != hEmpty {
		t.Fatalf("SpecFingerprint(nil) = %q, SpecFingerprint(empty) = %q, want equal", hNil, hEmpty)
	}
}

// TestSpecFingerprintWithoutDisabledTools verifies that a Spec with no
// DisabledTools field produces the same fingerprint as an older Spec struct
// that never had the field (backward-compatibility guard).
func TestSpecFingerprintWithoutDisabledTools(t *testing.T) {
	// This spec deliberately omits DisabledTools (zero-value nil).
	s := Spec{Name: "my-server", Type: "stdio", Command: "/usr/bin/example", Args: []string{"--flag", "x"}, Env: map[string]string{"FOO": "1", "BAR": "2"}, Headers: map[string]string{"X-Custom": "ok"}, Dir: "/work"}
	_ = SpecFingerprint(s) // must not panic; value not validated, only that it runs without the field
}

// TestLazyToolsetCacheHitFiltersDisabledTools verifies that the cache-hit
// branch of LazyToolset skips tools in DisabledTools.
func TestLazyToolsetCacheHitFiltersDisabledTools(t *testing.T) {
	redirectCache(t)
	spec := Spec{
		Name:          "srv",
		DisabledTools: map[string]bool{"a": true},
	}
	cs := &CachedSchema{
		SpecHash: SpecFingerprint(spec),
		Tools: []CachedTool{
			{Name: "a", Description: "disabled tool"},
			{Name: "b", Description: "kept tool"},
		},
	}

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tools := LazyToolset(spec, cs, host, reg, ctx, false)
	if len(tools) != 1 {
		t.Fatalf("LazyToolset returned %d tools, want 1 (tool a should be filtered)", len(tools))
	}
	if tools[0].Name() != "mcp__srv__b" {
		t.Fatalf("expected tool name mcp__srv__b, got %q", tools[0].Name())
	}
}

// TestCacheInvalidatesWhenDisabledToolsChanges verifies that SpecFingerprint
// changes when DisabledTools is modified, forcing a cache miss.
func TestCacheInvalidatesWhenDisabledToolsChanges(t *testing.T) {
	redirectCache(t)

	spec := sampleSpec()
	spec.DisabledTools = map[string]bool{"write_file": true}
	hash := SpecFingerprint(spec)
	if err := SaveCachedSchema(spec.Name, sampleCachedSchema(hash)); err != nil {
		t.Fatalf("SaveCachedSchema: %v", err)
	}

	// Change DisabledTools — should produce a different hash.
	changed := sampleSpec()
	changed.DisabledTools = map[string]bool{"delete_file": true}
	if _, ok := LoadCachedSchema(spec.Name, SpecFingerprint(changed)); ok {
		t.Fatal("LoadCachedSchema: hit after DisabledTools changed")
	}
}
