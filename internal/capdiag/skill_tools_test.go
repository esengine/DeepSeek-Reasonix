package capdiag_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/capdiag"
	"reasonix/internal/config"
	"reasonix/internal/doctor"
)

func TestSkillDiagnosticsProbeHelper(t *testing.T) {
	if marker := os.Getenv("REASONIX_SKILL_DIAGNOSTIC_PROBE"); marker != "" {
		if err := os.WriteFile(marker, []byte("started"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDoctorSkillReferenceParity(t *testing.T) {
	for _, withMCP := range []bool{false, true} {
		t.Run(fmt.Sprintf("mcp=%v", withMCP), func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			rh := filepath.Join(home, ".reasonix")
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("REASONIX_HOME", rh)
			t.Chdir(root)
			custom, excluded := filepath.Join(root, "custom"), filepath.Join(root, "excluded")
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()
			cfgText := fmt.Sprintf(`
default_model = "diagnostic-test"
[[providers]]
name = "diagnostic-test"
kind = "openai"
model = "offline"
base_url = %q
api_key = "test-placeholder"
[skills]
paths = [%q, %q]
excluded_paths = [%q]
disabled_skills = ["disabled-example"]
`, server.URL, custom, excluded, excluded)
			marker := filepath.Join(root, "mcp-started")
			t.Setenv("REASONIX_SKILL_DIAGNOSTIC_PROBE", marker)
			if withMCP {
				exe, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				cfgText += fmt.Sprintf("\n[[plugins]]\nname = \"probe\"\ntype = \"stdio\"\ncommand = %q\nargs = [\"-test.run=^TestSkillDiagnosticsProbeHelper$\"]\nauto_start = true\n", exe)
			}
			write(t, filepath.Join(root, "reasonix.toml"), cfgText)
			collect := func() ([]string, capdiag.Report) {
				t.Helper()
				cfg, err := config.LoadForRootReadOnly(root)
				if err != nil {
					t.Fatal(err)
				}
				ordinary := doctor.Collect(doctor.Options{Config: cfg})
				return ordinary.Warnings, capdiag.Collect(capdiag.Options{Root: root, HomeDir: home, ReasonixHomeDir: rh})
			}
			warnings, report := collect()
			for _, w := range warnings {
				if strings.Contains(w, "allowed-tools") {
					t.Fatal(w)
				}
			}
			if report.Skills.Winners < 2 {
				t.Fatal("built-in skills were not loaded")
			}
			for _, d := range report.Issues {
				if strings.HasPrefix(d.Code, "skill.tool_reference_") {
					t.Fatal(d)
				}
			}
			writeSkill := func(base, name, ref string) {
				t.Helper()
				write(t, filepath.Join(base, name, "SKILL.md"), fmt.Sprintf("---\nname: %s\ndescription: Test skill\nallowed-tools: [%q]\n---\nTest.\n", name, ref))
			}
			writeSkill(custom, "custom-example", "typo_read_file")
			writeSkill(custom, "dynamic-example", "mcp__future__search")
			writeSkill(custom, "disabled-example", "disabled_typo")
			writeSkill(excluded, "excluded-example", "excluded_typo")
			writeSkill(filepath.Join(rh, "skills"), "shadow-example", "shadowed_typo")
			writeSkill(filepath.Join(root, ".reasonix", "skills"), "shadow-example", "use_capability")
			warnings, report = collect()
			joined := strings.Join(warnings, "\n")
			matched := 0
			for _, d := range report.Issues {
				if !strings.HasPrefix(d.Code, "skill.tool_reference_") {
					continue
				}
				matched++
				if !strings.Contains(joined, d.Message) {
					t.Fatalf("doctor missing capability finding: %+v\n%s", d, joined)
				}
			}
			if matched != 2 {
				t.Fatalf("got %d reference findings, want unknown and unverified: %+v", matched, report.Issues)
			}
			for _, bad := range []string{"disabled_typo", "excluded_typo", "shadowed_typo"} {
				if strings.Contains(joined, bad) {
					t.Fatalf("inactive skill warned: %s", joined)
				}
			}
			if requests.Load() != 0 {
				t.Fatal("diagnostics called the provider")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("MCP process started: %v", err)
			}
		})
	}
}
