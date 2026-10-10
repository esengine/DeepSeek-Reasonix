package boot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/workspaceid"
	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
	"reasonix/internal/state/trustedstate"
	"reasonix/internal/tools/builtin"
)

func TestEffectSupportingWritesWithoutChecksMayFinish(t *testing.T) {
	const scanLimit = 64
	for _, tc := range []struct {
		name       string
		code       bool
		check      bool
		file       string
		delivery   bool
		symlink    bool
		incomplete bool
		prose      string
		move       bool
		outside    bool
	}{
		{name: "markdown only"},
		{name: "move last code to prose", move: true},
		{name: "outside AdditionalDirs write", outside: true},
		{name: "reStructuredText only", prose: "notes.rst"},
		{name: "untouched text input in the workspace", file: "notes.txt"},
		{name: "untouched MDX component in the workspace", file: "notes.mdx"},
		{name: "oversized workspace does not matter", incomplete: true},
		{name: "markdown and code", code: true},
		{name: "markdown with declared check", check: true},
		{name: "embedded policy written as prose", file: "embed"},
		{name: "case sensitive Go test suffix", file: "main_TEST.go"},
		{name: "untouched build input in the workspace", file: "CMakeLists.txt"},
		{name: "uppercase prose suffix in the workspace", file: "UPPER.MD"},
		{name: "symlink alias", symlink: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfigHome(t)
			home, err := filepath.EvalSymlinks(robustTempDir(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("REASONIX_HOME", home)
			dir, err := filepath.EvalSymlinks(robustTempDir(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir)
			writeUserConfig(t, userModel+"\n[codegraph]\nenabled = false\n")
			registerBootTokenProfileTestProvider()
			writeFile(t, dir, "todo.md", "A neutral task.\n")
			if tc.move {
				writeFile(t, dir, "main.go", "package fixture\n")
			}
			var extra string
			if tc.outside {
				extra = robustTempDir(t)
			}
			if tc.incomplete {
				for i := range scanLimit + 1 {
					writeFile(t, dir, fmt.Sprintf("note-%05d.md", i), "")
				}
			}
			if tc.file == "embed" {
				writeFile(t, dir, "go.mod", "module fixture\n\ngo 1.26\n")
				writeFile(t, dir, "main.go", "package fixture\nimport _ \"embed\"\n//go:embed policy.md\nvar policy string\n")
				writeFile(t, dir, "policy.md", "A neutral policy.\n")
			} else if tc.file != "" {
				writeFile(t, dir, tc.file, "package fixture\n")
			}
			if tc.symlink {
				writeFile(t, dir, "main.go", "package fixture\n")
				if err := os.Symlink(filepath.Join(dir, "main.go"), filepath.Join(dir, "notes.md")); err != nil {
					t.Skipf("symlink creation unavailable (Windows requires privilege or Developer Mode): %v", err)
				}
			}
			if tc.check {
				writeFile(t, dir, "AGENTS.md", "## Reasonix host checks\n\n- verify: go test ./...\n")
			}
			approveWorkspace(t, dir)
			turns := []testutil.Turn{call("notes", "write_file", `{"path":"notes.md","content":"A neutral note.\n"}`)}
			if tc.move {
				turns = append([]testutil.Turn{call("move", "move_file", `{"source_path":"main.go","destination_path":"main.md"}`)}, turns...)
			}
			if tc.outside {
				turns = append([]testutil.Turn{call("outside", "write_file", fmt.Sprintf(`{"path":%q,"content":"package fixture\n"}`, filepath.Join(extra, "plugin.go")))}, turns...)
			}
			if tc.prose != "" {
				turns = []testutil.Turn{call("notes", "write_file", fmt.Sprintf(`{"path":%q,"content":"A neutral note.\n"}`, tc.prose))}
			}
			if tc.file == "embed" {
				turns = []testutil.Turn{call("notes", "write_file", `{"path":"policy.md","content":"An updated neutral policy.\n"}`)}
			}
			if tc.file == "main_TEST.go" {
				turns = []testutil.Turn{call("notes", "write_file", `{"path":"main_TEST.go","content":"package fixture\nvar Updated = true\n"}`)}
			}
			if tc.delivery {
				turns = append([]testutil.Turn{call("todo", "todo_write", `{"todos":[{"content":"Write a neutral note","status":"in_progress"}]}`)}, turns...)
			}
			if tc.code {
				turns = append(turns, call("code", "write_file", `{"path":"main.go","content":"package main\n"}`))
			}
			turns = append(turns, testutil.Turn{Text: "done"})
			prov := testutil.NewMock("supporting", turns...)
			setBootTokenProfileTestProvider(t, prov)
			preset := AgentPresetBalanced
			if tc.delivery {
				preset = AgentPresetDelivery
			}
			sink := &bundleAuditSink{}
			opts := Options{Home: home, WorkspaceRoot: dir, AgentPreset: preset, Sink: sink, HeadlessApprovalMode: control.ToolApprovalAuto, WorkspaceScanLimit: scanLimit}
			if tc.outside {
				opts.AdditionalDirs = []string{extra}
			}
			ctrl, err := Build(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer ctrl.Close()
			err = ctrl.Run(context.Background(), "write the requested files")
			wantDebt := tc.code || tc.check || tc.file == "main_TEST.go" || tc.symlink || tc.delivery || tc.move || tc.outside
			var unready *agent.FinalReadinessError
			if wantDebt {
				if !errors.As(err, &unready) {
					t.Errorf("Run = %v, want readiness error", err)
				} else if !tc.delivery && !slices.Contains(unready.Missing, "verification") {
					t.Errorf("missing = %v, want verification", unready.Missing)
				} else if tc.check && !slices.Contains(unready.Missing, "project_check") {
					t.Errorf("missing = %v, want declared project check", unready.Missing)
				}
			} else if err != nil {
				t.Errorf("Run = %v, want completion", err)
			}
			id := "notes"
			if tc.code {
				id = "code"
			}
			result := toolResults(prov.Requests())[id]
			if debt := strings.Contains(result, "stale_verification"); debt != wantDebt {
				t.Errorf("stale_verification = %v, want %v; result: %s", debt, wantDebt, result)
			}
			target := "notes.md"
			if tc.prose != "" {
				target = tc.prose
			}
			if tc.file == "embed" {
				target = "policy.md"
			}
			if tc.file == "main_TEST.go" {
				target = "main_TEST.go"
			}
			if _, err := os.Stat(filepath.Join(dir, target)); err != nil {
				t.Fatal(err)
			}
			if tc.move {
				if _, err := os.Stat(filepath.Join(dir, "main.go")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("move source remains: %v", err)
				}
				if _, err := os.Stat(filepath.Join(dir, "main.md")); err != nil {
					t.Fatal(err)
				}
			}
			if tc.outside {
				if _, err := os.Stat(filepath.Join(extra, "plugin.go")); err != nil {
					t.Fatal(err)
				}
			}
			audits := sink.audits()
			if len(audits) == 0 || !audits[len(audits)-1].Sealed {
				t.Fatal("missing sealed evidence")
			}
			store := trustedstate.Open(filepath.Join(config.MemoryUserDir(), builtin.TrustedStateDir), nil)
			stream := workspaceid.PathFingerprint(dir)
			if _, err := store.Verify(stream); err != nil {
				t.Fatal(err)
			}
			record, err := store.Record(trustedstate.Digest(audits[len(audits)-1].Record))
			if err != nil {
				t.Fatal(err)
			}
			payload, err := store.Object(record.Payload)
			if err != nil {
				t.Fatal(err)
			}
			var bundle struct {
				Verdict struct {
					Obligations trustedstate.Digest `json:"obligations"`
				} `json:"verdict"`
			}
			if err := json.Unmarshal(payload, &bundle); err != nil {
				t.Fatal(err)
			}
			var obligations []struct {
				Source string `json:"source"`
			}
			if bundle.Verdict.Obligations != "" {
				body, err := store.Object(bundle.Verdict.Obligations)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(body, &obligations); err != nil {
					t.Fatal(err)
				}
			}
			debt := false
			for _, o := range obligations {
				debt = debt || o.Source == "host:stale_verification"
			}
			if debt != wantDebt {
				t.Errorf("sealed stale_verification = %v, want %v; bundle: %s", debt, wantDebt, payload)
			}
		})
	}
}
