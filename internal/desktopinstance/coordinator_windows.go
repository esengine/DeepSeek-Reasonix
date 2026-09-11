//go:build windows

package desktopinstance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/installlayout"
	"reasonix/internal/proc"
)

const gracefulTimeout = 20 * time.Second
const terminateTimeout = 10 * time.Second
const startupTimeout = 30 * time.Second

func closeProcesses(list []*process) {
	for _, p := range list {
		p.close()
	}
}
func aliveProcesses(list []*process) []*process {
	var alive []*process
	for _, p := range list {
		if p.alive() {
			alive = append(alive, p)
		}
	}
	return alive
}
func waitProcesses(list []*process, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for len(aliveProcesses(list)) > 0 {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true
}

func requestQuit(p *process, home string) {
	if !p.alive() {
		return
	}
	if p.status == nil {
		closeWindows(p)
		return
	}
	cmd := proc.Command(p.image, QuitRequest)
	cmd.Env = withHome(os.Environ(), home)
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}

func withHome(env []string, home string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		if !strings.EqualFold(key, "REASONIX_HOME") && !strings.EqualFold(key, "REASONIX_DESKTOP_SERVICE") {
			out = append(out, e)
		}
	}
	return append(out, "REASONIX_HOME="+home)
}

func recoverProcesses(root, profile, home string, list []*process, all, interactive bool) error {
	defer closeProcesses(list)
	if len(list) == 0 {
		return nil
	}
	// A whole-install operation may include services and renderers. Only shells
	// with a known data home can receive a targeted lifecycle request.
	for _, p := range list {
		if p.status != nil && p.status.HomeKey == ProfileKey(profile) {
			requestQuit(p, home)
		} else if ImageRole(root, p.image) == "shell" {
			closeWindows(p)
		}
	}
	if waitProcesses(list, gracefulTimeout) {
		return requireVacant(root, profile, all)
	}
	if !interactive {
		return outcome(ConfirmationRequired, "old Reasonix processes have not exited; interactive recovery is required")
	}
	fresh, err := inspect(root, profile, all)
	if err != nil {
		return err
	}
	defer closeProcesses(fresh)
	for _, p := range fresh {
		matched := false
		for _, old := range list {
			if old.pid == p.pid && old.created == p.created {
				matched = true
				break
			}
		}
		if !matched {
			return outcome(ConfirmationRequired, "another process appeared during recovery; retry to inspect it")
		}
	}
	live := aliveProcesses(list)
	// Include only verified descendants, not unrelated instances sharing an exe.
	if !all {
		children, err := inspect(root, profile, true)
		if err != nil {
			return err
		}
		defer closeProcesses(children)
		ids := map[uint32]*process{}
		for _, p := range live {
			ids[p.pid] = p
		}
		for changed := true; changed; {
			changed = false
			for _, p := range children {
				parent := ids[p.parent]
				if ids[p.pid] == nil && parent != nil && parent.alive() && parent.created.Nanoseconds() <= p.created.Nanoseconds() {
					ids[p.pid] = p
					live = append(live, p)
					changed = true
				}
			}
		}
	}
	if len(live) == 0 {
		return nil
	}
	if !confirmProcesses(live) {
		return outcome(Cancelled, "recovery cancelled; existing processes were preserved")
	}
	// Consent covers the displayed identities only, including when the dialog
	// remains open while an old launcher starts another instance.
	latest, err := inspect(root, profile, all)
	if err != nil {
		return err
	}
	defer closeProcesses(latest)
	for _, p := range latest {
		matched := false
		for _, approved := range live {
			if p.pid == approved.pid && p.created == approved.created {
				matched = true
				break
			}
		}
		if !matched {
			return outcome(ConfirmationRequired, "processes changed after confirmation; retry recovery")
		}
	}
	for _, p := range live {
		if err := p.terminate(); err != nil {
			return outcome(UnknownOwner, "could not end verified process %d: %v", p.pid, err)
		}
	}
	if !waitProcesses(live, terminateTimeout) {
		return outcome(ExitTimeout, "old Reasonix processes did not exit")
	}
	return requireVacant(root, profile, all)
}

func requireVacant(root, profile string, all bool) error {
	list, err := inspect(root, profile, all)
	if err != nil {
		return err
	}
	defer closeProcesses(list)
	if len(list) != 0 {
		return outcome(ConfirmationRequired, "another instance appeared during handoff; activation was stopped")
	}
	return nil
}

// CheckInstallVacant must be called while holding PrepareInstall's lease.
func CheckInstallVacant(root, home string) error {
	root, profile, err := preparePaths(root, home)
	if err != nil {
		return err
	}
	return requireVacant(root, profile, true)
}

// PrepareInstall holds process coordination until the caller commits or aborts.
func PrepareInstall(root, home string, interactive bool) (release func(), resultErr error) {
	finish := attemptLog(home, "prepare-install", root)
	defer func() { finish(resultErr) }()
	root, profile, err := preparePaths(root, home)
	if err != nil {
		return nil, err
	}
	unlock, err := lockInstall(root)
	if err != nil {
		return nil, err
	}
	list, err := inspect(root, profile, true)
	if err == nil {
		err = recoverProcesses(root, profile, home, list, true, interactive)
	}
	if err != nil {
		unlock()
		return nil, err
	}
	return unlock, nil
}

func target(root string) (string, string, error) {
	current, err := installlayout.ReadCurrent(root)
	if err != nil {
		return "", "", err
	}
	desktop, err := installlayout.ActiveDesktopPath(root)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(filepath.Dir(desktop), "app", "Reasonix.exe"), current.ActiveVersion, nil
}

func verify(root, profile, expected string) error {
	expectedImage := ""
	if expected != "" {
		var err error
		expectedImage, _, err = target(root)
		if err != nil {
			return err
		}
		expectedImage, err = canonical(expectedImage)
		if err != nil {
			return err
		}
	}
	deadline := time.Now().Add(startupTimeout)
	for {
		list, err := inspect(root, profile, false)
		if err != nil {
			return err
		}
		ready := false
		failed := false
		for _, p := range list {
			if p.status != nil {
				if p.status.Ready(expected) && (expectedImage == "" || strings.EqualFold(p.image, expectedImage)) {
					child, childErr := openProcess(p.status.ServicePID, 0)
					if childErr == nil {
						// A status response alone cannot prove that its advertised
						// service is still alive or belongs to this release.
						serviceImage := filepath.Join(filepath.Dir(p.image), "resources", "service", "reasonix-desktop.exe")
						entryImage := filepath.Join(filepath.Dir(filepath.Dir(p.image)), "reasonix-desktop.exe")
						ready = ready || strings.EqualFold(child.image, serviceImage) || strings.EqualFold(child.image, entryImage)
						child.close()
					}
				}
				failed = failed || p.status.Lifecycle == "failed"
			}
		}
		closeProcesses(list)
		if ready {
			return nil
		}
		if failed {
			return outcome(StartupFailed, "Reasonix startup failed; use the recovery window or desktop-shell/logs")
		}
		if time.Now().After(deadline) {
			return outcome(StartupFailed, "startup was not verified within 30 seconds; inspect desktop-shell/logs")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// LaunchAndVerify serializes handoff and never treats launcher exit as readiness.
func LaunchAndVerify(root, home string, interactive bool, start func() error, args ...string) (resultErr error) {
	finish := attemptLog(home, "launch", root)
	defer func() { finish(resultErr) }()
	root, profile, err := preparePaths(root, home)
	if err != nil {
		return err
	}
	unlock, err := lockInstall(root)
	if err != nil {
		return err
	}
	defer unlock()
	list, err := inspect(root, profile, false)
	if err != nil {
		return err
	}
	for _, p := range list {
		writeRecoveryLog(home, fmt.Sprintf("inspect pid=%d created=%d image=%q status=%+v", p.pid, p.created.Nanoseconds(), p.image, p.status))
	}
	for _, p := range list {
		if p.status == nil && p.legacyProfile != "" && focusLegacyWindow(p) {
			// A legacy instance cannot prove readiness. Showing its existing
			// window handles an ordinary double-click without interrupting work;
			// update acceptance still uses VerifyCurrent's strict checks.
			writeRecoveryLog(home, "existing legacy window displayed; health remains unverified")
			closeProcesses(list)
			return nil
		}
	}
	for _, p := range list {
		if p.status != nil && p.status.Lifecycle == "failed" {
			// A responsive failed shell owns its recovery UI and may still hold
			// an unsaved renderer. Let the user choose retry/exit there.
			cmd := proc.Command(p.image, args...)
			cmd.Env = withHome(os.Environ(), home)
			err := cmd.Start()
			if err == nil {
				go func() { _ = cmd.Wait() }()
			}
			closeProcesses(list)
			writeRecoveryLog(home, "existing recovery window requested; health remains unverified")
			return err
		}
		if p.status != nil && p.status.Lifecycle == "ready" && p.status.Service == "ready" {
			// Launch the running shell itself, preserving its existing service and drafts.
			cmd := proc.Command(p.image, args...)
			cmd.Env = withHome(os.Environ(), home)
			err := cmd.Start()
			if err == nil {
				go func() { _ = cmd.Wait() }()
			}
			closeProcesses(list)
			if err != nil {
				return err
			}
			return verify(root, profile, "")
		}
	}
	waiting := len(list) > 0
	for _, p := range list {
		if p.status == nil || (p.status.Lifecycle != "starting" && p.status.Lifecycle != "quitting" && p.status.Lifecycle != "done") {
			waiting = false
		}
	}
	if waiting {
		closeProcesses(list)
		deadline := time.Now().Add(startupTimeout)
		for {
			list, err = inspect(root, profile, false)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				break
			}
			ready := false
			for _, p := range list {
				ready = ready || (p.status != nil && p.status.Ready(""))
			}
			if ready {
				closeProcesses(list)
				return nil
			}
			if time.Now().After(deadline) {
				closeProcesses(list)
				return outcome(StartupFailed, "the existing instance is still starting or exiting")
			}
			closeProcesses(list)
			time.Sleep(200 * time.Millisecond)
		}
	}
	if err := recoverProcesses(root, profile, home, list, false, interactive); err != nil {
		return err
	}
	_, version, err := target(root)
	if err != nil {
		return fmt.Errorf("resolve current release: %w", err)
	}
	if err := start(); err != nil {
		return err
	}
	return verify(root, profile, version)
}

// VerifyCurrent is used after activation when the stable launcher owns recovery.
func VerifyCurrent(root, home string) error {
	root, profile, err := preparePaths(root, home)
	if err != nil {
		return err
	}
	_, version, err := target(root)
	if err != nil {
		return err
	}
	return verify(root, profile, version)
}
