package hook

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"reasonix/internal/sandbox"
)

func TestHookExecHelperProcess(t *testing.T) {
	if os.Getenv("REASONIX_HOOK_EXEC_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg != "--" {
			continue
		}
		if err := json.NewEncoder(os.Stdout).Encode(os.Args[i+1:]); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(3)
}

func TestExecFormPreservesLiteralArgumentsEndToEnd(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"",
		" leading and trailing ",
		"$HOME",
		"%PATH%",
		"!DELAYED!",
		`a && b | c > out`,
		`double"quote`,
		"single'quote",
		`C:\Program Files\Reasonix\hook.cmd`,
		"第一行\n第二行",
		"emoji-🧪",
	}
	args := append([]string{"-test.run=^TestHookExecHelperProcess$", "--"}, want...)
	result := DefaultSpawner(context.Background(), SpawnInput{
		Command: executable,
		Args:    args,
		Mode:    ExecutionExec,
		Env:     map[string]string{"REASONIX_HOOK_EXEC_HELPER": "1"},
		Timeout: realSpawnTimeout,
	})
	if result.ExitCode != 0 || result.SpawnErr != nil {
		t.Fatalf("exec-form helper failed: %+v", result)
	}
	var got []string
	if err := json.Unmarshal([]byte(result.Stdout), &got); err != nil {
		t.Fatalf("decode helper output %q: %v", result.Stdout, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("literal argv changed:\n got %#v\nwant %#v", got, want)
	}
}

func TestSpawnCommandExecutionContractMatrix(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	literalArgs := []string{"", "$VALUE", "a && b", `nested"quote`}
	cmd, err := spawnCommand(context.Background(), executable, ExecutionExec, "bash", literalArgs, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cmd.Args[1:], literalArgs) {
		t.Fatalf("exec argv = %#v, want %#v", cmd.Args[1:], literalArgs)
	}

	if _, err := spawnCommand(context.Background(), "ignored", ExecutionMode("future"), "", nil, RuntimeOptions{}); err == nil ||
		!strings.Contains(err.Error(), "unsupported hook execution mode") {
		t.Fatalf("unknown execution mode error = %v", err)
	}
	if _, err := spawnCommand(context.Background(), "ignored", ExecutionShell, "fish", nil, RuntimeOptions{}); err == nil ||
		!strings.Contains(err.Error(), "unsupported hook shell") {
		t.Fatalf("unknown shell error = %v", err)
	}
	if runtime.GOOS != "windows" {
		if _, err := spawnCommand(context.Background(), "echo ok", ExecutionShell, "cmd", nil, RuntimeOptions{}); err == nil ||
			!strings.Contains(err.Error(), "only available on Windows") {
			t.Fatalf("non-Windows cmd error = %v", err)
		}
	}
}

func TestShellSelectionBuildsExactInterpreterArgv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows interpreter selection has native runtime tests")
	}
	script := `printf '%s' "a && b"`
	tests := []struct {
		name      string
		preferred string
		wantPath  string
	}{
		{name: "default", preferred: "", wantPath: "sh"},
		{name: "auto", preferred: "auto", wantPath: "sh"},
		{name: "bash", preferred: "bash", wantPath: "bash"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := spawnShellCommand(context.Background(), script, tt.preferred, RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if got := cmd.Args; len(got) != 3 || got[0] != tt.wantPath || got[1] != "-c" || got[2] != script {
				t.Fatalf("shell argv = %#v, want [%q -c <exact script>]", got, tt.wantPath)
			}
		})
	}

	if _, err := exec.LookPath("pwsh"); err != nil {
		if _, err := spawnShellCommand(context.Background(), script, "pwsh", RuntimeOptions{}); err == nil ||
			!strings.Contains(err.Error(), "no usable PowerShell") {
			t.Fatalf("missing pwsh error = %v", err)
		}
	}
}

func TestRawShellCommandPreservesScriptForResolvedShells(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX executable paths for deterministic argv inspection")
	}
	script := `printf '%s' '"nested" && literal'`
	bashCmd, err := rawShellCommand(context.Background(), sandbox.Shell{Kind: sandbox.ShellBash, Path: "/bin/sh"}, script)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := bashCmd.Args, []string{"/bin/sh", "-c", script}; !reflect.DeepEqual(got, want) {
		t.Fatalf("raw Bash argv = %#v, want %#v", got, want)
	}

	powerShellScript := `$value = "a && 'b'"; Write-Output $value`
	powerShellCmd, err := rawShellCommand(context.Background(), sandbox.Shell{
		Kind: sandbox.ShellPowerShell,
		Path: "/bin/sh",
	}, powerShellScript)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodePowerShellCommandForTest(powerShellCmd.Args[4])
	if err != nil {
		t.Fatal(err)
	}
	if want := sandbox.PowerShellUTF8Script(powerShellScript); decoded != want {
		t.Fatalf("PowerShell script = %q, want %q", decoded, want)
	}
}

func TestResolvedHookShellPathAcceptsExecutableAndRejectsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX executable paths")
	}
	got, err := resolvedHookShellPath(sandbox.Shell{Kind: sandbox.ShellBash, Path: "/bin/sh"})
	if err != nil || got != "/bin/sh" {
		t.Fatalf("resolved /bin/sh = %q, %v", got, err)
	}
	if _, err := resolvedHookShellPath(sandbox.Shell{Kind: sandbox.ShellBash, Path: "/definitely/missing/reasonix-hook-shell"}); err == nil {
		t.Fatal("missing absolute shell unexpectedly resolved")
	}
}

func TestBashShellFormComplexCommandMatrix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows shell-form coverage lives in windows_batch_test.go")
	}
	tests := []struct {
		name    string
		command string
		stdin   string
		env     map[string]string
		want    string
	}{
		{
			name:    "operators inside literal quotes",
			command: `printf '%s' 'a && b | c > out'`,
			want:    `a && b | c > out`,
		},
		{
			name:    "nested quotes and variable expansion",
			command: `value='single "double"'; printf '%s:%s' "$HOOK_VALUE" "$value"`,
			env:     map[string]string{"HOOK_VALUE": "expanded"},
			want:    `expanded:single "double"`,
		},
		{
			name:    "pipeline",
			command: `printf 'left\nright\n' | tail -n 1`,
			want:    "right",
		},
		{
			name:    "subshell and chaining",
			command: `(printf one; printf two) && printf three`,
			want:    "onetwothree",
		},
		{
			name:    "command substitution",
			command: `printf '<%s>' "$(printf nested)"`,
			want:    "<nested>",
		},
		{
			name:    "stdin",
			command: `IFS= read -r value; printf '%s' "$value"`,
			stdin:   `payload "quoted" && literal`,
			want:    `payload "quoted" && literal`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DefaultSpawner(context.Background(), SpawnInput{
				Command: tt.command,
				Mode:    ExecutionShell,
				Shell:   "bash",
				Env:     tt.env,
				Stdin:   tt.stdin,
				Timeout: realSpawnTimeout,
			})
			if result.ExitCode != 0 || result.SpawnErr != nil || result.Stdout != tt.want {
				t.Fatalf("shell-form result = %+v, want stdout %q", result, tt.want)
			}
		})
	}
}

func TestShellFormHonorsExitStderrAndTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Bash")
	}
	failed := DefaultSpawner(context.Background(), SpawnInput{
		Command: `printf 'problem' >&2; exit 7`,
		Mode:    ExecutionShell,
		Shell:   "bash",
		Timeout: realSpawnTimeout,
	})
	if failed.ExitCode != 7 || failed.Stderr != "problem" || failed.SpawnErr != nil {
		t.Fatalf("shell failure result = %+v", failed)
	}

	timedOut := DefaultSpawner(context.Background(), SpawnInput{
		Command: "sleep 5",
		Mode:    ExecutionShell,
		Shell:   "bash",
		Timeout: 50 * time.Millisecond,
	})
	if !timedOut.TimedOut || timedOut.ExitCode != -1 {
		t.Fatalf("shell timeout result = %+v", timedOut)
	}
}

func decodePowerShellCommandForTest(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	if len(raw)%2 != 0 {
		return "", &oddUTF16LengthError{length: len(raw)}
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	return string(utf16.Decode(units)), nil
}

type oddUTF16LengthError struct {
	length int
}

func (e *oddUTF16LengthError) Error() string {
	return fmt.Sprintf("odd UTF-16LE byte length %d", e.length)
}

func FuzzPowerShellCommandEncodingRoundTrip(f *testing.F) {
	for _, seed := range []string{
		"",
		`Write-Output "a && 'b'"`,
		`$value = "C:\Program Files\Reasonix"; $value`,
		"第一行\n第二行",
		"Write-Output '🧪'",
		"`$literal; $(Write-Output nested)",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, script string) {
		if !utf8.ValidString(script) {
			t.Skip()
		}
		cmd := powerShellCommand(context.Background(), "powershell", script)
		got, err := decodePowerShellCommandForTest(cmd.Args[4])
		if err != nil {
			t.Fatal(err)
		}
		want := sandbox.PowerShellUTF8Script(script)
		if got != want {
			t.Fatalf("PowerShell script changed:\n got %q\nwant %q", got, want)
		}
	})
}
