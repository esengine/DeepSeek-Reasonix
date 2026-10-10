package boot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

func TestEffectPreToolAccessAllowsWriteOnlyTargets(t *testing.T) {
	for _, name := range []string{"write_file", "move_file"} {
		t.Run(name, func(t *testing.T) {
			const content = "synthetic content\n"
			args, err := json.Marshal(map[string]string{"path": "private/new.txt", "content": content, "source_path": "source.txt", "destination_path": "private/new.txt"})
			if err != nil {
				t.Fatal(err)
			}
			run := buildPostureRun(t, "[sandbox]\nbash = \"off\"\nforbid_read = [\"private\"]\n", provider.ToolCall{ID: "write-only", Name: name, Arguments: string(args)})
			if name == "move_file" {
				if err := os.WriteFile(filepath.Join(run.dir, "source.txt"), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			run.runIn(t, control.ToolApprovalYolo)
			result, ok := run.results["write-only"]
			if !ok || !result.Executed || result.RefusalCode != "" {
				t.Fatalf("write-only target rejected: %+v", result)
			}
			if got, err := os.ReadFile(filepath.Join(run.dir, "private", "new.txt")); err != nil || string(got) != content {
				t.Fatalf("destination=%q error=%v", got, err)
			}
		})
	}
}

func TestEffectSensitiveOverwriteHasWriteSpecificRefusal(t *testing.T) {
	for _, name := range []string{".env", "server.key"} {
		t.Run(name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"path": name, "content": "replacement\n"})
			if err != nil {
				t.Fatal(err)
			}
			run := buildPostureRun(t, "[sandbox]\nbash = \"off\"\n[secrets]\nprotect_sensitive_files = true\n", provider.ToolCall{ID: "overwrite", Name: "write_file", Arguments: string(args)})
			path := filepath.Join(run.dir, name)
			const original = "SENSITIVE_CANARY_DO_NOT_DISCLOSE\n"
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			run.runIn(t, control.ToolApprovalYolo)
			result := run.results["overwrite"]
			if result.Executed || result.RefusalCode != "workspace.overwrite_read_forbidden" || result.Output != "Cannot overwrite target file: permission to read existing contents is required." {
				t.Fatalf("wrong overwrite refusal: %+v", result)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != original {
				t.Fatalf("sensitive file changed: %q error=%v", got, err)
			}
		})
	}
}
