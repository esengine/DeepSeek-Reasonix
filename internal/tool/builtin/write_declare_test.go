package builtin

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

func TestFileToolsDeclareParentDirectories(t *testing.T) {
	work := t.TempDir()
	w := writeFile{workDir: work}
	decl, err := w.DeclareWriteAccess(json.RawMessage(`{"path":"pkg/a.go","content":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(decl.Directories) != 1 || decl.Directories[0] != filepath.Join(work, "pkg") {
		t.Fatalf("write_file dirs = %v", decl.Directories)
	}
	m := moveFile{workDir: work}
	decl, err = m.DeclareWriteAccess(json.RawMessage(`{"source_path":"a.go","destination_path":"out/b.go"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(decl.Directories) != 2 {
		t.Fatalf("move_file should declare both parents, got %v", decl.Directories)
	}
}

func TestBashDeclareWriteAccessRequiresJustification(t *testing.T) {
	var b bash
	if _, err := b.DeclareWriteAccess(json.RawMessage(`{"command":"cp a ~/.local/bin/a","additional_write_dirs":["~/.local"]}`)); err == nil {
		t.Fatal("missing justification should fail")
	}
	decl, err := b.DeclareWriteAccess(json.RawMessage(`{"command":"cp a ~/.local/bin/a","additional_write_dirs":["~/.local"],"justification":"install tool"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(decl.Directories) != 1 || decl.Directories[0] != "~/.local" || decl.Justification != "install tool" {
		t.Fatalf("unexpected declaration %+v", decl)
	}
}

func TestBashSchemaIncludesWriteDirFields(t *testing.T) {
	schema := string(bash{}.Schema())
	for _, want := range []string{"additional_write_dirs", "justification"} {
		if !strings.Contains(schema, want) {
			t.Fatalf("schema missing %s: %s", want, schema)
		}
	}
	if string(bash{rootSet: sandbox.NewWritableRootSet(nil)}.Schema()) != schema {
		t.Fatal("live write-root set must not change bash schema")
	}
}

func TestBindWriteRootSetPreservesDeclare(t *testing.T) {
	set := sandbox.NewWritableRootSet([]string{t.TempDir()})
	tl := BindWriteRootSet(writeFile{workDir: t.TempDir()}, set)
	if _, ok := tl.(tool.WriteAccessDeclarer); !ok {
		t.Fatal("bound write_file must still declare write access")
	}
}
