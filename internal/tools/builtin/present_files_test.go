package builtin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/delivery"
)

func TestPresentFilesValidatesEveryFileBeforeReturning(t *testing.T) {
	root := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "report.md"), []byte("# Report"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := presentFiles{workDir: root}
	out, err := p.Execute(t.Context(), json.RawMessage(`{"files":[{"path":"report.md","description":"Report"}]}`))
	if err != nil || !json.Valid([]byte(out)) {
		t.Fatalf("present = %q, %v", out, err)
	}
	for _, args := range []string{
		`{"files":[]}`,
		`{"files":[{"path":"report.md"},{"path":"missing.md"}]}`,
		`{"files":[{"path":"../secret"}]}`,
		`{"files":[{"path":"."}]}`,
	} {
		if out, err := p.Execute(t.Context(), json.RawMessage(args)); err == nil || out != "" {
			t.Fatalf("invalid declaration returned %q, %v", out, err)
		}
	}
}

func TestPresentFilesHonorsReadPolicyAndGrantedRoots(t *testing.T) {
	roots := realRoots([]string{testenv.TempDir(t), testenv.TempDir(t)})
	root, extra := roots[0], roots[1]
	path := filepath.Join(extra, "report 100%.md")
	if err := os.WriteFile(path, []byte("# Report"), 0o600); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]any{"files": []map[string]string{{"path": path}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, presenter := range []presentFiles{
		{workDir: root},
		{workDir: root, roots: []string{extra}, forbidRoots: []string{extra}},
		{workDir: root, roots: []string{extra}, readRoots: []string{root}},
	} {
		if out, err := presenter.Execute(t.Context(), args); err == nil || out != "" {
			t.Fatalf("restricted declaration = %q, %v", out, err)
		}
	}
	presenter := presentFiles{workDir: root, roots: []string{extra}, readRoots: []string{extra}}
	out, err := presenter.Execute(t.Context(), args)
	if err != nil {
		t.Fatal(err)
	}
	files := delivery.Decode(delivery.ToolName, delivery.Identity, false, out)
	if len(files) != 1 || files[0].Root != extra || files[0].Path != "report 100%.md" {
		t.Fatalf("normalized files = %+v", files)
	}
}
