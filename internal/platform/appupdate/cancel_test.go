package appupdate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/update"
)

func TestCancelStopsTheDownloadAndIgnoresWhatItStillEmits(t *testing.T) {
	var s installState
	if _, ok := s.begin("2.21.0"); !ok {
		t.Fatal("begin refused an idle slot")
	}
	cancel, done, ok := s.abandon("2.21.0")
	if !ok {
		t.Fatal("abandon refused a running download")
	}
	cancel(errInstallCancelled)
	s.finish()
	<-done

	if got := s.read(); got.Phase != update.PhaseCancelled || got.Version != "2.21.0" {
		t.Fatalf("progress = %+v, want cancelled 2.21.0", got)
	}
	s.set(update.Progress{Version: "2.21.0", Phase: update.PhaseDownloading, Received: 9})
	s.park(&readyMove{target: "2.21.0"})
	if got := s.read(); got.Phase != update.PhaseCancelled {
		t.Fatalf("a late frame put the cancelled download back: %+v", got)
	}
	if _, ok := s.claim("2.21.0"); ok {
		t.Fatal("a cancelled download was still claimable")
	}
}

func TestCancelRefusesWhatIsNotADownload(t *testing.T) {
	var s installState
	if _, ok := s.begin("2.21.0"); !ok {
		t.Fatal("begin refused an idle slot")
	}
	s.park(&readyMove{target: "2.21.0"})
	if _, _, ok := s.abandon("2.21.0"); ok {
		t.Fatal("abandoned a release that was already verified")
	}
	if _, ok := s.claim("2.21.0"); !ok {
		t.Fatal("the verified release was not claimable")
	}
	if _, _, ok := s.abandon("2.21.0"); ok {
		t.Fatal("abandoned an install that was already applying")
	}
	if _, _, ok := s.abandon("2.20.0"); ok {
		t.Fatal("abandoned a version that was not being moved")
	}
	s.finish()
}

func TestCancelLeavesTheSlotFree(t *testing.T) {
	var s installState
	if _, ok := s.begin("2.21.0"); !ok {
		t.Fatal("begin refused an idle slot")
	}
	cancel, _, ok := s.abandon("2.21.0")
	if !ok {
		t.Fatal("abandon refused a running download")
	}
	cancel(errInstallCancelled)
	s.finish()
	if _, ok := s.begin("2.21.0"); !ok {
		t.Fatal("a cancelled download left the slot claimed")
	}
	s.finish()
}

func TestCancelWithoutADownloadNamesTheProblem(t *testing.T) {
	c, _ := owned(t, "2.20.0")
	if err := c.CancelInstall(""); !errors.Is(err, ErrNoTarget) {
		t.Fatalf("cancel with no version = %v, want ErrNoTarget", err)
	}
	if err := c.CancelInstall("2.21.0"); !errors.Is(err, ErrNothingToCancel) {
		t.Fatalf("cancel with nothing running = %v, want ErrNothingToCancel", err)
	}
}

// A cancelled prepare is what removes a partial artifact and an unassembled
// delta from the cache; a failed or timed-out one keeps them to resume.
func TestACancelledPrepareDiscardsWhatItFetched(t *testing.T) {
	c, _ := owned(t, "2.20.0")
	dir, err := update.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(dir, "partial-deadbeef.download")
	chunk := filepath.Join(dir, deltaWorkDir, "chunks", "chunk")
	if err := os.MkdirAll(filepath.Dir(chunk), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{partial, chunk} {
		if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	ctx, ok := c.install.begin("2.21.0")
	if !ok {
		t.Fatal("begin refused an idle slot")
	}
	cancel, done, ok := c.install.abandon("2.21.0")
	if !ok {
		t.Fatal("abandon refused a running download")
	}
	cancel(errInstallCancelled)
	c.prepare(ctx, update.Install{Layout: update.Layout{Root: testenv.TempDir(t)}}, "2.21.0")
	select {
	case <-done:
	default:
		t.Fatal("prepare did not release the claim")
	}

	if got := c.InstallProgress(); got.Phase != update.PhaseCancelled {
		t.Fatalf("progress = %+v, want cancelled", got)
	}
	for _, path := range []string{partial, chunk} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s outlived the cancellation: %v", path, err)
		}
	}
}

// A cancelled move owns the slot until its cleanup closes done. Starting a new
// move there would let the old prepare cancel the new context and discard the
// bytes the new download wrote.
func TestANewMoveWaitsForTheCancelledOneToCleanUp(t *testing.T) {
	var s installState
	if _, ok := s.begin("2.21.0"); !ok {
		t.Fatal("begin refused an idle slot")
	}
	cancel, done, ok := s.abandon("2.21.0")
	if !ok {
		t.Fatal("abandon refused a running download")
	}
	cancel(errInstallCancelled)
	if _, ok := s.begin("2.22.0"); ok {
		t.Fatal("a new move began while the cancelled one was still cleaning up")
	}
	s.finish()
	<-done
	if _, ok := s.begin("2.22.0"); !ok {
		t.Fatal("the slot stayed claimed after the cancellation cleaned up")
	}
	s.finish()
}

// The same window through the public surface: StartInstall answers
// ErrInstallInFlight until the cancelled move has cleaned up, then accepts.
func TestStartInstallIsRefusedWhileCancelCleansUp(t *testing.T) {
	c, _ := owned(t, "2.20.0")
	install := update.Install{Layout: update.Layout{Root: testenv.TempDir(t)}}
	if _, ok := c.install.begin("2.21.0"); !ok {
		t.Fatal("begin refused an idle slot")
	}
	cancel, done, ok := c.install.abandon("2.21.0")
	if !ok {
		t.Fatal("abandon refused a running download")
	}
	cancel(errInstallCancelled)
	if err := c.StartInstall(install, "2.22.0"); !errors.Is(err, ErrInstallInFlight) {
		t.Fatalf("start during cleanup = %v, want ErrInstallInFlight", err)
	}
	c.install.finish()
	<-done
	if err := c.StartInstall(install, "2.22.0"); err != nil {
		t.Fatalf("start after cleanup = %v", err)
	}
	if cancel, done, ok := c.install.abandon("2.22.0"); ok {
		cancel(errInstallCancelled)
		<-done
	}
}
