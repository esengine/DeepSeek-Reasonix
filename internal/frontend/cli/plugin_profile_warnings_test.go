package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginDoctorSkipsNonresidentProfileSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires host privileges")
	}
	home, outside := testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	root := pluginpkg.InstallRoot(home, "resident-diagnostics")
	writePluginTestFile(t, filepath.Join(root, pluginpkg.NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"resident-diagnostics","contributes":{"agents":["agents"]}}`)
	writePluginTestFile(t, filepath.Join(root, "agents", "valid.md"), "---\ndescription: Valid\n---\nBODY")
	writePluginTestFile(t, filepath.Join(outside, "fixture.md"), "---\ndelivery:\n  private_field_marker: private_value_marker\n---\nBODY")
	if err := os.Symlink(filepath.Join(outside, "fixture.md"), filepath.Join(root, "agents", "outside.md")); err != nil {
		t.Fatal(err)
	}
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "resident-diagnostics", Root: pluginpkg.RelativeRoot(home, root), Enabled: false}); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if rc := pluginCommand([]string{"doctor", "resident-diagnostics"}); rc != 0 {
			t.Fatalf("doctor rc=%d", rc)
		}
	})
	for _, forbidden := range []string{"private_field_marker", "private_value_marker", "outside.md:", "profile.delivery.unknown_field"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("nonresident diagnostic projected: %s", out)
		}
	}
	if !strings.Contains(out, "ok: resident-diagnostics") {
		t.Fatalf("doctor output=%s", out)
	}
}

func TestPluginDoctorReportsRejectedProfileDeclarationsWhenDisabled(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	root := filepath.Join(home, "plugins", "profile-warning-kit")
	writePluginTestFile(t, filepath.Join(root, pluginpkg.NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"profile-warning-kit","contributes":{"agents":["agents"]}}`)
	path := filepath.Join(root, "agents", "reviewer.md")
	writePluginTestFile(t, path, "---\ndescription: Review files\nauthority:\n  baseline: approved\n---\nReview.")
	directoryPath := filepath.Join(root, "agents", "group", "directory", "SKILL.md")
	writePluginTestFile(t, directoryPath, "---\ndescription: Review files\nauthority:\n  baseline: approved\n---\nReview.")
	for _, enabled := range []bool{true, false} {
		if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "profile-warning-kit", Root: "plugins/profile-warning-kit", Enabled: enabled}); err != nil {
			t.Fatal(err)
		}
		out := captureStdout(t, func() {
			if rc := pluginCommand([]string{"doctor", "profile-warning-kit"}); rc != 0 {
				t.Fatalf("doctor rc=%d", rc)
			}
		})
		for _, want := range []string{"warning: agents/reviewer.md", "warning: agents/group/directory/SKILL.md", "`authority:` is host-owned", "Remove the block.", "ok: profile-warning-kit"} {
			if !strings.Contains(out, want) {
				t.Errorf("enabled=%v doctor missing %q:\n%s", enabled, want, out)
			}
		}
	}
	writePluginTestFile(t, path, "---\ndescription: Review files\ndelivery:\n  review-report: security\n---\nReview.")
	writePluginTestFile(t, directoryPath, "---\ndescription: Review files\ndelivery:\n  review-report: security\n---\nReview.")
	out := captureStdout(t, func() {
		if rc := pluginCommand([]string{"doctor", "profile-warning-kit"}); rc != 0 {
			t.Fatalf("corrected doctor rc=%d", rc)
		}
	})
	if strings.Contains(out, "warning:") || !strings.Contains(out, "ok: profile-warning-kit") {
		t.Fatalf("corrected doctor = %s", out)
	}
}

func TestPluginDoctorPrintsProfileWarningsBeforeRootFailure(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	root := pluginpkg.InstallRoot(home, "profile-root-warning-kit")
	writePluginTestFile(t, filepath.Join(root, pluginpkg.NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"profile-root-warning-kit","contributes":{"agents":["agents","other.md"]}}`)
	writePluginTestFile(t, filepath.Join(root, "agents", "reviewer.md"), "---\ndescription: Review files\nauthority:\n  baseline: approved\n---\nReview.")
	writePluginTestFile(t, filepath.Join(root, "other.md"), "Not an agent directory.")
	for _, enabled := range []bool{true, false} {
		if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "profile-root-warning-kit", Root: pluginpkg.RelativeRoot(home, root), Enabled: enabled}); err != nil {
			t.Fatal(err)
		}
		var out string
		errOut := captureStderr(t, func() {
			out = captureStdout(t, func() {
				if rc := pluginCommand([]string{"doctor", "profile-root-warning-kit"}); rc != 1 {
					t.Errorf("doctor rc=%d, want 1", rc)
				}
			})
		})
		if !strings.Contains(out, "warning: agents/reviewer.md") || !strings.Contains(out, "`authority:` is host-owned") || strings.Contains(out, "ok:") {
			t.Errorf("enabled=%v profile warning before failure = %s", enabled, out)
		}
		if !strings.Contains(errOut, "missing agent root: "+filepath.Join(root, "other.md")) {
			t.Errorf("enabled=%v root rejection = %s", enabled, errOut)
		}
	}
}
