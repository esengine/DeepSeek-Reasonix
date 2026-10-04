package main

import (
	"fmt"
	"io"
	"time"

	"reasonix/internal/assembly/boot"
)

type startupPhase string

const (
	startupPage          startupPhase = "page"
	startupConfigUpgrade startupPhase = "config-upgrade"
	startupConfig        startupPhase = "config"
	startupSinks         startupPhase = "sinks"
	startupWorkspace     startupPhase = "workspace"
	startupRuntime       startupPhase = "runtime"
	startupHub           startupPhase = "hub"
	startupAdopt         startupPhase = "adopt"
	startupBackground    startupPhase = "background"
	startupBind          startupPhase = "bind"
	startupSetup         startupPhase = "provider-setup"
	startupAnnounce      startupPhase = "announce"
)

type startupPhases struct {
	logs io.Writer
	now  func() time.Time
	last time.Time
}

func newStartupPhases(logs io.Writer, now func() time.Time) *startupPhases {
	return &startupPhases{logs: logs, now: now, last: now()}
}

func (p *startupPhases) mark(phase startupPhase) {
	now := p.now()
	p.write(phase, now.Sub(p.last))
	p.last = now
}

func (p *startupPhases) write(phase startupPhase, elapsed time.Duration) {
	fmt.Fprintf(p.logs, "reasonix-studio-host: startup phase=%s elapsed_ms=%d\n", phase, elapsed.Milliseconds())
}

func (p *startupPhases) boot(phase boot.Phase) {
	p.write(startupPhase("boot."+phase.Name), phase.D)
}
