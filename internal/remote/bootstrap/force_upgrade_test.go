package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/remote"
)

// TestForceUpgradePrefersUploadAndSkipsLocate: a same-platform force upgrade
// must skip the locate fast-path and the npm-first ladder, installing via the
// local binary upload instead.
func TestForceUpgradePrefersUploadAndSkipsLocate(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	paths := pathsFor(root, root)
	localBin := filepath.Join(t.TempDir(), "reasonix")
	if err := os.WriteFile(localBin, []byte("fake-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			t.Errorf("force upgrade must skip the locate fast-path; ran: %s", cmd)
			return ok("")
		case strings.Contains(cmd, "npm i -g"):
			t.Errorf("npm must not run before exact-release sources; ran: %s", cmd)
			return ok("")
		case strings.Contains(cmd, "--version"):
			// LocateUploadedCommand probing the freshly uploaded binary.
			return ok(uploadedBinPath(root) + "\nreasonix v1.9.5\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "nohup"):
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:44321\n"), 0o600)
			return ok("54321\n")
		case strings.Contains(cmd, "kill -0"), strings.Contains(cmd, "ps -p"):
			return ok("1\n")
		default:
			return ok("")
		}
	})
	res, err := EnsureServe(context.Background(), conn, Options{
		Workspace:      "~",
		ForceUpgrade:   true,
		LocalBinary:    localBin,
		LocalGOOS:      "linux",
		LocalGOARCH:    "amd64",
		ProductVersion: "1.9.5",
		Clock:          time.Now,
	})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if res.Reused {
		t.Fatal("force upgrade must not report reuse")
	}
	if res.State.Version != "1.9.5" {
		t.Fatalf("state version = %q, want 1.9.5", res.State.Version)
	}
}

// TestForceUpgradeCrossPlatformFetchesRelease: with no same-platform binary,
// the official release download at ProductVersion runs before npm.
func TestForceUpgradeCrossPlatformFetchesRelease(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	paths := pathsFor(root, root)
	var fetched string
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "npm i -g"):
			t.Errorf("npm must not run before the release download; ran: %s", cmd)
			return ok("")
		case strings.Contains(cmd, "--version"):
			return ok(uploadedBinPath(root) + "\nreasonix v1.9.5\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "nohup"):
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:44321\n"), 0o600)
			return ok("54321\n")
		case strings.Contains(cmd, "kill -0"), strings.Contains(cmd, "ps -p"):
			return ok("1\n")
		default:
			return ok("")
		}
	})
	_, err := EnsureServe(context.Background(), conn, Options{
		Workspace:      "~",
		ForceUpgrade:   true,
		LocalGOOS:      "windows", // cross-platform: the upload path is impossible
		ProductVersion: "1.9.5",
		FetchBinary: func(_ context.Context, version, goos, goarch string) ([]byte, error) {
			fetched = version + "/" + goos + "/" + goarch
			return []byte("fake-release-cli"), nil
		},
		Clock: time.Now,
	})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if fetched != "1.9.5/linux/amd64" {
		t.Fatalf("FetchBinary args = %q, want 1.9.5/linux/amd64", fetched)
	}
}

// TestForceUpgradeRejectsShortVersion: every rung landing below the target
// is an error naming the shortfall, not a silent half-upgrade.
func TestForceUpgradeRejectsShortVersion(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "--version"):
			return ok(uploadedBinPath(root) + "\nreasonix v1.9.0\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		default:
			return ok("")
		}
	})
	_, err := EnsureServe(context.Background(), conn, Options{
		Workspace:      "~",
		ForceUpgrade:   true,
		ProductVersion: "1.9.5",
		FetchBinary: func(_ context.Context, _ string, _, _ string) ([]byte, error) {
			return []byte("fake-release-cli"), nil
		},
		Clock: time.Now,
	})
	if err == nil || !strings.Contains(err.Error(), `short of "1.9.5"`) {
		t.Fatalf("err = %v, want a short-of-target error", err)
	}
}

// TestUpgradeTargetMet is pure and runs on every platform.
func TestUpgradeTargetMet(t *testing.T) {
	if !upgradeTargetMet("1.9.5", "1.9.5") || !upgradeTargetMet("2.0.0", "1.9.5") {
		t.Fatal("met targets must pass")
	}
	if upgradeTargetMet("1.9.0", "1.9.5") || upgradeTargetMet("", "1.9.5") {
		t.Fatal("short or empty versions must fail a real target")
	}
	if !upgradeTargetMet("", "dev") || !upgradeTargetMet("1.2.3", "dev") {
		t.Fatal("a dev target must not gate")
	}
}

// TestForceUpgradeSkipsLiveServeReuse: a recorded live serve is not adopted by
// a forced upgrade, not even at the post-lock re-check another client could
// satisfy by relaunching the old serve mid-update.
func TestForceUpgradeSkipsLiveServeReuse(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st, _ := MarshalState(ServeState{PID: 777, Addr: "127.0.0.1:5000", Workspace: root, ServeCaps: ServeCapsToken, TokenFile: paths.TokenFile})
	if err := os.WriteFile(paths.StateJSON, st, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TokenFile, []byte("existing-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "kill -0"), strings.Contains(cmd, "ps -p"):
			return ok("1\n")
		case strings.Contains(cmd, "--version"):
			return ok(uploadedBinPath(root) + "\nreasonix v1.9.5\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "nohup"):
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:44321\n"), 0o600)
			return ok("54321\n")
		default:
			return ok("")
		}
	})
	res, err := EnsureServe(context.Background(), conn, Options{
		Workspace:      "~",
		ForceUpgrade:   true,
		ProductVersion: "1.9.5",
		FetchBinary: func(_ context.Context, _ string, _, _ string) ([]byte, error) {
			return []byte("fake-release-cli"), nil
		},
		Clock: time.Now,
	})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if res.Reused {
		t.Fatal("a forced upgrade must not report reuse")
	}
	if res.State.PID != 54321 {
		t.Fatalf("state pid = %d, want the relaunched serve", res.State.PID)
	}
	if !conn.ranContaining("kill -TERM 777") {
		t.Fatal("a serve another client relaunched mid-update must be retired before the forced launch")
	}
}

// TestForceUpgradeContinuesPastShortRung: a rung landing below the target
// falls through to the remaining rungs instead of failing the whole ladder.
func TestForceUpgradeContinuesPastShortRung(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	paths := pathsFor(root, root)
	localBin := filepath.Join(t.TempDir(), "reasonix")
	if err := os.WriteFile(localBin, []byte("fake-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	locateCalls := 0
	fetchCalled := false
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "--version"):
			locateCalls++
			version := "1.9.0"
			if locateCalls >= 2 {
				version = "1.9.5"
			}
			return ok(uploadedBinPath(root) + "\nreasonix v" + version + "\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "nohup"):
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:44321\n"), 0o600)
			return ok("54321\n")
		case strings.Contains(cmd, "kill -0"), strings.Contains(cmd, "ps -p"):
			return ok("1\n")
		default:
			return ok("")
		}
	})
	res, err := EnsureServe(context.Background(), conn, Options{
		Workspace:      "~",
		ForceUpgrade:   true,
		LocalBinary:    localBin,
		LocalGOOS:      "linux",
		LocalGOARCH:    "amd64",
		ProductVersion: "1.9.5",
		FetchBinary: func(_ context.Context, _ string, _, _ string) ([]byte, error) {
			fetchCalled = true
			return []byte("fake-release-cli"), nil
		},
		Clock: time.Now,
	})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if res.State.Version != "1.9.5" {
		t.Fatalf("state version = %q, want the later rung's 1.9.5", res.State.Version)
	}
	if !fetchCalled {
		t.Fatal("a short upload rung must fall through to the release download")
	}
}

// TestForceUpgradeNPMStrategyPinsVersion: the explicit npm strategy pins the
// install to the desktop version - translated to its published canary spec
// for preview prereleases - instead of installing latest.
func TestForceUpgradeNPMStrategyPinsVersion(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	paths := pathsFor(root, root)
	pinned := false
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "npm i -g reasonix@1.3.0-canary.42"):
			pinned = true
			return ok("")
		case strings.Contains(cmd, "npm i -g reasonix"):
			t.Errorf("forced npm upgrade must pin the translated spec; ran: %s", cmd)
			return ok("")
		case strings.Contains(cmd, "npm prefix"):
			return ok("/npm-global/bin/reasonix\nreasonix v1.3.0-preview.42\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "nohup"):
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:44321\n"), 0o600)
			return ok("54321\n")
		case strings.Contains(cmd, "kill -0"), strings.Contains(cmd, "ps -p"):
			return ok("1\n")
		default:
			return ok("")
		}
	})
	res, err := EnsureServe(context.Background(), conn, Options{
		Workspace:      "~",
		Install:        InstallNPM,
		ForceUpgrade:   true,
		ProductVersion: "1.3.0-preview.42",
		Clock:          time.Now,
	})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if !pinned {
		t.Fatal("the forced npm strategy must install the canary spec for a preview target")
	}
	if res.State.Version != "1.3.0-preview.42" {
		t.Fatalf("state version = %q, want the desktop-style preview", res.State.Version)
	}
}

// TestNPMVersionSpec is pure and runs on every platform.
func TestNPMVersionSpec(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.3.0-preview.42", "1.3.0-canary.42"},
		{"1.3.0-preview.9", "1.3.0-canary.9"},
		{"1.38.7", "1.38.7"},
		{"1.40.0-rc.1", "1.40.0-rc.1"},
		{"dev", "dev"},
	}
	for _, c := range cases {
		if got := npmVersionSpec(c.in); got != c.want {
			t.Errorf("npmVersionSpec(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestForceUpgradeRestoresBinaryOnFailedLaunch: the pre-upgrade managed
// binary comes back when the replacement fails to become a healthy serve.
func TestForceUpgradeRestoresBinaryOnFailedLaunch(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	managed := uploadedBinPath(root)
	if err := os.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("previous-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A clock that jumps forward on every call fails the port-file poll
	// immediately, driving the launch into its failure cleanup.
	step := time.Now()
	clock := func() time.Time { step = step.Add(30 * time.Second); return step }
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "--version"):
			return ok(managed + "\nreasonix v1.9.5\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "nohup"):
			return ok("0\n")
		default:
			return ok("")
		}
	})
	_, err := EnsureServe(context.Background(), conn, Options{
		Workspace:      "~",
		ForceUpgrade:   true,
		ProductVersion: "1.9.5",
		FetchBinary: func(_ context.Context, _ string, _, _ string) ([]byte, error) {
			return []byte("replacement-binary"), nil
		},
		Clock: clock,
	})
	if err == nil {
		t.Fatal("a launch that never reports a port must fail")
	}
	data, rerr := os.ReadFile(managed)
	if rerr != nil {
		t.Fatalf("managed binary missing after failed launch: %v", rerr)
	}
	if string(data) != "previous-binary" {
		t.Fatalf("managed binary = %q, want the pre-upgrade binary restored", string(data))
	}
	if _, serr := os.Stat(managed + ".prev"); serr == nil {
		t.Fatal("the backup must be consumed by the restore")
	}
}

// TestCompareVersionsPrerelease is pure and runs on every platform.
func TestCompareVersionsPrerelease(t *testing.T) {
	ordered := []string{
		"1.40.0-preview.9",
		"1.40.0-preview.10",
		"1.40.0-preview.41",
		"1.40.0-preview.42",
		"1.40.0-rc.1",
		"1.40.0",
		"1.40.1",
	}
	for i := 0; i+1 < len(ordered); i++ {
		if CompareVersions(ordered[i], ordered[i+1]) >= 0 {
			t.Errorf("CompareVersions(%q, %q) must be -1", ordered[i], ordered[i+1])
		}
		if CompareVersions(ordered[i+1], ordered[i]) <= 0 {
			t.Errorf("CompareVersions(%q, %q) must be 1", ordered[i+1], ordered[i])
		}
	}
	if CompareVersions("1.40.0-preview.42", "1.40.0-preview.42") != 0 {
		t.Error("identical prereleases must compare equal")
	}
}

// TestForceUpgradeNeverKeepsManagedBinary: a never-install host rejects the
// upgrade before the managed binary is renamed aside, leaving it launchable.
func TestForceUpgradeNeverKeepsManagedBinary(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	managed := uploadedBinPath(root)
	if err := os.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		if strings.Contains(cmd, "uname") {
			return ok("Linux x86_64\n")
		}
		return ok("")
	})
	_, err := EnsureServe(context.Background(), conn, Options{
		Workspace:    "~",
		Install:      InstallNever,
		ForceUpgrade: true,
		Clock:        time.Now,
	})
	if err == nil || !strings.Contains(err.Error(), "serve_install = never forbids upgrading") {
		t.Fatalf("err = %v, want the never-strategy rejection", err)
	}
	data, rerr := os.ReadFile(managed)
	if rerr != nil || string(data) != "old-binary" {
		t.Fatalf("managed binary = %q (%v), want it untouched", string(data), rerr)
	}
	if _, serr := os.Stat(managed + ".prev"); serr == nil {
		t.Fatal("no backup may be created for a rejected never upgrade")
	}
}

// TestForceUpgradeLadderErrorRestoresManaged: when every rung fails, the
// pre-upgrade managed binary comes back before the error returns.
func TestForceUpgradeLadderErrorRestoresManaged(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	managed := uploadedBinPath(root)
	if err := os.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "--version"):
			return ok(managed + "\nreasonix v1.9.0\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		default:
			return ok("")
		}
	})
	_, err := EnsureServe(context.Background(), conn, Options{
		Workspace:      "~",
		ForceUpgrade:   true,
		ProductVersion: "1.9.5",
		FetchBinary: func(_ context.Context, _ string, _, _ string) ([]byte, error) {
			return []byte("fake-release-cli"), nil
		},
		Clock: time.Now,
	})
	if err == nil {
		t.Fatal("an all-short ladder must fail")
	}
	data, rerr := os.ReadFile(managed)
	if rerr != nil || string(data) != "old-binary" {
		t.Fatalf("managed binary = %q (%v), want the pre-upgrade binary restored", string(data), rerr)
	}
}

// TestForceUpgradeNPMRungRestoresManaged: when the pinned npm rung wins, the
// rejected short upload is replaced by the pre-upgrade managed binary again.
func TestForceUpgradeNPMRungRestoresManaged(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	paths := pathsFor(root, root)
	managed := uploadedBinPath(root)
	if err := os.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	localBin := filepath.Join(t.TempDir(), "reasonix")
	if err := os.WriteFile(localBin, []byte("fake-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	locateCalls := 0
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "npm prefix"):
			return ok("/npm-global/bin/reasonix\nreasonix v1.9.5\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "--version"):
			locateCalls++
			version := "1.9.0"
			if locateCalls >= 2 {
				version = "1.9.5"
			}
			return ok(managed + "\nreasonix v" + version + "\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "nohup"):
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:44321\n"), 0o600)
			return ok("54321\n")
		case strings.Contains(cmd, "kill -0"), strings.Contains(cmd, "ps -p"):
			return ok("1\n")
		default:
			return ok("")
		}
	})
	res, err := EnsureServe(context.Background(), conn, Options{
		Workspace:      "~",
		ForceUpgrade:   true,
		LocalBinary:    localBin,
		LocalGOOS:      "linux",
		LocalGOARCH:    "amd64",
		ProductVersion: "1.9.5",
		Clock:          time.Now,
	})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if res.State.Version != "1.9.5" {
		t.Fatalf("state version = %q, want the npm rung's 1.9.5", res.State.Version)
	}
	data, rerr := os.ReadFile(managed)
	if rerr != nil || string(data) != "old-binary" {
		t.Fatalf("managed binary = %q (%v), want the pre-upgrade binary restored", string(data), rerr)
	}
	if !conn.ranContaining("npm-global/bin/reasonix") {
		t.Fatal("the serve must launch from the npm rung's binary")
	}
}

// TestForceUpgradeNPMRollbackOnFailedLaunch: an unhealthy replacement under
// the explicit npm strategy reinstalls the previous global package.
func TestForceUpgradeNPMRollbackOnFailedLaunch(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	installed := false
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "npm i -g reasonix@1.9.5"):
			installed = true
			return ok("")
		case strings.Contains(cmd, "npm prefix"):
			version := "1.38.6"
			if installed {
				version = "1.9.5"
			}
			return ok("/npm-global/bin/reasonix\nreasonix v" + version + "\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "--version"):
			return ok(uploadedBinPath(root) + "\nreasonix v" + map[bool]string{true: "1.9.5", false: "1.38.6"}[installed] + "\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "nohup"):
			return ok("0\n")
		default:
			return ok("")
		}
	})
	step := time.Now()
	clock := func() time.Time { step = step.Add(30 * time.Second); return step }
	_, err := EnsureServe(context.Background(), conn, Options{
		Workspace:      "~",
		Install:        InstallNPM,
		ForceUpgrade:   true,
		ProductVersion: "1.9.5",
		Clock:          clock,
	})
	if err == nil {
		t.Fatal("a launch that never reports a port must fail")
	}
	if !conn.ranContaining("npm i -g reasonix@1.38.6") {
		t.Fatal("a failed npm replacement must roll back to the previous package")
	}
}
