//go:build windows

package installlayout

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func silenceRetryDelay(t *testing.T) *[]time.Duration {
	t.Helper()
	restore := transientRetryDelay
	var delays []time.Duration
	transientRetryDelay = func(d time.Duration) { delays = append(delays, d) }
	t.Cleanup(func() { transientRetryDelay = restore })
	return &delays
}

func TestRetryTransientRecoversFromScannerLocks(t *testing.T) {
	delays := silenceRetryDelay(t)
	attempts := 0
	err := retryTransient(func() error {
		attempts++
		if attempts < 4 {
			return &os.PathError{Op: "rename", Path: "x", Err: windows.ERROR_SHARING_VIOLATION}
		}
		return nil
	})
	if err != nil || attempts != 4 || len(*delays) != 3 {
		t.Fatalf("attempts=%d delays=%v err=%v", attempts, *delays, err)
	}
	if (*delays)[0] != 250*time.Millisecond || (*delays)[2] != 750*time.Millisecond {
		t.Fatalf("backoff must grow linearly: %v", *delays)
	}
}

func TestRetryTransientGivesUpAndKeepsTheErrno(t *testing.T) {
	delays := silenceRetryDelay(t)
	attempts := 0
	err := retryTransient(func() error {
		attempts++
		return &os.PathError{Op: "remove", Path: "x", Err: windows.ERROR_ACCESS_DENIED}
	})
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) || attempts != transientRetryAttempts || len(*delays) != transientRetryAttempts-1 {
		t.Fatalf("attempts=%d delays=%d err=%v", attempts, len(*delays), err)
	}
}

// Windows refuses to rename a directory while any file inside it is open,
// which is how a scanner briefly blocks displacing an existing version tree.
func TestActivateVersionOutlastsAnOpenHandleInTheReplacedTree(t *testing.T) {
	root := t.TempDir()
	seed := ActivationRequest{InstallRoot: root, Version: "v1.20.0", RequestID: "seed", Members: writeMembers(t, t.TempDir(), "old")}
	if err := ActivateVersion(seed); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(filepath.Join(root, VersionsDirName, "v1.20.0", DesktopBinaryName()))
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(600 * time.Millisecond)
		held.Close()
		close(released)
	}()
	err = ActivateVersion(ActivationRequest{InstallRoot: root, Version: "v1.20.0", RequestID: "again", Members: writeMembers(t, t.TempDir(), "new")})
	<-released
	if err != nil {
		t.Fatalf("activation must wait out a transient handle: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, VersionsDirName, "v1.20.0", DesktopBinaryName()))
	if err != nil || string(data) != "new-"+DesktopBinaryName() {
		t.Fatalf("replacement not published: %q err=%v", data, err)
	}
}
