package cli

import (
	"context"
	"strings"

	"reasonix/internal/control"
)

// tuiSessionReclaimedMsg tells the live TUI that the takeover mirror has
// yielded. It is deliberately separate from tuiShutdownMsg: reclaiming one
// session must not terminate the process that may still host other sessions.
type tuiSessionReclaimedMsg struct{}

const sessionReclaimedNotice = "this session was taken back by the remote side; /resume switches to another session, /takeover takes it back, /quit exits"

func (m *chatTUI) handleSessionReclaimed() {
	if m == nil || m.sessionReclaimed {
		return
	}
	m.rememberReclaimedTarget()
	if current, ok := m.ctrl.(*control.Controller); ok {
		m.releaseCanonicalRuntime(current)
	}
	m.sessionReclaimed = true
	m.resetComposerInput()
	// Keep the reclaimed conversation rendered and the process alive: the
	// notice explains the read-only state and the input gate accepts only the
	// switch/takeover/exit commands, so the user chooses the next step instead
	// of being thrown into the session chooser.
	m.notice(sessionReclaimedNotice)
}

func (m *chatTUI) rememberReclaimedTarget() {
	if m == nil || m.ctrl == nil {
		return
	}
	if identity, ok := m.ctrl.(control.IdentityLifecycle); ok {
		if ref, bound := identity.SessionRef(); bound {
			m.reclaimedTarget = cliResumeTarget{ref: ref}
			return
		}
	}
	if path := strings.TrimSpace(m.ctrl.SessionPath()); path != "" {
		m.reclaimedTarget = cliResumeTarget{path: path}
	}
}

func (m *chatTUI) releaseCanonicalRuntime(current *control.Controller) {
	if current == nil {
		return
	}
	service, runtime, exclusive := current.SessionBinding()
	if !exclusive || service == nil || runtime == nil {
		return
	}
	ref := runtime.Ref()
	release := func(candidate *control.Controller) {
		if candidate == nil {
			return
		}
		candidateService, candidateRuntime, candidateExclusive := candidate.SessionBinding()
		if candidateExclusive && candidateService == service && candidateRuntime == runtime {
			if err := candidate.ReleaseSessionRuntimeBinding(); err != nil {
				m.notice("session reclaim: release runtime binding: " + err.Error())
			}
		}
	}

	// Rebuilt controllers can retain a client binding until process teardown.
	// Drop every binding for this exact runtime before asking the service to
	// close it, otherwise its writer lock would remain held by an old model.
	release(current)
	for _, old := range m.oldControllers {
		if candidate, ok := old.(*control.Controller); ok {
			release(candidate)
		}
	}
	if err := service.Close(context.Background(), ref); err != nil {
		m.notice("session reclaim: release writer: " + err.Error())
	}
}

func (m *chatTUI) resumeAfterReclaim() {
	if m == nil {
		return
	}
	m.sessionReclaimed = false
	m.reclaimedTarget = cliResumeTarget{}
	if m.takeover != nil {
		m.takeover.ResumeAfterYield()
	}
}

func reclaimInputAllowed(line string) bool {
	command := strings.TrimSpace(strings.SplitN(line, " ", 2)[0])
	switch command {
	case "/resume", "/takeover", "/quit", "/exit":
		return true
	default:
		return false
	}
}
