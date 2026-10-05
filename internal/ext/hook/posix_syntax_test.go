package hook

import (
	"runtime"
	"testing"
)

// The reported hook: shell syntax, no interpreter named. On Windows it reached
// the cmd.exe fallback, which has no grep and no <<<.
const reportedGuardHook = `grep -q '"\.env' <<< "$REASONIX_HOOK_PAYLOAD" && exit 2 || exit 0`

func TestRequiresPOSIXShell(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"reported guard hook", reportedGuardHook, true},
		{"herestring", `cat <<< "x"`, true},
		{"heredoc", "cat <<EOF\nx\nEOF", true},
		{"parameter expansion", `echo "$HOME"`, true},
		{"command substitution", "echo $(date)", true},
		{"single quotes", `grep 'pattern' file.txt`, true},
		{"arithmetic", "echo $((1 + 1))", true},

		{"plain command", "check-secrets.exe", false},
		{"and chain", "build.bat && test.bat", false},
		{"pipe", "type file.txt | findstr secret", false},
		{"double quoted path", `"C:\Program Files\tool\run.exe" --check`, false},
		{"redirect", "run.exe > out.txt", false},
		{"windows percent variable", "echo %USERPROFILE%", false},
		{"unparseable", `run.exe "unterminated`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requiresPOSIXShell(tt.command); got != tt.want {
				t.Fatalf("requiresPOSIXShell(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

// The spawn path and the diagnostics path must agree on which hooks need bash,
// or a hook runs one way and reports another.
func TestShellSyntaxHookRequiresWindowsBash(t *testing.T) {
	config := HookConfig{Command: reportedGuardHook, ExecutionMode: ExecutionLegacy}
	if !requiresWindowsBashForHook(config) {
		t.Fatal("a shell-syntax settings hook should require bash on Windows")
	}
	batch := HookConfig{Command: "build.bat && test.bat", ExecutionMode: ExecutionLegacy}
	if requiresWindowsBashForHook(batch) {
		t.Fatal("a batch-style hook must keep its cmd.exe interpreter")
	}
}

// A shell-syntax hook must reach a POSIX shell rather than cmd.exe, which
// cannot parse it whatever program it names.
func TestSpawnerRunsShellSyntaxHookThroughPOSIXShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := resolveWindowsHookBash(""); err != nil {
			t.Skip("no Git Bash on this host")
		}
	}
	result := DefaultSpawner(t.Context(), SpawnInput{
		// POSIX only. A here-string is a bashism, and the shell this reaches is
		// `sh` — which is dash on Debian-family runners, where the fixture died
		// on its own syntax before it could test anything.
		Command: `payload=$(cat); printf '%s' "$payload" | grep -q 'secret' && printf blocked || printf allowed`,
		Stdin:   `{"tool":"read","path":"secret.env"}`,
		Timeout: realSpawnTimeout,
	})
	if result.SpawnErr != nil {
		t.Fatalf("spawn: %v", result.SpawnErr)
	}
	if result.Stdout != "blocked" {
		t.Fatalf("stdout = %q (stderr %q), want %q", result.Stdout, result.Stderr, "blocked")
	}
}

func TestUndefinedPayloadVars(t *testing.T) {
	config := HookConfig{Command: reportedGuardHook}
	got := UndefinedPayloadVars(config)
	if len(got) != 1 || got[0] != "REASONIX_HOOK_PAYLOAD" {
		t.Fatalf("UndefinedPayloadVars = %v, want [REASONIX_HOOK_PAYLOAD]", got)
	}

	// A variable the hook is actually given is not a dangling reference.
	supplied := HookConfig{
		Command: `echo "$REASONIX_PLUGIN_ROOT"`,
		Env:     pluginHookEnv("/plugins/guard", "guard", "1.0.0", "/home/.reasonix", "/work"),
	}
	if got := UndefinedPayloadVars(supplied); len(got) != 0 {
		t.Fatalf("injected plugin env reported undefined: %v", got)
	}

	// $NAME inside single quotes is a literal, not a reference — the reason this
	// reads the shell AST instead of scanning for "$".
	quoted := HookConfig{Command: `printf '$REASONIX_HOOK_PAYLOAD'`}
	if got := UndefinedPayloadVars(quoted); len(got) != 0 {
		t.Fatalf("literal in single quotes reported as a reference: %v", got)
	}

	// Variables outside the Reasonix namespace belong to the user.
	foreign := HookConfig{Command: `echo "$SOME_OTHER_TOOL_PAYLOAD"`}
	if got := UndefinedPayloadVars(foreign); len(got) != 0 {
		t.Fatalf("foreign variable reported: %v", got)
	}
}
