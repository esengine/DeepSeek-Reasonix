//go:build windows

package main

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/desktop/internal/instanceidentity"
)

func stubDesktopHandoff(t *testing.T) {
	t.Helper()
	verify, notify := verifyDesktopHandoffFn, notifyHandoffBlockedFn
	t.Cleanup(func() { verifyDesktopHandoffFn = verify; notifyHandoffBlockedFn = notify })
	verifyDesktopHandoffFn = func(string, bool) error { return nil }
	notifyHandoffBlockedFn = func(bool) {}
}
func TestDesktopHandoffConfirmsOnlyTargetInstance(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	target := filepath.Join(root, "reasonix-desktop.exe")
	old := filepath.Join(t.TempDir(), "reasonix-desktop.exe")
	for _, p := range []string{target, old} {
		if err := os.WriteFile(p, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	id := instanceidentity.ForHome(home)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv(instanceidentity.UpdateEnvironmentKey, id)
	original, now, sleep := desktopEndpointImageFn, handoffNowFn, handoffSleepFn
	t.Cleanup(func() { desktopEndpointImageFn = original; handoffNowFn = now; handoffSleepFn = sleep })
	for _, tc := range []struct {
		name     string
		started  bool
		paths    []string
		probeErr error
		wantErr  bool
	}{
		{name: "no owner before launch", paths: []string{""}},
		{name: "active target", started: true, paths: []string{target}},
		{name: "new owner ready", started: true, paths: []string{"", target}},
		{name: "old owner preserved", paths: []string{old}, wantErr: true},
		{name: "old owner wins race", started: true, paths: []string{"", old}, wantErr: true},
		{name: "access denied", probeErr: errors.New("access denied"), wantErr: true},
		{name: "timeout", started: true, paths: []string{""}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := time.Unix(0, 0)
			handoffNowFn = func() time.Time { return clock }
			handoffSleepFn = func(d time.Duration) { clock = clock.Add(d) }
			n := 0
			desktopEndpointImageFn = func(got string) (string, error) {
				if got != id {
					t.Fatalf("probed another home %q", got)
				}
				if tc.probeErr != nil {
					return "", tc.probeErr
				}
				i := n
				if i >= len(tc.paths) {
					i = len(tc.paths) - 1
				}
				n++
				return tc.paths[i], nil
			}
			err := verifyDesktopHandoff(root, tc.started)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v wantError=%v", err, tc.wantErr)
			}
		})
	}
}
func TestDesktopHandoffRejectsUnboundIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	for _, id := range []string{"", instanceidentity.ForHome(t.TempDir())} {
		t.Setenv(instanceidentity.UpdateEnvironmentKey, id)
		if err := verifyDesktopHandoff(t.TempDir(), false); err == nil {
			t.Fatal("unbound helper accepted")
		}
	}
}

func TestPublishedRelaunchDoesNotReportSuccessWhenOldOwnerWins(t *testing.T) {
	stubDesktopHandoff(t)
	originalStart := startRelaunchFn
	t.Cleanup(func() { startRelaunchFn = originalStart })
	root := t.TempDir()
	target := filepath.Join(root, "reasonix-desktop.exe")
	if err := os.WriteFile(target, []byte("target"), 0700); err != nil {
		t.Fatal(err)
	}
	started, notified := false, false
	startRelaunchFn = func(string, string) error { started = true; return nil }
	verifyDesktopHandoffFn = func(_ string, after bool) error {
		if after {
			return errors.New("old owner acquired endpoint")
		}
		return nil
	}
	notifyHandoffBlockedFn = func(installed bool) {
		if !installed {
			t.Fatal("lost installed outcome")
		}
		notified = true
	}
	if code := relaunchPublishedInstall(log.New(io.Discard, "", 0), target, root, "restart"); code != 1 || !started || !notified {
		t.Fatalf("code=%d started=%v notified=%v", code, started, notified)
	}
}
