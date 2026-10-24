package delegation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/tools/builtin"
)

func childToolFixture(extra ...tool.Tool) *TaskTool {
	parent := tool.NewRegistry()
	parent.Add(subagentRegistryTool{name: "read_file", readOnly: true})
	return (&TaskTool{parentReg: parent}).WithChildTools(func(modelRef string) []tool.Tool {
		if modelRef == "vision/model" {
			return extra
		}
		return nil
	})
}

func TestChildModelWriterCannotEscapeExplicitPaths(t *testing.T) {
	root := t.TempDir()
	ws := builtin.Workspace{Dir: root}
	task := childToolFixture(ws.Tools("write_file")[0])
	task.workspaceRoot = root
	paths, err := writeclaim.NormalizeWritePaths(root, []string{filepath.Join(root, "allowed.txt")})
	if err != nil {
		t.Fatal(err)
	}
	spec := ProfileExecSpec{Grant: CapabilityGrant{WritePaths: paths}}
	reg, err := task.childRegistry(&spec, 1, writeclaim.NewWriteGrant(paths), "vision/model")
	if err != nil {
		t.Fatal(err)
	}
	writer, ok := reg.Get("write_file")
	if !ok {
		t.Fatal("path-capable child tool was removed")
	}
	_, err = writer.Execute(t.Context(), json.RawMessage(`{"path":"outside.txt","content":"escape"}`))
	if !errors.Is(err, writeclaim.ErrWriteFenceClosed) {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "outside.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tool added after grants bypassed the boundary: %v", err)
	}
}

func childHas(t *testing.T, task *TaskTool, spec ProfileExecSpec, modelRef, name string) bool {
	t.Helper()
	reg, err := task.childRegistry(&spec, 1, nil, modelRef)
	if err != nil {
		t.Fatalf("childRegistry: %v", err)
	}
	_, ok := reg.Get(name)
	return ok
}

// A child's tools follow the model the child runs on, not only the parent's:
// a text-only parent can delegate to a model that reads images, and that child
// has to be able to open one.
func TestChildGainsAnImageToolOnlyWhenItsOwnModelReadsImages(t *testing.T) {
	task := childToolFixture(subagentRegistryTool{name: "view_image", readOnly: true})
	spec := ProfileExecSpec{}
	if !childHas(t, task, spec, "vision/model", "view_image") {
		t.Error("a child on a model that reads images was not given view_image")
	}
	if childHas(t, task, spec, "text/model", "view_image") {
		t.Error("a child on a text-only model was given view_image")
	}
	if childHas(t, task, spec, "", "view_image") {
		t.Error("a child that inherits the parent's model was given a tool the parent does not hold")
	}
	if !childHas(t, task, spec, "text/model", "read_file") {
		t.Error("the child lost a tool it inherits")
	}
}

func TestChildToolsRespectTheProfileCeilingAndTheReadOnlyGrant(t *testing.T) {
	task := childToolFixture(subagentRegistryTool{name: "view_image", readOnly: true})
	ceiling := ProfileExecSpec{Grant: CapabilityGrant{ProfileTools: []string{"read_file"}}}
	if childHas(t, task, ceiling, "vision/model", "view_image") {
		t.Error("a tool outside the profile's ceiling was added")
	}
	readOnly := ProfileExecSpec{Grant: CapabilityGrant{ReadOnly: true}}
	if !childHas(t, task, readOnly, "vision/model", "view_image") {
		t.Error("a read-only child was not given a read-only extra tool")
	}
	writer := childToolFixture(subagentRegistryTool{name: "paint", readOnly: false})
	if childHas(t, writer, readOnly, "vision/model", "paint") {
		t.Error("a read-only child was given a tool that is not read-only")
	}
}

func TestChildToolsDoNotReplaceWhatTheParentAlreadyHolds(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentRegistryTool{name: "view_image", readOnly: true, result: "parent's"})
	task := (&TaskTool{parentReg: parent}).WithChildTools(func(string) []tool.Tool {
		return []tool.Tool{subagentRegistryTool{name: "view_image", readOnly: true, result: "extra"}}
	})
	reg, err := task.childRegistry(&ProfileExecSpec{}, 1, nil, "vision/model")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reg.Get("view_image")
	if got.(subagentRegistryTool).result != "parent's" {
		t.Error("the parent's own tool was replaced")
	}
}
