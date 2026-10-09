package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
)

type deleteGateTerminal struct{ calls int }

func (r *deleteGateTerminal) RunCommand(context.Context, string, string, time.Duration, map[string]string) (string, bool, error) {
	r.calls++
	return "stub", true, nil
}

func TestBashDestructiveTargetGate(t *testing.T) {
	t.Setenv("REASONIX_FILTER_SUBPROCESS_ENV", "")
	root := t.TempDir()
	outside := t.TempDir()
	cases := []struct {
		command string
		ps      bool
		code    string
	}{
		{`rm -rf "$HOME"`, false, CodeDeleteNonliteral}, {`rm -rf ~`, false, CodeDeleteNonliteral}, {`rm -rf /`, false, CodeDestructiveTarget},
		{`rm -rf "$target"`, false, CodeDeleteNonliteral}, {`rm -rf "${target}/child"`, false, CodeDeleteNonliteral},
		{`rm --recursive '` + filepath.ToSlash(outside) + `'`, false, CodeDestructiveTarget}, {`rm -rf ..`, false, CodeDestructiveTarget},
		{`rm -rf .`, false, CodeDestructiveTarget}, {`cd ..; rm -rf child`, false, CodeDestructiveTarget},
		{`Remove-Item -Recurse -Force $HOME`, true, CodeDeleteNonliteral},
		{`Remove-Item -Recurse -Force $env:USERPROFILE`, true, CodeDeleteNonliteral},
		{`Remove-Item -Recurse -Force $target`, true, CodeDeleteNonliteral},
		{`Remove-Item -Recurse -Force ~`, true, CodeDeleteNonliteral},
		{`Remove-Item -Recurse -Force 'C:\'`, true, CodeDestructiveTarget},
		{`Remove-Item -Recurse -Force '` + outside + `'`, true, CodeDestructiveTarget},
		{`Remove-Item -Recurse -Force ..`, true, CodeDestructiveTarget},
		{`Remove-Item -Recurse -Force .`, true, CodeDestructiveTarget},
		{`rm -r -fo $target`, true, CodeDeleteNonliteral}, {`del /s %USERPROFILE%`, true, CodeDeleteNonliteral}, {`rd /s C:\`, true, CodeDestructiveTarget},
		{`$home = Join-Path $env:TEMP 'cf-p6-manual'; if (Test-Path $home) { Remove-Item -Recurse -Force $home }`, true, CodeDeleteSequence},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			sh := sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}
			if tc.ps {
				sh = sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershellPath(t)}
			}
			terminal := &deleteGateTerminal{}
			b := bash{shell: sh, workDir: root, sb: sandbox.Spec{WriteRoots: []string{root}}, terminal: terminal}
			args, _ := json.Marshal(map[string]string{"command": tc.command})
			res, err := b.ExecuteDetailed(t.Context(), args)
			var refusal tool.Refusal
			if !errors.As(err, &refusal) || refusal.Code != tc.code {
				t.Fatalf("want %s refusal, got %v", tc.code, err)
			}
			if terminal.calls != 0 || res.Execution.State != tool.ShellStateNotRun || res.Execution.MutationRisk != tool.ShellMutationNotStarted {
				t.Fatalf("command reached execution: calls=%d, execution=%+v", terminal.calls, res.Execution)
			}
		})
	}
}

func TestBashDeleteGateAllowsBoundedCleanup(t *testing.T) {
	root := t.TempDir()
	for _, ps := range []bool{false, true} {
		sh := sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}
		command := `rm -rf child`
		if ps {
			sh = sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershellPath(t)}
			command = `Remove-Item -LiteralPath child -Recurse -Force`
		}
		terminal := &deleteGateTerminal{}
		b := bash{shell: sh, workDir: root, sb: sandbox.Spec{WriteRoots: []string{root}}, terminal: terminal}
		args, _ := json.Marshal(map[string]string{"command": command})
		_, err := b.ExecuteDetailed(t.Context(), args)
		if err != nil || terminal.calls != 1 {
			t.Fatalf("bounded cleanup: calls=%d, err=%v", terminal.calls, err)
		}
	}
}

func TestBashDeleteGateRejectsAlternateScopes(t *testing.T) {
	root := t.TempDir()
	for _, command := range []string{`command rm -rf "$target"`, `env rm -rf "$target"`, `Remove-Item -Recurse /outside`, `Remove-Item -Recurse \outside`} {
		if command == `Remove-Item -Recurse \outside` && runtime.GOOS != "windows" {
			continue
		}
		sh := sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}
		if strings.HasPrefix(command, "Remove-Item") {
			sh = sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershellPath(t)}
		}
		terminal := &deleteGateTerminal{}
		b := bash{shell: sh, workDir: root, sb: sandbox.Spec{WriteRoots: []string{root}}, terminal: terminal}
		args, _ := json.Marshal(map[string]string{"command": command})
		_, err := b.ExecuteDetailed(t.Context(), args)
		var refusal tool.Refusal
		code := CodeDestructiveTarget
		if sh.Kind == sandbox.ShellBash {
			code = CodeDeleteNonliteral
		}
		if !errors.As(err, &refusal) || refusal.Code != code {
			t.Errorf("%s: expected refusal, got %v", command, err)
		}
	}
}

func TestPowerShellDeleteAnalysisPreservesUnicode(t *testing.T) {
	sh := sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershellPath(t)}
	target := "\u4e2d\u6587-fixture"
	analysis, err := analyzePowerShellDelete(t.Context(), sh, "Remove-Item -Recurse '"+target+"'")
	if err != nil || len(analysis.Calls) != 1 || len(analysis.Calls[0].Args) != 2 || analysis.Calls[0].Args[1] != target {
		t.Fatalf("unicode analysis = %+v, err=%v", analysis, err)
	}
}

func TestBashDeleteGateRejectsNestedDirectoryChange(t *testing.T) {
	terminal := &deleteGateTerminal{}
	root := t.TempDir()
	b := bash{shell: sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}, workDir: root, sb: sandbox.Spec{WriteRoots: []string{root}}, terminal: terminal}
	args, _ := json.Marshal(map[string]string{"command": `echo "$(cd ..; rm -rf child)"`})
	_, err := b.ExecuteDetailed(t.Context(), args)
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeDeleteSequence || terminal.calls != 0 {
		t.Fatalf("nested delete reached execution: err=%v calls=%d", err, terminal.calls)
	}
}

func TestBashDeleteGateExpansionShapes(t *testing.T) {
	root := t.TempDir()
	cases := map[string]string{
		`rm -rf {~,x}`: CodeDeleteNonliteral, `rm -rf "{~,x}"`: CodeDeleteNonliteral, `rm -rf {x,..}`: CodeDeleteNonliteral,
		`rm -rf a=~`: CodeDeleteNonliteral, `rm -rf a=x:~`: CodeDeleteNonliteral, `rm -rf x/~`: CodeDeleteNonliteral,
		`rm -rf a~b`: CodeDeleteNonliteral, `rm -rf "a~b"`: CodeDeleteNonliteral, `rm -rf '~'`: CodeDeleteNonliteral,
		`rm -rf 'C:/Users/RUNNER~1/AppData/Local/Temp/x'`: CodeDestructiveTarget,
		`rm -rf 'C:/Users/RUNNER~1/~x'`:                   CodeDeleteNonliteral,
	}
	for command, code := range cases {
		t.Run(command, func(t *testing.T) {
			terminal := &deleteGateTerminal{}
			b := bash{shell: sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}, workDir: root, sb: sandbox.Spec{WriteRoots: []string{root}}, terminal: terminal}
			args, _ := json.Marshal(map[string]string{"command": command})
			_, err := b.ExecuteDetailed(t.Context(), args)
			var refusal tool.Refusal
			if !errors.As(err, &refusal) || refusal.Code != code || terminal.calls != 0 {
				t.Fatalf("want %s refusal, got %v (calls=%d)", code, err, terminal.calls)
			}
		})
	}
}

func TestLiteralDeletePathTilde(t *testing.T) {
	for target, want := range map[string]bool{
		"~": false, "~/x": false, "~user/x": false, "a~b": false, "x/~": false, "a=~": false, "{a,b}": false,
		`C:\Users\RUNNER~1\Temp\x`: true, "C:/Users/RUNNER~1/x": true, "build~1/out": false, `C:\a\~b`: false,
	} {
		if got := literalDeletePath(target); got != want {
			t.Errorf("literalDeletePath(%q) = %v, want %v", target, got, want)
		}
	}
}
