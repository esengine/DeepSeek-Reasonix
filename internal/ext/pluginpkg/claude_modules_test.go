package pluginpkg

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func writeModsPackage(t *testing.T, hooksJSON string) string {
	t.Helper()
	root := testenv.TempDir(t)
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name": "mod-pack"}`)
	writeTestFile(t, filepath.Join(root, "skills", "greet", "SKILL.md"), "---\nname: greet\ndescription: Greets\n---\nSay hi.")
	writeTestFile(t, filepath.Join(root, "hooks", "hooks.json"), hooksJSON)
	return root
}

func modulesIssues(pkg Package) []CompatibilityIssue {
	var out []CompatibilityIssue
	for _, issue := range pkg.Compatibility.Skipped {
		if issue.Capability == "modules" {
			out = append(out, issue)
		}
	}
	return out
}

func TestParseClaudeModulesOnlyHooksFileIsPartial(t *testing.T) {
	root := writeModsPackage(t, `{"modules":["./register.ts","./other.js"]}`)
	pkg, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if pkg.Compatibility.Status != "partial" {
		t.Fatalf("status = %q, want partial", pkg.Compatibility.Status)
	}
	issues := modulesIssues(pkg)
	if len(issues) != 1 || issues[0].Path != "hooks/hooks.json" {
		t.Fatalf("modules issues = %+v, want one for hooks/hooks.json", issues)
	}
	if strings.Contains(issues[0].Reason, "register.ts") || len(warnings) != 1 {
		t.Fatalf("reason echoes module paths or warnings = %v", warnings)
	}
	if len(pkg.Manifest.Hooks) != 0 {
		t.Fatalf("modules must not produce hooks: %#v", pkg.Manifest.Hooks)
	}
}

func TestParseClaudeModulesKeepsClassicHooks(t *testing.T) {
	root := writeModsPackage(t, `{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo ok"}]}]},"modules":["./register.ts"]}`)
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(pkg.Manifest.Hooks["PostToolUse"]) != 1 {
		t.Fatalf("classic hook not mapped: %#v", pkg.Manifest.Hooks)
	}
	if pkg.Compatibility.Status != "partial" || len(modulesIssues(pkg)) != 1 {
		t.Fatalf("status = %q issues = %+v, want partial with one modules issue", pkg.Compatibility.Status, pkg.Compatibility.Skipped)
	}
}

func TestParseClaudeClassicHooksWithoutModulesStayFull(t *testing.T) {
	root := writeModsPackage(t, `{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo ok"}]}]}}`)
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if pkg.Compatibility.Status != "full" || len(pkg.Compatibility.Skipped) != 0 {
		t.Fatalf("status = %q skipped = %+v, want full", pkg.Compatibility.Status, pkg.Compatibility.Skipped)
	}
	if len(pkg.Manifest.Hooks["PostToolUse"]) != 1 {
		t.Fatalf("hooks = %#v", pkg.Manifest.Hooks)
	}
}

func TestParseClaudeModulesNullOrEmptyHooksFileDeclaresNothing(t *testing.T) {
	for _, body := range []string{`{}`, `{"modules":null}`, `{"modules":[]}`} {
		pkg, _, err := ParseDir(writeModsPackage(t, body))
		if err != nil {
			t.Fatalf("%s: ParseDir: %v", body, err)
		}
		if pkg.Compatibility.Status != "full" || len(pkg.Compatibility.Skipped) != 0 {
			t.Fatalf("%s: status = %q skipped = %+v, want full", body, pkg.Compatibility.Status, pkg.Compatibility.Skipped)
		}
	}
}

func TestParseClaudeModulesMalformedIsReportedWithoutEcho(t *testing.T) {
	for _, body := range []string{
		`{"modules":"./secret-name.ts"}`,
		`{"modules":{"a":"secret-name"}}`,
		`{"modules":[1,null,{"x":"secret-name"}]}`,
	} {
		pkg, warnings, err := ParseDir(writeModsPackage(t, body))
		if err != nil {
			t.Fatalf("%s: ParseDir: %v", body, err)
		}
		if pkg.Compatibility.Status != "partial" {
			t.Fatalf("%s: status = %q, want partial", body, pkg.Compatibility.Status)
		}
		issues := modulesIssues(pkg)
		if len(issues) != 1 {
			t.Fatalf("%s: modules issues = %+v", body, issues)
		}
		if strings.Contains(issues[0].Reason, "secret-name") || strings.Contains(strings.Join(warnings, "\n"), "secret-name") {
			t.Fatalf("%s: package-controlled text echoed: %q %v", body, issues[0].Reason, warnings)
		}
	}
}
