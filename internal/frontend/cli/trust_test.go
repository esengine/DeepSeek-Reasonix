package cli

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func TestTrustApprovesWhatTheWorkspaceNamesOnlyWhenAsked(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.MkdirAll(filepath.Join(root, ".reasonix"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".reasonix", "settings.json"), []byte(`{"hooks":{"Stop":[{"command":"echo stop"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rg := filepath.Join(filepath.VolumeName(os.TempDir())+string(filepath.Separator), "opt", "reasonix-test", "rg")
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[tools.search]\nrg_path = '"+rg+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(input string, interactive bool, args ...string) (int, string) {
		var out bytes.Buffer
		code := runTrust(append([]string{"--dir", root}, args...), bufio.NewScanner(strings.NewReader(input)), &out, interactive)
		return code, out.String()
	}

	if code, out := run("", false); code != 1 || !strings.Contains(out, "echo stop") || !strings.Contains(out, rg) {
		t.Fatalf("non-interactive listing = %d %q, want both programs listed and nothing approved", code, out)
	}
	if code, _ := run("n\n", true); code != 1 {
		t.Fatalf("declined prompt exit = %d, want 1", code)
	}
	if pending, _ := pendingProjectPrograms(config.Roots{}, root); len(pending) != 2 {
		t.Fatalf("pending after declining = %d, want 2", len(pending))
	}
	if code, out := run("y\n", true); code != 0 {
		t.Fatalf("approving = %d %q", code, out)
	}
	if pending, _ := pendingProjectPrograms(config.Roots{}, root); len(pending) != 0 {
		t.Fatalf("pending after approval = %+v, want none", pending)
	}
	if code, _ := run("", false, "--revoke"); code != 0 {
		t.Fatal("revoke failed")
	}
	if pending, _ := pendingProjectPrograms(config.Roots{}, root); len(pending) != 2 {
		t.Fatalf("pending after revoke = %d, want 2", len(pending))
	}
}

func TestTrustRecordsTheFolderAndRevokeForgetsIt(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	grants := config.NewProjectGrantStore(config.Roots{}.Home())
	run := func(input string, interactive bool, args ...string) int {
		var out bytes.Buffer
		return runTrust(append([]string{"--dir", root}, args...), bufio.NewScanner(strings.NewReader(input)), &out, interactive)
	}
	trust := func() config.WorkspaceTrust {
		got, err := grants.Trust(root)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if code := run("", false); code != 1 || trust() != config.WorkspaceTrustUndecided {
		t.Fatalf("an unattended trust without --yes = %d, trust %q; it must record nothing", code, trust())
	}
	if code := run("n\n", true); code != 1 || trust() != config.WorkspaceTrustUndecided {
		t.Fatalf("declining = %d, trust %q", code, trust())
	}
	if code := run("y\n", true); code != 0 || trust() != config.WorkspaceTrusted {
		t.Fatalf("accepting = %d, trust %q", code, trust())
	}
	if code := run("", false); code != 0 {
		t.Fatalf("a trusted folder with nothing pending = %d, want 0", code)
	}
	if code := run("", false, "--revoke"); code != 0 || trust() != config.WorkspaceTrustUndecided {
		t.Fatalf("revoke = %d, trust %q", code, trust())
	}
	if code := run("", false, "--yes"); code != 0 || trust() != config.WorkspaceTrusted {
		t.Fatalf("--yes = %d, trust %q", code, trust())
	}
}

func TestTrustNeverTrustsAHomeDirectoryWholesale(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	home := homeDir(t)
	var out bytes.Buffer
	if code := runTrust([]string{"--dir", home, "--yes"}, bufio.NewScanner(strings.NewReader("")), &out, false); code != 0 {
		t.Fatalf("trust in a home directory = %d: %s", code, out.String())
	}
	if got, _ := config.NewProjectGrantStore(config.Roots{}.Home()).Trust(home); got != config.WorkspaceTrustUndecided {
		t.Fatalf("a home directory was recorded as %q", got)
	}
	if !strings.Contains(out.String(), "not trusted as a whole") {
		t.Fatalf("the refusal is not said: %s", out.String())
	}
}
