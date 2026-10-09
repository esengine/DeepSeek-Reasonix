package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

func TestEffectDestructiveDeleteNeverExecutes(t *testing.T) {
	for _, mode := range []string{control.ToolApprovalAuto, control.ToolApprovalYolo} {
		t.Run(mode, func(t *testing.T) {
			isolateConfigHome(t)
			root := robustTempDir(t)
			outside := robustTempDir(t)
			t.Chdir(root)
			target := filepath.Join(outside, "victim")
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			writeFile(t, target, "keep.txt", "neutral fixture")
			marker := filepath.Join(root, "executed.txt")
			command := `rm -rf '` + filepath.ToSlash(target) + `'; printf executed > executed.txt`
			code := "shell.destructive_target"
			shellConfig := ""
			if runtime.GOOS == "windows" {
				shellConfig = "\n[tools.shell]\nprefer = \"powershell\"\n"
				t.Setenv("USERPROFILE", target)
				t.Setenv("HOME", target)
				command = `$home = Join-Path $env:TEMP 'cf-p6-manual'; if (Test-Path $home) { Remove-Item -Recurse -Force $home }; Set-Content executed.txt executed`
				code = "shell.delete_sequence"
			}
			writeUserConfig(t, userModel+"\n[sandbox]\nbash = \"off\"\n"+shellConfig)
			approveWorkspace(t, root)
			registerBootTokenProfileTestProvider()
			args, _ := json.Marshal(map[string]string{"command": command})
			prov := testutil.NewMock("delete-gate", call("delete", "bash", string(args)), testutil.Turn{Text: "done"})
			setBootTokenProfileTestProvider(t, prov)
			ctrl, err := Build(t.Context(), Options{WorkspaceRoot: root, Sink: event.Discard, HeadlessApprovalMode: mode})
			if err != nil {
				t.Fatal(err)
			}
			defer ctrl.Close()
			ctrl.ApplyHeadlessApprovalMode(mode)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := ctrl.Run(ctx, "run the fixture cleanup"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(target, "keep.txt")); err != nil {
				t.Fatalf("delete executed: %v", err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("following statement executed: %v", err)
			}
			result := toolResults(prov.Requests())["delete"]
			if !strings.Contains(result, "(refusal: "+code+")") {
				t.Fatalf("model did not receive refusal identity: %q", result)
			}
		})
	}
}
