package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

// Use the public assembly and pre-existing APIs so the same test runs on the
// vulnerable source, where edit mismatch errors disclose the nearest file line.
func TestEffectForbiddenReadDoesNotLeakFileContents(t *testing.T) {
	for _, name := range []string{"read_file", "edit_file", "multi_edit"} {
		t.Run(name, func(t *testing.T) {
			const marker = "review-canary-do-not-disclose"
			const original = "INTERNAL_NOTE=" + marker + "\n"
			params := map[string]any{"path": "private/note.txt"}
			switch name {
			case "edit_file":
				params["old_string"], params["new_string"] = "INTERNAL_NOTE=missing-value", "updated"
			case "multi_edit":
				params["edits"] = []map[string]string{{"old_string": "INTERNAL_NOTE=missing-value", "new_string": "updated"}}
			}
			args, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			const callID = "forbidden-read"
			run := buildPostureRun(t, "[sandbox]\nbash = \"off\"\nforbid_read = [\"private\"]\n", provider.ToolCall{ID: callID, Name: name, Arguments: string(args)})
			path := filepath.Join(run.dir, "private", "note.txt")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			run.ctrl.ApplyHeadlessApprovalMode(control.ToolApprovalYolo)
			if err := run.ctrl.Run(context.Background(), "perform the requested file operation"); err != nil {
				t.Fatal(err)
			}
			result, ok := run.results[callID]
			if !ok || result.Err == "" || result.Output == "" {
				t.Fatalf("expected a failed file operation, got present=%v result=%+v", ok, result)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != original {
				t.Fatalf("failed operation changed the protected file: %q error=%v", got, err)
			}
			if strings.Contains(result.Output, marker) || strings.Contains(result.Err, marker) {
				t.Errorf("forbidden file contents leaked in tool error: output=%q error=%q", result.Output, result.Err)
			}
			seen := run.modelSaw(t, callID)
			if strings.Contains(seen, marker) {
				t.Errorf("forbidden file contents reached the model: %q", seen)
			}
			if seen != result.Output {
				t.Errorf("model result differs from tool result: model=%q tool=%q", seen, result.Output)
			}
			for _, msg := range run.ctrl.History() {
				for _, call := range msg.ToolCalls {
					if call.ID == callID && strings.Contains(call.Diff, marker) {
						t.Errorf("forbidden file contents leaked in archived preview: %q", call.Diff)
					}
				}
			}
		})
	}
}

func TestEffectForbiddenReadDoesNotLeakPreviewOnDeniedWrite(t *testing.T) {
	for _, name := range []string{"edit_file", "multi_edit", "write_file"} {
		t.Run(name, func(t *testing.T) {
			const original = "INTERNAL_NOTE=preview-canary-do-not-disclose\n"
			params := map[string]any{"path": "private/note.txt"}
			switch name {
			case "edit_file":
				params["old_string"], params["new_string"] = "INTERNAL_NOTE=", "UPDATED_NOTE="
			case "multi_edit":
				params["edits"] = []map[string]string{{"old_string": "INTERNAL_NOTE=", "new_string": "UPDATED_NOTE="}}
			case "write_file":
				params["content"] = "replacement\n"
			}
			args, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			const callID = "forbidden-preview"
			run := buildPostureRun(t, "[sandbox]\nbash = \"off\"\nforbid_read = [\"private\"]\n", provider.ToolCall{ID: callID, Name: name, Arguments: string(args)})
			path := filepath.Join(run.dir, "private", "note.txt")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			run.runIn(t, control.ToolApprovalReadOnly)
			if result := run.results[callID]; result.Executed {
				t.Fatal("denied write executed")
			}
			for _, msg := range run.ctrl.History() {
				for _, call := range msg.ToolCalls {
					if call.ID == callID && strings.Contains(call.Diff, "preview-canary-do-not-disclose") {
						t.Errorf("denied write archived forbidden contents: %q", call.Diff)
					}
				}
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != original {
				t.Fatalf("denied write changed the file: %q error=%v", got, err)
			}
		})
	}
}
