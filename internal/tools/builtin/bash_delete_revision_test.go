package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/sessiontemp"
)

func TestDeleteRevisionScope(t *testing.T) {
	for _, tc := range []struct {
		source, code string
		ps           bool
	}{
		{`"$VENV/bin/python" --version`, "", false},
		{`rm "$f"`, "", false},
		{`sudo -E make`, "", false},
		{`env -i git status`, "", false},
		{`rmdir /absolute/empty`, "", false},
		{`rm -rf build && make`, "", false},
		{`cd app && rm -rf dist 2>/dev/null`, "", false},
		{`rm -rf build || true`, "", false},
		{`f=build; rm -rf build`, "shell.delete_sequence", false},
		{`((x=1)); rm -rf build`, "shell.delete_sequence", false},
		{`function x() { :; }; rm -rf build`, "shell.delete_sequence", false},
		{`[Console]::WriteLine('fixture'); Remove-Item -Recurse child`, "shell.delete_sequence", true},
		{`cd "$x"; rm -rf build`, "shell.delete_sequence", false},
		{`rm -rf "$x"`, "shell.delete_nonliteral", false},
		{`rm -rf --unknown build`, "shell.delete_option", false},
		{`rm -rf ..`, "shell.destructive_target", false},
		{`bash -c 'rm -rf ..'`, "shell.destructive_target", false},
		{`f=x; bash -c 'bash -c "rm -rf child"'`, "shell.delete_sequence", false},
		{`cd missing; rm -rf ../victim`, "shell.destructive_target", false},
		{`bash -c "$payload"`, "", false},
		{`"$exe" -rf child`, "shell.analysis_unknown", false},
		{`'dist' | Remove-Item -Force`, "", true},
		{`[IO.Directory]::Delete('..',$false)`, "", true},
		{`[IO.Directory]::Delete('child')`, "", true},
		{`(Get-Item child).Delete()`, "", true},
		{`(Get-Item child).Delete($true)`, "shell.delete_nonliteral", true},
		{`sudo -E bash -c 'rm -rf ..'`, "shell.destructive_target", false},
		{`find .. -delete`, "shell.destructive_target", false},
		{`find child -exec rm -rf / \;`, "shell.destructive_target", false},
		{`find child -exec rm -rf {} \;`, "", false},
		{`cmd /c "r^d /s /q .."`, "shell.analysis_unknown", true},
		{`Remove-Item -Recurse:$false $f`, "", true},
		{`$PSDefaultParameterValues['*:Recurse']=$true; Microsoft.PowerShell.Management\Remove-Item -Force ..`, "shell.delete_sequence", true},
		{`Set-Alias z $value; z -Recurse ..`, "shell.analysis_unknown", true},
		{`while ($true) { [IO.Directory]::Delete('child',$true) }`, "shell.delete_sequence", true},
		{`env -C / rm -rf child`, "shell.delete_nonliteral", false},
		{`[IO.Directory]::Delete('child',$true); Remove-Item -Recurse ..`, "shell.destructive_target", true},
		{`xargs rm -rf`, "shell.delete_nonliteral", false},
		{`& "$env:ProgramFiles\Git\cmd\git.exe" status`, "", true},
		{`& $exe --version`, "", true},
		{`Remove-Item $f`, "", true},
		{`Remove-Item -Recurse -Force dist -ErrorAction SilentlyContinue`, "", true},
		{`if (Test-Path dist) { Remove-Item -Recurse -Force dist -ErrorAction SilentlyContinue } 2>$null`, "", true},
		{`Set-Location app; Remove-Item -Recurse dist`, "", true},
		{`$home='scratch'; Remove-Item -Recurse $home`, "shell.delete_sequence", true},
		{`$PSDefaultParameterValues['Remove-Item:Recurse']=$true; Remove-Item -Force ..`, "shell.delete_sequence", true},
		{`Get-ChildItem -Recurse .. | Remove-Item -Force`, "shell.delete_nonliteral", true},
		{`Set-Alias z Remove-Item; z -Recurse ..`, "shell.delete_sequence", true},
		{`iex 'Remove-Item -Recurse ..'`, "shell.destructive_target", true},
		{`iex ('Remove-Item -Recurse ..')`, "shell.destructive_target", true},
		{`iex ('Remove-Item ' + '-Recurse ..')`, "shell.destructive_target", true},
		{`powershell -Command 'Remove-Item -Recurse ..'`, "shell.destructive_target", true},
		{`powershell -Command Remove-Item -Recurse ..`, "shell.destructive_target", true},
		{`powershell -Command 'Remove-Item -Recurse' $target`, "shell.delete_nonliteral", true},
		{`[IO.Directory]::Delete('..',$true)`, "shell.destructive_target", true},
		{`cmd /c rd /s /q ..`, "shell.destructive_target", true},
	} {
		t.Run(tc.source, func(t *testing.T) {
			sh := sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}
			if tc.ps {
				sh = sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershellPath(t)}
			}
			root := t.TempDir()
			b := bash{workDir: root, sb: sandbox.Spec{WriteRoots: []string{root}}}
			err := b.refuseDestructiveDelete(t.Context(), sh, tc.source)
			var r tool.Refusal
			if tc.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.As(err, &r) || r.Code != tc.code {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
		})
	}
}

func TestDeleteRevisionInterpreterPayloads(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte{'R', 0, 'e', 0, 'm', 0, 'o', 0, 'v', 0, 'e', 0, '-', 0, 'I', 0, 't', 0, 'e', 0, 'm', 0, ' ', 0, '-', 0, 'R', 0, 'e', 0, 'c', 0, 'u', 0, 'r', 0, 's', 0, 'e', 0, ' ', 0, '.', 0, '.', 0})
	root := t.TempDir()
	ps := sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershellPath(t)}
	b := bash{workDir: root, sb: sandbox.Spec{WriteRoots: []string{root}}}
	for _, source := range []string{`powershell -EncodedCommand '` + encoded + `'`, `pwsh -Command 'Remove-Item -Recurse ..'`, `Invoke-Expression 'Remove-Item -Recurse ..'`, `sh -c 'rm -rf ..'`} {
		var r tool.Refusal
		if err := b.refuseDestructiveDelete(t.Context(), ps, source); !errors.As(err, &r) || r.Code != CodeDestructiveTarget {
			t.Fatalf("%s: %v", source, err)
		}
	}
	for _, source := range []string{`iex $payload`, `powershell -Command $payload`, `sh -c $payload`} {
		if err := b.refuseDestructiveDelete(t.Context(), ps, source); err != nil {
			t.Fatalf("ordinary dynamic payload: %v", err)
		}
	}
	for _, source := range []string{`Remove-Item -Recurse child; iex $payload`, `iex $payload; Remove-Item -Recurse child`} {
		var r tool.Refusal
		if err := b.refuseDestructiveDelete(t.Context(), ps, source); !errors.As(err, &r) {
			t.Fatalf("unresolved payload with delete allowed: %s", source)
		}
	}
}

func TestDeleteRevisionParserReuse(t *testing.T) {
	sh := sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershellPath(t)}
	if _, err := analyzePowerShellDelete(t.Context(), sh, `git status`); err != nil {
		t.Fatal(err)
	}
	deleteParsers.Lock()
	w := deleteParsers.workers[sh.Path]
	deleteParsers.Unlock()
	<-w.token
	pid := w.process.cmd.Process.Pid
	w.token <- struct{}{}
	start := time.Now()
	for range 5 {
		if _, err := analyzePowerShellDelete(t.Context(), sh, `Remove-Item -Recurse child`); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("five warm parser requests: %s", time.Since(start))
	<-w.token
	defer func() { w.token <- struct{}{} }()
	if w.process.cmd.Process.Pid != pid {
		t.Fatal("parser restarted between calls")
	}
}

func TestDeleteRevisionParserNeverEvaluates(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "marker")
	sh := sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershellPath(t)}
	source := `Set-Content -LiteralPath '` + marker + `' injected; function Analyze-DeleteSource { }; Remove-Item -Recurse child`
	if _, err := analyzePowerShellDelete(t.Context(), sh, source); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("source evaluated: %v", err)
	}
	a, err := analyzePowerShellDelete(t.Context(), sh, `Remove-Item -Recurse child`)
	if err != nil || len(a.Calls) != 1 || a.Calls[0].Name != "Remove-Item" {
		t.Fatalf("source contaminated parser: %+v %v", a, err)
	}
}

func TestDeleteRevisionHostAttribution(t *testing.T) {
	root := t.TempDir()
	b := bash{workDir: root, sb: sandbox.Spec{WriteRoots: []string{root}}}
	ps := sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershellPath(t)}
	for _, tc := range []struct {
		sh           sandbox.Shell
		source, code string
	}{
		{ps, `Remove-Item -Recurse '`, "shell.syntax_error"},
		{sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: filepath.Join(root, "missing.exe")}, `git status`, "shell.parser_unavailable"},
		{sandbox.Shell{Kind: sandbox.ShellBash}, `rm -rf '`, "shell.syntax_error"},
	} {
		var r tool.Refusal
		err := b.refuseDestructiveDelete(t.Context(), tc.sh, tc.source)
		if !errors.As(err, &r) || r.Code != tc.code || strings.Contains(r.Message, "exit status") {
			t.Fatalf("%s: %v", tc.code, err)
		}
	}
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	var r tool.Refusal
	if err := b.refuseDestructiveDelete(ctx, ps, `git status`); !errors.As(err, &r) || r.Code != "shell.parser_timeout" {
		t.Fatal(err)
	}
}

func TestDeleteRevisionSessionTemp(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("TEMP", os.Getenv("TMPDIR"))
	t.Setenv("TMP", os.Getenv("TMPDIR"))
	m := sessiontemp.New()
	m.Retain()
	defer m.Release()
	lease, err := m.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	root := t.TempDir()
	b := bash{workDir: root, sb: sandbox.Spec{WriteRoots: []string{root}}, sessionTemp: m}
	if !b.boundedDeleteTarget(filepath.Join(m.Dir(), "child")) {
		t.Fatal("session scratch refused")
	}
	if b.boundedDeleteTarget(m.Dir()) {
		t.Fatal("session temp root allowed")
	}
}

func TestDeleteRevisionSymlinkDotDot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "sub"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	b := bash{workDir: root, sb: sandbox.Spec{WriteRoots: []string{root}}}
	if b.boundedDeleteTarget("link/../victim") {
		t.Fatal("symlink parent escaped")
	}
}

func TestDeleteRevisionOrdinaryCommandsLaunch(t *testing.T) {
	for _, tc := range []struct {
		source string
		ps     bool
	}{{`"$VENV/bin/python" --version`, false}, {`rm "$f"`, false}, {`sudo -E make`, false}, {`& "$env:ProgramFiles\Git\cmd\git.exe" status`, true}, {`Remove-Item $f`, true}} {
		sh := sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}
		if tc.ps {
			sh = sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershellPath(t)}
		}
		terminal := &deleteGateTerminal{}
		root := t.TempDir()
		b := bash{shell: sh, workDir: root, sb: sandbox.Spec{WriteRoots: []string{root}}, terminal: terminal}
		args, _ := json.Marshal(map[string]string{"command": tc.source})
		_, err := b.ExecuteDetailed(t.Context(), args)
		if err != nil || terminal.calls != 1 {
			t.Fatalf("%s: calls=%d err=%v", tc.source, terminal.calls, err)
		}
	}
}

func TestDeleteRevisionCommandLength(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows command line ceiling")
	}
	b := bash{shell: sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershellPath(t)}, workDir: t.TempDir()}
	args, _ := json.Marshal(map[string]string{"command": "echo " + strings.Repeat("x", 40000)})
	result, err := b.ExecuteDetailed(t.Context(), args)
	var r tool.Refusal
	if !errors.As(err, &r) || r.Code != "shell.command_line_too_long" || !errors.Is(err, errCommandLineTooLong) || result.Execution.FailurePhase != tool.ShellPhasePreflight {
		t.Fatalf("%+v, %v", result.Execution, err)
	}
}
