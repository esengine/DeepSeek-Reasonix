package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

const rewriteScript = `
for p in ["plain.go", "bom.go", "crlf.txt", "big.txt"]:
    src = call("read_file", path=p)
    call("write_file", path=p, content=src.replace("OldName", "NewName"))
print("rewrote")
`

// A script that reads a file, edits the text and writes it back changes only
// what it edited: no line numbers, no lost BOM or CRLF, and no window cut from
// a file larger than one model-facing result.
func TestEffectRunScriptReadEditWriteKeepsTheBytes(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	big := strings.Repeat("// filler line that pads the file past one tool result\n", 1200) + "func OldName() {}\n"
	files := map[string]string{
		"plain.go": "package p\n\nfunc OldName() {}\n",
		"bom.go":   "\ufeffpackage p\n\nfunc OldName() {}\n",
		"crlf.txt": "OldName\r\nsecond\r\nno final newline",
		"big.txt":  big,
	}
	for name, body := range files {
		writeFile(t, dir, name, body)
	}
	rec := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("s1", "run_script", map[string]any{"script": rewriteScript})
		},
	}}
	kind := "boot-script-" + strings.ToLower(t.Name())
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"
code_mode = true

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspaceAt(t, home, dir)
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: &noticeRecorder{}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_ = ctrl.Run(context.Background(), "rename OldName with a script")
	ctrl.Close()
	reqs := agentRequests(rec.requests())
	if results := effectToolResults(reqs[len(reqs)-1]); len(results) != 1 || !strings.Contains(results[0], "rewrote") {
		t.Fatalf("script result = %q", results)
	}
	for name, body := range files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if want := strings.ReplaceAll(body, "OldName", "NewName"); string(got) != want {
			t.Errorf("%s:\n got %q\nwant %q", name, clip(string(got)), clip(want))
		}
	}
}

func clip(s string) string {
	if len(s) > 120 {
		return s[:60] + "…" + s[len(s)-60:]
	}
	return s
}
