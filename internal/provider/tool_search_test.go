package provider

import (
	"context"
	"testing"
)

func TestNativeToolSearchDefaultOffKeepsVisibleBytes(t *testing.T) {
	visible := []ToolSchema{{Name: "use_capability", Description: "proxy", Parameters: []byte(`{"type":"object"}`)}}
	extra := []ToolSchema{{Name: "mcp__s__t", Description: "t", Parameters: []byte(`{"type":"object"}`)}}
	got := ApplyNativeToolSearch(visible, extra, nil)
	if len(got) != 1 || got[0].Name != "use_capability" || got[0].Deferred {
		t.Fatalf("preview off must keep fixed proxy only: %+v", got)
	}
}

func TestNativeToolSearchUnsupportedProviders(t *testing.T) {
	if NativeToolSearchSupported(namedProvider("openai")) {
		t.Fatal("provider names must not imply protocol support")
	}
}

type namedProvider string

func (p namedProvider) Name() string                                          { return string(p) }
func (p namedProvider) Stream(context.Context, Request) (<-chan Chunk, error) { return nil, nil }

func TestNativeToolSearchPreviewMarksDeferred(t *testing.T) {
	restore := SetNativeToolSearchPreviewForTest(true)
	defer restore()
	if NativeToolSearchEnabled(nil) {
		t.Fatal("nil provider must stay disabled")
	}
}
