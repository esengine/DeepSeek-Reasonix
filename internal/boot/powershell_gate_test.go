package boot

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/netclient"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

// goldenDefaultToolList is the exact default per-run tool list (registry
// order) a config without [tools.powershell] must produce — the system-prompt
// prefix is built from it, so any drift here cold-starts the provider's
// prefix cache. The opt-in powershell tool is registered for lookup only and
// must stay out of this list unless its config gate is open.
var goldenDefaultToolList = []string{
	"bash", "bash_output", "code_index", "complete_step", "delete_range",
	"delete_symbol", "edit_file", "glob", "grep", "kill_shell", "ls",
	"move_file", "multi_edit", "notebook_edit", "read_file", "todo_write",
	"wait", "web_fetch", "write_file",
}

func addBuiltinsForTest(enabled []string, workDir string, psEnabled bool) (*tool.Registry, *bytes.Buffer) {
	reg := tool.NewRegistry()
	var stderr bytes.Buffer
	addBuiltins(reg, enabled, nil, sandbox.Spec{}, 120*time.Second, builtin.SearchSpec{}, &stderr, workDir,
		netclient.ProxySpec{}, nil, nil, builtin.SessionDataGuard{}, builtin.ManagedConfigPaths{}, nil, nil,
		sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: `C:\ps\pwsh.exe`, MajorVersion: 7}, psEnabled)
	return reg, &stderr
}

func TestAddBuiltinsPowerShellGate(t *testing.T) {
	cases := []struct {
		name      string
		enabled   []string
		workDir   string
		psEnabled bool
		wantTool  bool
	}{
		{"gate off, empty enabled", nil, "", false, false},
		{"gate off, explicitly named", []string{"powershell", "bash"}, "", false, false},
		{"gate on, empty enabled", nil, "", true, true},
		{"gate on, allow-list without it", []string{"bash"}, "", true, false},
		{"gate on, allow-list with it", []string{"powershell"}, "", true, true},
		{"workspace, gate off", nil, `C:\ws`, false, false},
		{"workspace, gate on", nil, `C:\ws`, true, true},
		{"workspace, gate on, allow-list without it", []string{"bash"}, `C:\ws`, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg, stderr := addBuiltinsForTest(tc.enabled, tc.workDir, tc.psEnabled)
			_, ok := reg.Get("powershell")
			if ok != tc.wantTool {
				t.Errorf("powershell in registry = %v, want %v (names: %v)", ok, tc.wantTool, reg.Names())
			}
			// The zero-value powershell is registered for lookup, so naming it
			// must never produce an "unknown built-in" warning.
			if strings.Contains(stderr.String(), "unknown built-in") {
				t.Errorf("unexpected warning: %q", stderr.String())
			}
			// bash stays registered in every configuration.
			if _, ok := reg.Get("bash"); !ok && builtinToolEnabled(tc.enabled, "bash") {
				t.Errorf("bash missing from registry: %v", reg.Names())
			}
		})
	}
}

func TestDefaultToolListPrefixStability(t *testing.T) {
	// Gate off: the tool list is byte-identical to the pre-powershell default.
	reg, _ := addBuiltinsForTest(nil, "", false)
	got := reg.Names()
	if len(got) != len(goldenDefaultToolList) {
		t.Fatalf("default tool count = %d, want %d: %v", len(got), len(goldenDefaultToolList), got)
	}
	for i, name := range goldenDefaultToolList {
		if got[i] != name {
			t.Fatalf("default tool list drifted at index %d: got %v, want %v", i, got, goldenDefaultToolList)
		}
	}

	// Gate on: the list gains exactly "powershell" in sorted-registry position
	// (between notebook_edit and read_file); everything else is unchanged.
	regOn, _ := addBuiltinsForTest(nil, "", true)
	gotOn := regOn.Names()
	wantOn := make([]string, 0, len(goldenDefaultToolList)+1)
	for _, name := range goldenDefaultToolList {
		if name == "read_file" {
			wantOn = append(wantOn, "powershell")
		}
		wantOn = append(wantOn, name)
	}
	if len(gotOn) != len(wantOn) {
		t.Fatalf("gated tool count = %d, want %d: %v", len(gotOn), len(wantOn), gotOn)
	}
	for i, name := range wantOn {
		if gotOn[i] != name {
			t.Fatalf("gated tool list drifted at index %d: got %v, want %v", i, gotOn, wantOn)
		}
	}
}

func TestResolvePowerShellToolGateAndWarning(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }
	pwsh := sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: `C:\ps\pwsh.exe`, MajorVersion: 7}

	t.Run("gate off: resolver untouched, silent", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Tools.PowerShell.Enabled = boolPtr(false)
		var stderr bytes.Buffer
		called := false
		sh, enabled := resolvePowerShellTool(cfg, &stderr, func(string, string, io.Writer) sandbox.Shell {
			called = true
			return pwsh
		})
		if called {
			t.Error("resolver must not run when the tool is disabled")
		}
		if enabled || sh.Path != "" {
			t.Errorf("gate off: got (%+v, %v), want zero shell and false", sh, enabled)
		}
		if stderr.Len() != 0 {
			t.Errorf("gate off must be silent, got %q", stderr.String())
		}
	})

	t.Run("gate on, interpreter found: no warning", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Tools.PowerShell.Enabled = boolPtr(true)
		var stderr bytes.Buffer
		sh, enabled := resolvePowerShellTool(cfg, &stderr, func(prefer, path string, warn io.Writer) sandbox.Shell {
			// The configured prefer passes through verbatim; an empty default
			// means pwsh-first to the resolver.
			if prefer != "" {
				t.Errorf("prefer = %q, want the empty default", prefer)
			}
			return pwsh
		})
		if !enabled || sh.Path != pwsh.Path {
			t.Errorf("gate on: got (%+v, %v), want the resolved shell and true", sh, enabled)
		}
		if strings.Contains(stderr.String(), "no PowerShell interpreter") {
			t.Errorf("found interpreter should not warn: %q", stderr.String())
		}
	})

	t.Run("gate on, resolution fails: startup warning", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Tools.PowerShell.Enabled = boolPtr(true)
		var stderr bytes.Buffer
		sh, enabled := resolvePowerShellTool(cfg, &stderr, func(string, string, io.Writer) sandbox.Shell {
			return sandbox.Shell{}
		})
		if !enabled {
			t.Error("gate on should report enabled even when resolution fails")
		}
		if sh.Path != "" {
			t.Errorf("failed resolution should return the zero shell, got %+v", sh)
		}
		if !strings.Contains(stderr.String(), "no PowerShell interpreter was found") {
			t.Errorf("missing startup warning, got %q", stderr.String())
		}
	})
}
