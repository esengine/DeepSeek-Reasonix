package builtin

import (
	"strings"
	"testing"

	"reasonix/internal/sandbox"
)

func TestExtractWriteTargets_PS_SetContent(t *testing.T) {
	targets := extractWriteTargets(
		`Set-Content -Path "/outside/file.md" -Value $content`,
		sandbox.Shell{Kind: sandbox.ShellPowerShell},
	)
	if len(targets) != 1 || targets[0] != `/outside/file.md` {
		t.Fatalf("got %v, want [/outside/file.md]", targets)
	}
}

func TestExtractWriteTargets_PS_OutFile(t *testing.T) {
	targets := extractWriteTargets(
		`Out-File -FilePath /foo/bar.txt -InputObject $x`,
		sandbox.Shell{Kind: sandbox.ShellPowerShell},
	)
	if len(targets) != 1 || targets[0] != `/foo/bar.txt` {
		t.Fatalf("got %v, want [/foo/bar.txt]", targets)
	}
}

func TestExtractWriteTargets_PS_AddContent(t *testing.T) {
	targets := extractWriteTargets(
		`Add-Content -Path "/outside/file.log" -Value "log line"`,
		sandbox.Shell{Kind: sandbox.ShellPowerShell},
	)
	if len(targets) != 1 || targets[0] != `/outside/file.log` {
		t.Fatalf("got %v, want [/outside/file.log]", targets)
	}
}

func TestExtractWriteTargets_PS_CaseInsensitive(t *testing.T) {
	targets := extractWriteTargets(
		`set-content -path "/mixed/CASE.md" -Value x`,
		sandbox.Shell{Kind: sandbox.ShellPowerShell},
	)
	if len(targets) != 1 || targets[0] != `/mixed/CASE.md` {
		t.Fatalf("got %v, want [/mixed/CASE.md]", targets)
	}
}

func TestExtractWriteTargets_PS_FlagEquals(t *testing.T) {
	targets := extractWriteTargets(
		`Set-Content -Path=/path/to/file.txt -Value x`,
		sandbox.Shell{Kind: sandbox.ShellPowerShell},
	)
	if len(targets) != 1 || targets[0] != `/path/to/file.txt` {
		t.Fatalf("got %v, want [/path/to/file.txt]", targets)
	}
}

func TestExtractWriteTargets_ShellRedirect(t *testing.T) {
	targets := extractWriteTargets(
		`echo hello > /tmp/out.txt`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 1 || targets[0] != `/tmp/out.txt` {
		t.Fatalf("got %v, want [/tmp/out.txt]", targets)
	}
}

func TestExtractWriteTargets_ShellAppendRedirect(t *testing.T) {
	targets := extractWriteTargets(
		`echo hello >> /var/log/app.log`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 1 || targets[0] != `/var/log/app.log` {
		t.Fatalf("got %v, want [/var/log/app.log]", targets)
	}
}

func TestExtractWriteTargets_ShellStderrRedirect(t *testing.T) {
	targets := extractWriteTargets(
		`cmd 2> /tmp/err.log`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 1 || targets[0] != `/tmp/err.log` {
		t.Fatalf("got %v, want [/tmp/err.log]", targets)
	}
}

func TestExtractWriteTargets_ShellRedirectFdPassthrough(t *testing.T) {
	// 2>&1 should NOT be treated as a write target — it's fd passthrough.
	targets := extractWriteTargets(
		`cmd 2>&1`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 0 {
		t.Fatalf("got %v, want [] (2>&1 is fd passthrough, not a file write)", targets)
	}
}

func TestExtractWriteTargets_ShellDevNullIgnored(t *testing.T) {
	targets := extractWriteTargets(
		`cmd 2>/dev/null`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 0 {
		t.Fatalf("got %v, want [] (/dev/null writes should be ignored)", targets)
	}
}

func TestExtractWriteTargets_Tee(t *testing.T) {
	targets := extractWriteTargets(
		`echo hello | tee /tmp/tee_out.txt`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 1 || targets[0] != `/tmp/tee_out.txt` {
		t.Fatalf("got %v, want [/tmp/tee_out.txt]", targets)
	}
}

func TestExtractWriteTargets_Dd(t *testing.T) {
	targets := extractWriteTargets(
		`dd if=/dev/zero of=/tmp/dd_out.bin bs=1024 count=1`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 1 || targets[0] != `/tmp/dd_out.bin` {
		t.Fatalf("got %v, want [/tmp/dd_out.bin]", targets)
	}
}

func TestExtractWriteTargets_BashOnlyNoPS(t *testing.T) {
	// Set-Content in a bash context (no PowerShell) — bash won't recognize it as a cmdlet.
	targets := extractWriteTargets(
		`Set-Content -Path /tmp/x.txt -Value hi`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	// Bash doesn't have Set-Content, but PowerShell patterns are only extracted when sh.Kind == PowerShell.
	// However, the command might still match redirect patterns — verify none here.
	found := false
	for _, tgt := range targets {
		if strings.Contains(tgt, "x.txt") {
			found = true
		}
	}
	if found {
		t.Fatalf("should not extract PS paths in bash context, got %v", targets)
	}
}

func TestExtractWriteTargets_Empty(t *testing.T) {
	targets := extractWriteTargets(
		`echo "hello world"`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 0 {
		t.Fatalf("got %v, want []", targets)
	}
}

func TestExtractWriteTargets_MultipleRedirects(t *testing.T) {
	targets := extractWriteTargets(
		`cmd > /tmp/a.txt 2> /tmp/b.txt`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 2 {
		t.Fatalf("got %d targets %v, want 2", len(targets), targets)
	}
}

func TestCheckBashWriteTargets_PassesInRoot(t *testing.T) {
	root := t.TempDir()
	err := checkBashWriteTargets(
		`echo hello > `+root+`/file.txt`,
		sandbox.Shell{Kind: sandbox.ShellBash},
		[]string{root},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckBashWriteTargets_BlocksOutsideRoot(t *testing.T) {
	root := t.TempDir()
	err := checkBashWriteTargets(
		`echo hello > /outside/file.txt`,
		sandbox.Shell{Kind: sandbox.ShellBash},
		[]string{root},
	)
	if err == nil {
		t.Fatal("expected error for path outside write roots, got nil")
	}
	if !strings.Contains(err.Error(), "outside the writable roots") {
		t.Errorf("error should mention writable roots, got: %v", err)
	}
}

func TestCheckBashWriteTargets_BlocksRedirectOutsideRoot(t *testing.T) {
	err := checkBashWriteTargets(
		`echo hello > /etc/passwd`,
		sandbox.Shell{Kind: sandbox.ShellBash},
		[]string{`/workspace`},
	)
	if err == nil {
		t.Fatal("expected error for redirect outside write roots, got nil")
	}
}

func TestCheckBashWriteTargets_EmptyRootsAllowed(t *testing.T) {
	err := checkBashWriteTargets(
		`echo hello > /anywhere/file.txt`,
		sandbox.Shell{Kind: sandbox.ShellBash},
		nil,
	)
	if err != nil {
		t.Fatalf("empty roots should be unconfined, got: %v", err)
	}
}

func TestExtractWriteTargets_PS_NewItem(t *testing.T) {
	targets := extractWriteTargets(
		`New-Item -Path "/workspace/newfile.txt" -ItemType File`,
		sandbox.Shell{Kind: sandbox.ShellPowerShell},
	)
	if len(targets) != 1 || targets[0] != `/workspace/newfile.txt` {
		t.Fatalf("got %v, want [/workspace/newfile.txt]", targets)
	}
}

func TestExtractWriteTargets_SingleQuotedPath(t *testing.T) {
	targets := extractWriteTargets(
		`Set-Content -Path '/Program Files/app/config.ini' -Value x`,
		sandbox.Shell{Kind: sandbox.ShellPowerShell},
	)
	if len(targets) != 1 || targets[0] != `/Program Files/app/config.ini` {
		t.Fatalf("got %v, want [/Program Files/app/config.ini]", targets)
	}
}

func TestExtractWriteTargets_BashAmpersandRedirect(t *testing.T) {
	// ">& file" redirects both stdout+stderr to file — must be caught.
	targets := extractWriteTargets(
		`cmd >& /tmp/out.txt`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 1 || targets[0] != `/tmp/out.txt` {
		t.Fatalf("got %v, want [/tmp/out.txt]", targets)
	}
}

func TestExtractWriteTargets_BashAmpersandBothRedirect(t *testing.T) {
	// "&> file" is equivalent to ">& file" in bash.
	targets := extractWriteTargets(
		`cmd &> /tmp/both.txt`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 1 || targets[0] != `/tmp/both.txt` {
		t.Fatalf("got %v, want [/tmp/both.txt]", targets)
	}
}

func TestExtractWriteTargets_FdPassthroughStillIgnored(t *testing.T) {
	// 2>&1, 1>&2, and &> with digit are still fd passthrough.
	targets := extractWriteTargets(
		`cmd 2>&1 1>&2`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 0 {
		t.Fatalf("got %v, want [] (fd passthrough should be ignored)", targets)
	}
}

func TestExtractWriteTargets_TeeMultiFile(t *testing.T) {
	targets := extractWriteTargets(
		`echo hi | tee /tmp/a.txt /tmp/b.txt /tmp/c.txt`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 3 {
		t.Fatalf("got %d targets %v, want 3", len(targets), targets)
	}
	for i, want := range []string{"/tmp/a.txt", "/tmp/b.txt", "/tmp/c.txt"} {
		if targets[i] != want {
			t.Fatalf("targets[%d] = %q, want %q", i, targets[i], want)
		}
	}
}

func TestExtractWriteTargets_TeeAppend(t *testing.T) {
	targets := extractWriteTargets(
		`echo hi | tee -a /tmp/appended.log`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 1 || targets[0] != `/tmp/appended.log` {
		t.Fatalf("got %v, want [/tmp/appended.log]", targets)
	}
}

func TestExtractWriteTargets_TeeAppendLongFlag(t *testing.T) {
	targets := extractWriteTargets(
		`echo hi | tee --append /tmp/appended2.log /tmp/appended3.log`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	if len(targets) != 2 {
		t.Fatalf("got %d targets %v, want 2", len(targets), targets)
	}
}

func TestExtractWriteTargets_ExportCsv(t *testing.T) {
	targets := extractWriteTargets(
		`Get-Process | Export-Csv -Path /tmp/procs.csv`,
		sandbox.Shell{Kind: sandbox.ShellPowerShell},
	)
	if len(targets) != 1 || targets[0] != `/tmp/procs.csv` {
		t.Fatalf("got %v, want [/tmp/procs.csv]", targets)
	}
}

func TestExtractWriteTargets_ExportClixml(t *testing.T) {
	targets := extractWriteTargets(
		`Get-Process | Export-Clixml -Path /tmp/procs.xml`,
		sandbox.Shell{Kind: sandbox.ShellPowerShell},
	)
	if len(targets) != 1 || targets[0] != `/tmp/procs.xml` {
		t.Fatalf("got %v, want [/tmp/procs.xml]", targets)
	}
}

func TestCheckBashWriteTargets_TeeMultiFileBlocksOutside(t *testing.T) {
	root := t.TempDir()
	err := checkBashWriteTargets(
		`echo hi | tee `+root+`/ok.txt /outside/bad.txt`,
		sandbox.Shell{Kind: sandbox.ShellBash},
		[]string{root},
	)
	if err == nil {
		t.Fatal("expected error for tee with one file outside write roots, got nil")
	}
}

func TestExtractWriteTargets_ProcessSubstitutionNotFalsePositive(t *testing.T) {
	// >(...) is process substitution, not a file redirect — should NOT be caught.
	targets := extractWriteTargets(
		`diff <(ls) <(ls -la)`,
		sandbox.Shell{Kind: sandbox.ShellBash},
	)
	// <(... ) are reads, >(...) are writes — but our parser only looks for `>` as
	// a redirect operator, and `>(...)` has no space before `(`. Verify no
	// false positive.
	if len(targets) != 0 {
		t.Fatalf("process substitution should not produce targets, got %v", targets)
	}
}
