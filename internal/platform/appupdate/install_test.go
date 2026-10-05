package appupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/update"
)

type countingOwner struct{ ended chan struct{} }

func (countingOwner) PrepareForUpdate(context.Context) error    { return nil }
func (countingOwner) RelaunchAfterUpdate(context.Context) error { return nil }
func (o countingOwner) EndApplication(context.Context)          { close(o.ended) }

func owned(t *testing.T, running string) (*capability, countingOwner) {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	o := countingOwner{ended: make(chan struct{})}
	return New(Options{Owner: o, Running: running}).(*capability), o
}

func settled(t *testing.T, c *capability, phases ...string) update.Progress {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if p := c.InstallProgress(); slices.Contains(phases, p.Phase) {
			return p
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("progress never reached %v: %+v", phases, c.InstallProgress())
	return update.Progress{}
}

func ready(c *capability, target string, apply func(context.Context) error) {
	dir, _ := update.CacheDir()
	c.install.park(&readyMove{target: target, cache: dir, apply: apply})
}

// Downloading is not consent to restart: a verified release waits, and nothing
// installs or ends until the restart is asked for.
func TestAReadyReleaseWaitsForTheRestart(t *testing.T) {
	c, o := owned(t, "2.20.0")
	applied := 0
	ready(c, "2.21.0", func(context.Context) error { applied++; return nil })
	time.Sleep(20 * time.Millisecond)
	if applied != 0 || c.InstallProgress().Phase != update.PhaseReady {
		t.Fatalf("applied %d times before any restart, progress %+v", applied, c.InstallProgress())
	}
	if err := c.CommitInstall("2.21.0"); err != nil {
		t.Fatal(err)
	}
	<-o.ended
	if applied != 1 {
		t.Fatalf("applied %d times, want 1", applied)
	}
	if err := c.CommitInstall("2.21.0"); !errors.Is(err, ErrNothingReady) {
		t.Fatalf("second restart = %v, want ErrNothingReady", err)
	}
}

// Going forward leaves no pin, and releases one the user had.
func TestMovingForwardNeverPins(t *testing.T) {
	c, o := owned(t, "2.20.0")
	if err := update.Pin("2.20.0"); err != nil {
		t.Fatal(err)
	}
	ready(c, "2.21.0", func(context.Context) error { return nil })
	if err := c.CommitInstall("2.21.0"); err != nil {
		t.Fatal(err)
	}
	<-o.ended
	if got := update.PinnedVersion(); got != "" {
		t.Fatalf("pinned %q after moving forward, want none", got)
	}
}

// A rollback holds the chosen build, written once the install is allowed.
func TestRollingBackPinsTheChosenBuild(t *testing.T) {
	c, o := owned(t, "2.21.0")
	ready(c, "2.19.0", func(context.Context) error {
		if got := update.PinnedVersion(); got != "2.19.0" {
			return fmt.Errorf("install ran with pin %q", got)
		}
		return nil
	})
	if err := c.CommitInstall("2.19.0"); err != nil {
		t.Fatal(err)
	}
	<-o.ended
	if got := update.PinnedVersion(); got != "2.19.0" {
		t.Fatalf("pinned %q, want 2.19.0", got)
	}
}

// An install that does not happen leaves the pin as it found it, and says why
// with the installer's code rather than a bare sentence.
func TestAFailedInstallLeavesNoPinBehind(t *testing.T) {
	c, _ := owned(t, "2.21.0")
	ready(c, "2.19.0", func(context.Context) error { return errors.New("installer refused") })
	if err := c.CommitInstall("2.19.0"); err != nil {
		t.Fatal(err)
	}
	p := settled(t, c, update.PhaseFailed)
	if p.Code != FailInstaller {
		t.Fatalf("code %q, want %q", p.Code, FailInstaller)
	}
	if got := update.PinnedVersion(); got != "" {
		t.Fatalf("pinned %q after a failed install, want none", got)
	}
}

// A dismissed system prompt keeps the release ready, and the pin unwritten.
func TestADismissedPromptKeepsTheReleaseReady(t *testing.T) {
	c, _ := owned(t, "2.21.0")
	ready(c, "2.19.0", func(context.Context) error { return update.ErrDebAuthCancelled })
	if err := c.CommitInstall("2.19.0"); err != nil {
		t.Fatal(err)
	}
	settled(t, c, update.PhaseReady)
	if got := update.PinnedVersion(); got != "" {
		t.Fatalf("pinned %q after a dismissed prompt, want none", got)
	}
}

// The launch after a move forward is the only witness to whether it landed.
// Still running the old build is a failure, reported with the helper's reason.
func TestTheNextLaunchReportsAMoveThatDidNotLand(t *testing.T) {
	c, o := owned(t, "2.20.0")
	ready(c, "2.21.0", func(context.Context) error { return nil })
	if err := c.CommitInstall("2.21.0"); err != nil {
		t.Fatal(err)
	}
	<-o.ended
	dir, _ := update.CacheDir()
	if err := os.WriteFile(filepath.Join(dir, swapOutcomeName), []byte("update: set aside app.exe: access denied\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	again := New(Options{Owner: stubOwner{}, Running: "2.20.0"}).(*capability)
	p := again.InstallProgress()
	if p.Phase != update.PhaseFailed || p.Code != FailNotApplied || p.Version != "2.21.0" || p.Err != "update: set aside app.exe: access denied" {
		t.Fatalf("progress %+v, want a not-applied failure for 2.21.0 carrying the helper's reason", p)
	}
	if third := New(Options{Owner: stubOwner{}, Running: "2.20.0"}).(*capability); third.InstallProgress().Phase != "" {
		t.Fatalf("the record was reported twice: %+v", third.InstallProgress())
	}
}

func TestAMoveThatLandedReportsNothing(t *testing.T) {
	_, _ = owned(t, "2.20.0")
	dir, _ := update.CacheDir()
	if err := recordMove(dir, pendingMove{From: "2.20.0", To: "2.21.0"}); err != nil {
		t.Fatal(err)
	}
	c := New(Options{Owner: stubOwner{}, Running: "2.21.0"}).(*capability)
	if p := c.InstallProgress(); p.Phase != "" {
		t.Fatalf("a landed move reported %+v", p)
	}
	if _, err := os.Stat(filepath.Join(dir, moveRecordName)); !os.IsNotExist(err) {
		t.Fatalf("the record outlived the launch that settled it: %v", err)
	}
}

// A pin naming another build holds nothing; a source build proves nothing.
func TestAStalePinIsReleasedAtLaunch(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	for _, tc := range []struct{ pin, running, want string }{
		{"2.21.0", "2.20.0", ""},
		{"2.19.0", "2.20.0", ""},
		{"2.20.0", "v2.20.0", "2.20.0"},
		{"2.20.0", "dev", "2.20.0"},
	} {
		if err := update.Pin(tc.pin); err != nil {
			t.Fatal(err)
		}
		New(Options{Owner: stubOwner{}, Running: tc.running})
		if got := update.PinnedVersion(); got != tc.want {
			t.Errorf("pin %q running %q: pinned %q, want %q", tc.pin, tc.running, got, tc.want)
		}
	}
}

func TestDownloadFailuresKeepTheirClass(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: %w", update.ErrFetch, context.DeadlineExceeded), FailDownload},
		{fmt.Errorf("%w: bad sig", update.ErrVerify), FailVerify},
		{fmt.Errorf("%w: disk full", update.ErrStore), FailDisk},
		{failAs(FailCatalog, errors.New("catalog 503")), FailCatalog},
		{errors.New("something else"), FailUnknown},
	} {
		if got := failureCode(tc.err); got != tc.want {
			t.Errorf("failureCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
