package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"reasonix/internal/sandbox"
)

// --- dry-run tests: pure construction/decision logic, run on any host ---

func pwsh7Shell() sandbox.Shell {
	return sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: `C:\Program Files\PowerShell\7\pwsh.exe`, MajorVersion: 7}
}

func ps51Shell() sandbox.Shell {
	return sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: "powershell", MajorVersion: 5}
}

func TestPwshArgvPlain(t *testing.T) {
	wrapped := wrapPowerShellCommand("Write-Output hi")
	argv := pwshArgv(pwsh7Shell(), wrapped)

	wantFlags := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-NoLogo"}
	if len(argv) != 1+len(wantFlags)+2 {
		t.Fatalf("argv length = %d, want %d: %v", len(argv), 1+len(wantFlags)+2, argv)
	}
	if argv[0] != pwsh7Shell().Path {
		t.Errorf("argv[0] = %q, want the shell path", argv[0])
	}
	for i, f := range wantFlags {
		if argv[1+i] != f {
			t.Errorf("argv[%d] = %q, want %q", 1+i, argv[1+i], f)
		}
	}
	// The payload is exactly one argument, always last.
	if argv[len(argv)-2] != "-Command" || argv[len(argv)-1] != wrapped {
		t.Errorf("payload tail = %q %q..., want -Command <wrapped>", argv[len(argv)-2], truncate(argv[len(argv)-1], 30))
	}
	// The wrapper carries the UTF-8 prologue, terminating-error trap and the
	// native-exit-code passthrough.
	for _, want := range []string{"OutputEncoding", "UTF8", "try{", "catch{", "Write-Error", "exit $LASTEXITCODE"} {
		if !strings.Contains(wrapped, want) {
			t.Errorf("wrapped script missing %q: %q", want, wrapped)
		}
	}
	if !strings.Contains(wrapped, "try{Write-Output hi}") {
		t.Errorf("user command should sit inside the try block: %q", wrapped)
	}
}

func TestPwshArgvEncoded(t *testing.T) {
	wrapped := wrapPowerShellCommand("Write-Output x") + strings.Repeat("# padding", 8010)
	if len(wrapped) <= pwshCommandArgLimit {
		t.Fatalf("test setup: wrapped length %d should exceed %d", len(wrapped), pwshCommandArgLimit)
	}
	argv := pwshArgv(pwsh7Shell(), wrapped)
	if argv[len(argv)-2] != "-EncodedCommand" {
		t.Fatalf("argv payload flag = %q, want -EncodedCommand", argv[len(argv)-2])
	}
	if got := decodePowerShellCommand(t, argv[len(argv)-1]); got != wrapped {
		t.Fatalf("EncodedCommand round-trip mismatch:\n got %q\nwant %q", truncate(got, 80), truncate(wrapped, 80))
	}
}

func TestPwshArgvThreshold(t *testing.T) {
	for _, n := range []int{pwshCommandArgLimit - 1, pwshCommandArgLimit, pwshCommandArgLimit + 1} {
		wrapped := strings.Repeat("x", n)
		argv := pwshArgv(pwsh7Shell(), wrapped)
		flag := argv[len(argv)-2]
		want := "-Command"
		if n > pwshCommandArgLimit {
			want = "-EncodedCommand"
		}
		if flag != want {
			t.Errorf("len(wrapped)=%d: payload flag = %q, want %q", n, flag, want)
		}
	}
}

func decodePowerShellCommand(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("EncodedCommand payload is not valid base64: %v", err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("UTF-16LE payload has odd byte count %d", len(raw))
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	return string(utf16.Decode(units))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func TestPowerShellValidation(t *testing.T) {
	cases := []struct {
		name    string
		cmd     string
		wantErr string // substring; empty = valid
	}{
		{"empty", "", "required"},
		{"whitespace only", "   \n ", "required"},
		{"odd double quotes", `Write-Output "a`, "unbalanced"},
		{"even double quotes", `Write-Output "a"`, ""},
		{"double quote inside single quotes", `Write-Output 'a " b'`, ""},
		{"backtick-escaped quote", "Write-Output \"a `\" b\"", ""},
		{"doubled-quote escape", `Write-Output "a "" b"`, ""},
		{"doubled single-quote escape", `Write-Output 'it''s "fine"'`, ""},
		{"plain command", "Get-ChildItem", ""},
		// Heuristic limit, documented: an unbalanced SINGLE quote is not
		// rejected (apostrophes in comments are common); the interpreter's own
		// parser error covers it.
		{"odd single quotes pass", `Write-Output 'a`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePowerShellCommand(tc.cmd)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validatePowerShellCommand(%q) = %v, want nil", tc.cmd, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validatePowerShellCommand(%q) = %v, want error containing %q", tc.cmd, err, tc.wantErr)
			}
		})
	}
}

func TestPowerShellRejectsChainingOn51(t *testing.T) {
	p := powershell{shell: ps51Shell()}
	for _, cmd := range []string{"echo a && echo b", "echo a || echo b"} {
		args, _ := json.Marshal(map[string]string{"command": cmd})
		out, err := p.Execute(context.Background(), args)
		if err == nil {
			t.Errorf("%q should be rejected on PowerShell 5.1, got out=%q", cmd, out)
		} else if !strings.Contains(err.Error(), "does not parse") {
			t.Errorf("%q error should explain the chaining limitation, got %v", cmd, err)
		}
	}

	// "&&" inside a string literal is data, not chaining — the guard must not
	// fire. (On CI without PowerShell the run itself may fail; only the guard
	// verdict is asserted.)
	args, _ := json.Marshal(map[string]string{"command": `Write-Output "a && b"`})
	if _, err := p.Execute(context.Background(), args); err != nil && strings.Contains(err.Error(), "does not parse") {
		t.Errorf("quoted && must not trip the chaining guard: %v", err)
	}

	// pwsh 7 parses && — never rejected.
	p7 := powershell{shell: pwsh7Shell()}
	args, _ = json.Marshal(map[string]string{"command": "echo a && echo b"})
	if _, err := p7.Execute(context.Background(), args); err != nil && strings.Contains(err.Error(), "does not parse") {
		t.Errorf("pwsh 7 should not be blocked by the chaining guard: %v", err)
	}
}

func TestPowerShellMissingInterpreter(t *testing.T) {
	// A binding that resolved to a non-PowerShell interpreter must not run:
	// the tool errors with install/config guidance instead of silently
	// executing under a different shell than its name promises.
	p := powershell{shell: sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}}
	args, _ := json.Marshal(map[string]string{"command": "Get-Date"})
	if _, err := p.Execute(context.Background(), args); err == nil ||
		!strings.Contains(err.Error(), "install PowerShell 7") {
		t.Fatalf("missing interpreter error should advise install/config, got %v", err)
	}
}

func TestPowerShellDescriptionReflectsVersion(t *testing.T) {
	desc51 := powershell{shell: ps51Shell()}.Description()
	if !strings.Contains(desc51, "'&&' and '||' are NOT parsed") {
		t.Errorf("5.1 description should warn about unsupported chaining: %q", desc51)
	}
	if !strings.Contains(desc51, "backslash") {
		t.Errorf("5.1 description should mention backslash paths: %q", desc51)
	}

	desc7 := powershell{shell: pwsh7Shell()}.Description()
	if !strings.Contains(desc7, "'&&' and '||' are parsed") {
		t.Errorf("pwsh 7 description should allow conditional chaining: %q", desc7)
	}
	if strings.Contains(desc7, "NOT parsed") {
		t.Errorf("pwsh 7 description should not reuse the 5.1 chaining warning: %q", desc7)
	}
	if !strings.Contains(desc7, "backslash") {
		t.Errorf("pwsh 7 description should mention backslash paths: %q", desc7)
	}

	// The bash description is untouched by the new tool.
	if strings.Contains(bash{shell: sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}}.Description(), "Prefer this over bash") {
		t.Error("bash description should not carry the powershell cross-reference")
	}
}

func TestPowerShellSchema(t *testing.T) {
	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(powershell{}.Schema(), &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if schema.Type != "object" {
		t.Errorf("schema type = %q, want object", schema.Type)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "command" {
		t.Errorf("required = %v, want [command]", schema.Required)
	}
	for _, prop := range []string{"command", "run_in_background", "timeout_seconds"} {
		if _, ok := schema.Properties[prop]; !ok {
			t.Errorf("schema properties missing %q", prop)
		}
	}
}

func TestPowerShellForegroundTimeoutTightensOnly(t *testing.T) {
	p := powershell{timeout: 120 * time.Second}
	if got := p.foregroundTimeout(0); got != 120*time.Second {
		t.Errorf("no per-call cap: %v, want 120s", got)
	}
	if got := p.foregroundTimeout(30); got != 30*time.Second {
		t.Errorf("tighter per-call cap: %v, want 30s", got)
	}
	if got := p.foregroundTimeout(300); got != 120*time.Second {
		t.Errorf("looser per-call cap must not win: %v, want 120s", got)
	}
	uncapped := powershell{}
	if got := uncapped.foregroundTimeout(30); got != 30*time.Second {
		t.Errorf("per-call cap with no configured cap: %v, want 30s", got)
	}
	if got := uncapped.foregroundTimeout(0); got != 0 {
		t.Errorf("no caps at all: %v, want 0", got)
	}
}

// --- Windows-gated e2e tests: spawn a real PowerShell ---

func resolveTestPowerShell(t *testing.T) sandbox.Shell {
	t.Helper()
	sh := sandbox.ResolvePowerShell("", "", nil)
	if sh.Path == "" {
		t.Skip("no PowerShell interpreter on this host")
	}
	return sh
}

func runPowerShell(t *testing.T, command string) (string, error) {
	t.Helper()
	p := powershell{shell: resolveTestPowerShell(t)}
	args, _ := json.Marshal(map[string]string{"command": command})
	return p.Execute(context.Background(), args)
}

func TestPowerShellRunsNativeCommand(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell e2e is windows-only")
	}
	out, err := runPowerShell(t, "Write-Output reasonix-ok")
	if err != nil {
		t.Fatalf("powershell command failed: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "reasonix-ok") {
		t.Fatalf("output = %q, want it to contain reasonix-ok", out)
	}
}

func TestPowerShellPreservesNativeExitCode(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell e2e is windows-only")
	}
	_, err := runPowerShell(t, "exit 3")
	if err == nil {
		t.Fatal("non-zero exit should surface as an error")
	}
	if !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("real exit code should survive the wrapper; got %v", err)
	}
}

func TestPowerShellTerminatingErrorIsNonZero(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell e2e is windows-only")
	}
	out, err := runPowerShell(t, "throw 'boom'")
	if err == nil {
		t.Fatal("a terminating error should produce a non-zero exit")
	}
	if !strings.Contains(out, "boom") {
		t.Fatalf("the error text should reach the output, got %q", out)
	}
}

func TestPowerShellOutputIsUTF8(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell e2e is windows-only")
	}
	out, err := runPowerShell(t, "Write-Output 'AB-中文-CD'")
	if err != nil {
		t.Fatalf("command failed: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "中文") {
		t.Fatalf("non-ASCII output mojibake — got %q (want it to contain 中文)", out)
	}
}

func TestPowerShellEncodedCommandRunsLongScript(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell e2e is windows-only")
	}
	// > 8000 chars after wrapping → the -EncodedCommand path executes.
	script := "$x = '" + strings.Repeat("a", 9000) + "'; Write-Output $x.Length"
	out, err := runPowerShell(t, script)
	if err != nil {
		t.Fatalf("long EncodedCommand script failed: %v (out=%q)", err, truncate(out, 200))
	}
	if !strings.Contains(out, "9000") {
		t.Fatalf("output = %q, want it to contain 9000", truncate(out, 200))
	}
}
