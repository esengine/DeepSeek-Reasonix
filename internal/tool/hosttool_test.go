package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestHostToolAdapterSurface(t *testing.T) {
	var executed bool
	ht := HostTool{
		Name:         "browser_read",
		Description:  "read-only browser",
		Schema:       json.RawMessage(`{"type":"object"}`),
		ReadOnly:     true,
		PlanModeSafe: true,
		HostMutation: true,
		Execute: func(ctx context.Context, args json.RawMessage) (string, error) {
			executed = true
			return "ok", nil
		},
	}
	tool := NewHostTool(ht)
	if tool.Name() != "browser_read" || tool.Description() != "read-only browser" || !tool.ReadOnly() {
		t.Fatalf("surface mismatch: %+v", tool)
	}
	if string(tool.Schema()) != `{"type":"object"}` {
		t.Fatalf("schema mismatch: %s", tool.Schema())
	}
	text, err := tool.Execute(context.Background(), nil)
	if err != nil || text != "ok" || !executed {
		t.Fatalf("execute: %q %v", text, err)
	}
	// Classifiers must survive the adapter wrap.
	if c, ok := tool.(PlanModeClassifier); !ok || !c.PlanModeSafe() {
		t.Fatal("PlanModeSafe classifier lost")
	}
	if m, ok := tool.(ReadOnlyExecutionHostMutation); !ok || !m.ReadOnlyExecutionHostMutation() {
		t.Fatal("ReadOnlyExecutionHostMutation classifier lost")
	}
}

func TestHostToolAdapterImageChannel(t *testing.T) {
	ht := HostTool{
		Name:     "browser_read",
		Schema:   json.RawMessage(`{"type":"object"}`),
		ReadOnly: true,
		ExecuteWithImages: func(ctx context.Context, args json.RawMessage) (string, []string, error) {
			return "screenshot", []string{"data:image/png;base64,abc"}, nil
		},
	}
	tool := NewHostTool(ht)
	if _, ok := tool.(ImageTool); !ok {
		t.Fatal("ImageTool capability lost")
	}
	text, images, err := tool.(ImageTool).ExecuteWithImages(context.Background(), nil)
	if err != nil || text != "screenshot" || len(images) != 1 {
		t.Fatalf("images: %q %v %v", text, images, err)
	}
}

func TestHostToolAdapterWithoutImagesFallsBack(t *testing.T) {
	ht := HostTool{
		Name:   "browser_act",
		Schema: json.RawMessage(`{"type":"object"}`),
		Execute: func(ctx context.Context, args json.RawMessage) (string, error) {
			return "clicked", nil
		},
	}
	tool := NewHostTool(ht)
	// The structural image channel falls back to text-only for tools that
	// never produce images.
	text, images, err := tool.(ImageTool).ExecuteWithImages(context.Background(), nil)
	if err != nil || text != "clicked" || images != nil {
		t.Fatalf("images fallback: %q %v %v", text, images, err)
	}
}
