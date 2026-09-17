package shellrun

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

const shellProbeMarker = "REASONIX_SHELL_CHILD_READY"

// WindowsProbeArgv builds a harmless shell and child-process check using the
// actual policy and session temp. Host discovery alone cannot establish that
// MSYS or PowerShell can initialize under the restricted token.
func WindowsProbeArgv(spec sandbox.Spec, sh sandbox.Shell, sessionTemp string) []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	p := sandbox.PrepareShell(spec, sh, shellProbeCommand(sh), sessionTemp)
	if spec.Enforce() && !p.Wrapped {
		// Never probe outside a requested sandbox when its helper is unavailable.
		return []string{}
	}
	return p.Argv
}

func shellProbeCommand(sh sandbox.Shell) string {
	if sh.Kind == sandbox.ShellPowerShell {
		path := strings.ReplaceAll(sh.Path, "'", "''")
		return "& '" + path + "' -NoLogo -NoProfile -NonInteractive -Command 'exit 0'; if ($LASTEXITCODE -ne 0) { exit 1 }; [Console]::WriteLine('" + shellProbeMarker + "')"
	}
	path := strings.ReplaceAll(strings.ReplaceAll(sh.Path, "\\", "/"), "'", "'\"'\"'")
	return "'" + path + "' -c 'exit 0' && printf '%s\\n' '" + shellProbeMarker + "'"
}

type probeFailure struct {
	at     time.Time
	result Result
}

// Cache failures only, briefly: retries of a different user command cannot
// repair shell initialization. Changes to policy, environment, cwd or temp
// produce a new key; successes are always checked against the current policy.
type probeFailures struct {
	mu      sync.Mutex
	entries map[[32]byte]probeFailure
}

var windowsProbeFailures probeFailures

// CheckShellLaunch never executes the user's command. A failure therefore has
// preflight/not-run semantics, even if the diagnostic child itself started.
// ProbeArgv nil means this platform/caller has no preflight requirement.
func CheckShellLaunch(ctx context.Context, req Request) *Result {
	return windowsProbeFailures.check(ctx, req)
}

func (c *probeFailures) check(ctx context.Context, req Request) *Result {
	if req.ProbeArgv == nil || ctx.Err() != nil {
		return nil
	}
	if req.Env == nil {
		req.Env = os.Environ()
	}
	probeTimeout := 10 * time.Second
	if req.Timeout > 0 && req.Timeout < probeTimeout {
		probeTimeout = req.Timeout
	}
	encoded, _ := json.Marshal([]any{req.ProbeArgv, req.Dir, req.Env, probeTimeout})
	key := sha256.Sum256(encoded)
	c.mu.Lock()
	entry, ok := c.entries[key]
	c.mu.Unlock()
	if ok && time.Since(entry.at) < 30*time.Second {
		result := entry.result
		return &result
	}
	probe := req
	probe.Argv, probe.ProbeArgv = req.ProbeArgv, nil
	probe.CommandPreview = "shell startup and child-process preflight"
	probe.Progress = nil
	probe.Timeout = probeTimeout
	probe.Track = false
	probe.PreserveWaitDelay = false
	r := RunForeground(ctx, probe)
	if r.Err == nil && strings.Contains(r.Combined, shellProbeMarker) {
		return nil
	}
	// Do not retain process handles in a diagnostic cache.
	r.Cmd, r.Tracked = nil, nil
	if len(r.Combined) > tool.OutputTailMaxBytes {
		r.Combined = string(completeTail([]byte(r.Combined[len(r.Combined)-tool.OutputTailMaxBytes:])))
	}
	r.OutputTail = r.Combined
	r.Started = false
	if ctx.Err() != nil {
		return &r
	}
	detail := "readiness marker missing"
	if r.Err != nil {
		detail = r.Err.Error()
	}
	r.State, r.FailurePhase = tool.ShellStateNotRun, tool.ShellPhasePreflight
	r.Err = fmt.Errorf("shell startup/child-process check failed; requested command was not run: %s. Do not retry commands through this shell until its configuration or execution environment is repaired", detail)
	c.mu.Lock()
	if c.entries == nil || len(c.entries) >= 64 {
		c.entries = make(map[[32]byte]probeFailure)
	}
	c.entries[key] = probeFailure{at: time.Now(), result: r}
	c.mu.Unlock()
	return &r
}
