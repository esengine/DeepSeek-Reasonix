package capdiag_test

import (
	"path/filepath"
	"testing"

	"reasonix/internal/capdiag"
)

func permissionReport(t *testing.T, toml string) capdiag.Report {
	t.Helper()
	root := t.TempDir()
	home := t.TempDir()
	write(t, filepath.Join(root, "reasonix.toml"), toml)
	return capdiag.Collect(capdiag.Options{
		Root:            root,
		HomeDir:         home,
		ReasonixHomeDir: filepath.Join(home, ".reasonix"),
		Live:            false,
	})
}

func TestCollectPermissionsEffectiveDecisions(t *testing.T) {
	r := permissionReport(t, `
[permissions]
mode = "ask"
allow = ["Edit", "Bash(go test:*)"]
deny = ["Bash(rm -rf /)"]
`)

	p := r.Permissions
	if p.Mode != "ask" {
		t.Fatalf("mode = %q", p.Mode)
	}
	if len(p.Allow) != 2 || len(p.Ask) != 0 || len(p.Deny) != 1 {
		t.Fatalf("rule lists = allow:%v ask:%v deny:%v", p.Allow, p.Ask, p.Deny)
	}
	for _, info := range append(append(append([]capdiag.PermissionRuleInfo{}, p.Allow...), p.Ask...), p.Deny...) {
		if info.Status != "ok" {
			t.Errorf("rule %q status = %q (%s)", info.Rule, info.Status, info.Message)
		}
	}

	decision := map[string]capdiag.PermissionToolInfo{}
	for _, ti := range p.Tools {
		decision[ti.Tool] = ti
	}
	// A bare Edit allow rule decides every file-mutation tool.
	for _, tool := range []string{"edit_file", "write_file", "move_file"} {
		info, ok := decision[tool]
		if !ok {
			t.Fatalf("missing tool row for %s", tool)
		}
		if info.Decision != "allow" || info.Matched != "Edit" || info.Scope != "rule" {
			t.Errorf("%s effective = %+v, want allow (rule Edit)", tool, info)
		}
	}
	// Bash falls back to the writer mode for a bare call; the deny rule only
	// matches its subject.
	info, ok := decision["bash"]
	if !ok {
		t.Fatal("missing tool row for bash")
	}
	if info.Decision != "ask" || info.Scope != "fallback" {
		t.Errorf("bash effective = %+v, want ask fallback", info)
	}
	// Read-only tools always fall back to allow.
	info, ok = decision["read_file"]
	if !ok {
		t.Fatal("missing tool row for read_file")
	}
	if info.Decision != "allow" || info.Scope != "fallback" {
		t.Errorf("read_file effective = %+v, want allow fallback", info)
	}
}

func TestCollectPermissionsNormalizesMode(t *testing.T) {
	r := permissionReport(t, `
[permissions]
mode = "ALLOW"
`)
	if got := r.Permissions.Mode; got != "allow" {
		t.Errorf("mode = %q, want normalized allow", got)
	}
	// An invalid mode string is silently treated as ask by the engine; the
	// report echoes the effective mode so it cannot contradict the decisions.
	r = permissionReport(t, `
[permissions]
mode = "weird"
`)
	if got := r.Permissions.Mode; got != "ask" {
		t.Errorf("mode = %q, want ask fallback", got)
	}
}

func TestCollectPermissionsValidationIssues(t *testing.T) {
	r := permissionReport(t, `
[permissions]
mode = "ask"
allow = ["edit_fil(src/**)"]
ask = ["Bash(git push:*)"]
deny = ["Edit()", "Edit(/etc/passwd)"]
`)

	severity := map[string]string{}
	for _, is := range r.Issues {
		if is.Subsystem == "permissions" {
			severity[is.Code] = is.Severity
		}
	}
	for code, want := range map[string]string{
		"permission.unknown_tool":      "warning",
		"permission.empty_specifier":   "error",
		"permission.outside_workspace": "warning",
	} {
		if severity[code] != want {
			t.Errorf("issue %s severity = %q, want %q (got %v)", code, severity[code], want, severity)
		}
	}

	p := r.Permissions
	status := map[string]string{
		p.Allow[0].Rule: p.Allow[0].Status,
		p.Ask[0].Rule:   p.Ask[0].Status,
		p.Deny[0].Rule:  p.Deny[0].Status,
		p.Deny[1].Rule:  p.Deny[1].Status,
	}
	for rule, want := range map[string]string{
		"edit_fil(src/**)":  "unknown_tool",
		"Bash(git push:*)":  "ok",
		"Edit()":            "empty_specifier",
		"Edit(/etc/passwd)": "outside_workspace",
	} {
		if status[rule] != want {
			t.Errorf("rule %q status = %q, want %q", rule, status[rule], want)
		}
	}

	// The empty-specifier deny rule collapses to a bare deny for file tools, so
	// the report shows edit_file denied by the normalized rule rather than the
	// fallback.
	for _, ti := range p.Tools {
		if ti.Tool == "edit_file" && (ti.Decision != "deny" || ti.Matched != "Edit") {
			t.Errorf("edit_file effective = %+v, want deny (rule Edit)", ti)
		}
	}
}
