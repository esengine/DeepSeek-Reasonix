//go:build windows

package sandbox

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

var (
	windowsBooksMu sync.Mutex
	windowsBooks   = map[string]*GrantBook{}
)

// windowsBook opens the grant book under stateDir once per process and repairs
// it on the way in: what a crash left half-done is finished before a command runs.
func windowsBook(stateDir string) (*GrantBook, error) {
	windowsBooksMu.Lock()
	defer windowsBooksMu.Unlock()
	if b, ok := windowsBooks[stateDir]; ok {
		return b, nil
	}
	b, err := OpenGrantBook(stateDir, WindowsGrantOps())
	if err != nil {
		return nil, err
	}
	b.Repair()
	windowsBooks[stateDir] = b
	return b, nil
}

// GrantSpec prepares an enforcing spec for the Windows launcher: it grants the
// install's SID on the spec's roots and the toolchain caches that exist, once each,
// and proves a restricted token can be made here. On failure the spec comes back
// unprepared with an ErrWin* identity, so enforce mode refuses instead of running
// unconfined. A spec that does not enforce is returned as it was.
func GrantSpec(spec Spec, stateDir string) Spec {
	if err := RequiredIntegrityFault(spec); err != nil {
		spec.GrantSID, spec.GrantFault = "", err
		return spec
	}
	if spec.Mode != "enforce" {
		return spec
	}
	book, err := windowsBook(stateDir)
	if err != nil {
		spec.GrantFault = fmt.Errorf("%w: %w", ErrWinGrantUnreadable, err)
		return spec
	}
	dirs := planWriteRoots(spec, backendWriteDirs(spec)).dirs
	if exposed, ok := exposedRecord(stateDir, dirs); ok {
		spec.GrantFault = fmt.Errorf("%w: %s", ErrWinRecordExposed, exposed)
		return spec
	}
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		if err := book.Ensure(dir); err != nil {
			spec.GrantFault = fmt.Errorf("%w: %s: %w", ErrWinGrantDenied, dir, err)
			return spec
		}
	}
	if err := probeToken(book.SID()); err != nil {
		spec.GrantFault = err
		return spec
	}
	spec.GrantSID = book.SID()
	return spec
}

// probeToken makes and drops a restricted token, to learn at session start what
// a launch would otherwise learn when it fails.
func probeToken(sid string) error {
	if err := launcherMayRun(); err != nil {
		return fmt.Errorf("%w: %w", ErrWinLauncher, err)
	}
	tok, err := restrictedToken(sid)
	if err != nil {
		if errors.Is(err, errNoLogonSID) {
			return fmt.Errorf("%w: %w", ErrWinNoLogonSID, err)
		}
		return fmt.Errorf("%w: %w", ErrWinLauncher, err)
	}
	tok.Close()
	return nil
}

// windowsCacheDirs are the data caches of the user the spec's environment names.
// Each is granted only if it already exists.
func windowsCacheDirs(spec Spec) []string {
	getenv := EffectiveGetenv(spec.ShellEnv)
	return cacheDirsFor(getenv("LOCALAPPDATA"), getenv("USERPROFILE"))
}

// exposedRecord reports a root that contains the grant record: a command allowed
// to write there could rewrite the record, and the host re-grants what it lists.
func exposedRecord(stateDir string, roots []string) (string, bool) {
	state, _ := resolveWithLinks(stateDir)
	for _, root := range roots {
		if pathWithin(fold(root), fold(state)) {
			return root, true
		}
	}
	return "", false
}
