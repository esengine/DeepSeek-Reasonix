package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/sandbox"
	"reasonix/internal/sessiontemp"
)

func TestBashSharesSessionTempAcrossCalls(t *testing.T) {
	type shellCase struct {
		name  string
		shell sandbox.Shell
	}
	shells := []shellCase{
		{name: "default"},
	}
	if runtime.GOOS == "windows" {
		// The default on Windows prefers Git Bash when installed. Exercise native
		// PowerShell explicitly as well so both supported shell modes prove the
		// same session-temp contract.
		powerShell := sandbox.ResolveShell("powershell", "", nil)
		if powerShell.Kind != sandbox.ShellPowerShell {
			t.Fatal("PowerShell is required for the Windows session-temp regression")
		}
		shells = append(shells, shellCase{name: "powershell", shell: powerShell})
	}

	for _, tc := range shells {
		t.Run(tc.name, func(t *testing.T) {
			m := sessiontemp.NewWithRoot(t.TempDir())
			m.Retain()
			defer m.Release()

			b := bash{
				sb:          sandbox.Spec{Mode: "off"},
				shell:       tc.shell,
				workDir:     t.TempDir(),
				sessionTemp: m,
			}
			// Pin the lazily resolved default so command syntax follows the shell
			// actually selected, rather than assuming every Windows host uses
			// PowerShell (Git Bash is preferred when present).
			b.shell = b.resolved()

			marker := "reasonix-session-temp-share"
			writeCmd := `test "$TMPDIR" = "$TMP" && test "$TMPDIR" = "$TEMP" && printf '%s' shared > "${TMPDIR:?}/` + marker + `"`
			readCmd := `cat "${TMPDIR:?}/` + marker + `"`
			if b.shell.Kind == sandbox.ShellPowerShell {
				writeCmd = `if (($env:TMPDIR -ne $env:TMP) -or ($env:TMPDIR -ne $env:TEMP)) { throw 'temporary environment variables differ' }; Set-Content -Path (Join-Path $env:TEMP '` + marker + `') -Value 'shared' -NoNewline`
				readCmd = `Get-Content -Raw (Join-Path $env:TEMP '` + marker + `')`
			}
			if _, err := b.Execute(context.Background(), argsJSON(t, map[string]any{"command": writeCmd})); err != nil {
				t.Fatalf("write: %v", err)
			}

			out, err := b.Execute(context.Background(), argsJSON(t, map[string]any{"command": readCmd}))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if !strings.Contains(out, "shared") {
				t.Fatalf("second bash call did not see first temp file: %q", out)
			}

			dir := m.Dir()
			if dir == "" {
				t.Fatal("manager has no generation after use")
			}
			body, err := os.ReadFile(filepath.Join(dir, marker))
			if err != nil || string(body) != "shared" {
				t.Fatalf("host private dir content = %q err=%v", body, err)
			}
		})
	}
}

func TestBashSchemaUnchangedWithSessionTemp(t *testing.T) {
	plain := bash{}.Schema()
	withTemp := bash{sessionTemp: sessiontemp.New()}.Schema()
	if string(plain) != string(withTemp) {
		t.Fatalf("session temp must not change bash schema\nplain=%s\nwith=%s", plain, withTemp)
	}
	var schema map[string]any
	if err := json.Unmarshal(plain, &schema); err != nil {
		t.Fatal(err)
	}
	req, _ := schema["required"].([]any)
	if len(req) != 1 || req[0] != "command" {
		t.Fatalf("required = %v", req)
	}
}

func TestBashFailsWhenSessionTempUnavailable(t *testing.T) {
	// Create-failure path: manager is owned but cannot create the directory.
	m := sessiontemp.NewWithRoot(t.TempDir())
	m.Retain()
	defer m.Release()
	m.SetMkDirForTest(func(string) (string, error) {
		return "", os.ErrPermission
	})
	b := bash{
		sb:          sandbox.Spec{Mode: "off"},
		workDir:     t.TempDir(),
		sessionTemp: m,
	}
	_, err := b.Execute(context.Background(), argsJSON(t, map[string]any{"command": "true"}))
	if err == nil {
		t.Fatal("want command failure when session temp cannot be created")
	}
	if !errors.Is(err, sessiontemp.ErrUnavailable) && !strings.Contains(err.Error(), "session temporary") {
		t.Fatalf("error = %v, want session temporary failure", err)
	}

	// Sealed manager (last owner released) must fail closed, not fall back.
	sealed := sessiontemp.NewWithRoot(t.TempDir())
	sealed.Retain()
	sealed.Release()
	b2 := bash{
		sb:          sandbox.Spec{Mode: "off"},
		workDir:     t.TempDir(),
		sessionTemp: sealed,
	}
	_, err = b2.Execute(context.Background(), argsJSON(t, map[string]any{"command": "true"}))
	if err == nil {
		t.Fatal("want failure against sealed session temp manager")
	}
}

func TestBashBackgroundLeaseSurvivesRotate(t *testing.T) {
	m := sessiontemp.NewWithRoot(t.TempDir())
	m.Retain()
	defer m.Release()

	// Pin the old generation with a lease (simulates a running background job).
	oldLease, err := m.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	oldDir := oldLease.Dir()
	if err := os.WriteFile(filepath.Join(oldDir, "bg.txt"), []byte("still-here"), 0o600); err != nil {
		t.Fatal(err)
	}

	m.Rotate()
	fresh, err := m.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Dir() == oldDir {
		t.Fatal("rotate should yield a new generation for new commands")
	}
	// Background job's generation remains until its lease is released.
	if _, err := os.Stat(filepath.Join(oldDir, "bg.txt")); err != nil {
		t.Fatalf("old generation deleted while background lease held: %v", err)
	}
	oldLease.Release()
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old generation should delete after background lease release: %v", err)
	}
	fresh.Release()
}
