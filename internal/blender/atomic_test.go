package blender

import (
	"context"
	"strings"
	"testing"
	"time"
)


// TestAtomicAddCubeLive drives a real Blender headless run: add_cube then
// Summary must report one mesh.
func TestAtomicAddCubeLive(t *testing.T) {
	requireBlender(t)
	if _, err := RunAtomic(context.Background(), "", "add_cube", map[string]any{"size": 2.0}, false, 90*time.Second); err != nil {
		t.Fatalf("add_cube: %v", err)
	}
	s, err := Summary(context.Background(), "", 60*time.Second)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if s.Objects < 1 || s.Meshes < 1 {
		t.Errorf("expected ≥1 mesh after add_cube, got objects=%d meshes=%d", s.Objects, s.Meshes)
	}
}

// TestAtomicRegistryKnownOps: the registry is non-empty and every op's script
// is valid Python (rough: contains import bpy).
func TestAtomicRegistryKnownOps(t *testing.T) {
	if len(AtomicOps) < 5 {
		t.Fatalf("expected ≥5 atomic ops, got %d", len(AtomicOps))
	}
	for _, o := range AtomicOps {
		if o.Script == nil || !strings.Contains(o.Script(map[string]any{}), "bpy") {
			t.Errorf("op %q script must reference bpy", o.Name)
		}
	}
	if _, ok := FindAtomicOp("add_cube"); !ok {
		t.Error("add_cube not found")
	}
	if _, ok := FindAtomicOp("nope"); ok {
		t.Error("unknown op must not be found")
	}
}
