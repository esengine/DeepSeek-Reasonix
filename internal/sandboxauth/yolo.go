package sandboxauth

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
)

// YOLOAcknowledgementStatus is the transport-neutral project-expansion state.
type YOLOAcknowledgementStatus string

const (
	// YOLONotRequired means configuration does not expand project-local authority.
	YOLONotRequired YOLOAcknowledgementStatus = "not_required"
	// YOLORequired means interactive auto-approval is paused for acknowledgement.
	YOLORequired YOLOAcknowledgementStatus = "required"
	// YOLOAccepted means the user accepted this workspace/session expansion.
	YOLOAccepted YOLOAcknowledgementStatus = "accepted"
	// YOLORefused means the user refused this workspace/session expansion.
	YOLORefused YOLOAcknowledgementStatus = "refused"
)

// YOLOPolicyConfig is the resolved configuration and its security provenance.
type YOLOPolicyConfig struct {
	Workspace        string
	Effective        bool
	ProjectExpansion bool
}

// YOLOPolicyState is safe for frontends to query without re-deriving policy.
type YOLOPolicyState struct {
	Workspace       string                    `json:"workspace"`
	Effective       bool                      `json:"effective"`
	YOLO            bool                      `json:"yolo"`
	Interactive     bool                      `json:"interactive"`
	Acknowledgement YOLOAcknowledgementStatus `json:"acknowledgement"`
	Warnings        []Warning                 `json:"warnings,omitempty"`
}

// YOLOSessionState is ephemeral state carried across same-session rebuilds.
// StartupWarningDelivered is non-authority delivery bookkeeping; callers must not
// persist this value to transcripts, configuration, or history.
type YOLOSessionState struct {
	Workspace               string
	Acknowledgement         YOLOAcknowledgementStatus
	StartupWarningDelivered bool
}

// YOLOPolicy owns session-local acknowledgement and capability auto-once policy.
type YOLOPolicy struct {
	mu sync.RWMutex

	workspace               string
	effective               bool
	projectExpansion        bool
	yolo                    bool
	interactive             bool
	ack                     YOLOAcknowledgementStatus
	startupWarning          bool
	startupWarningDelivered bool
}

// NewYOLOPolicy creates policy state for one workspace/session.
func NewYOLOPolicy(cfg YOLOPolicyConfig) *YOLOPolicy {
	p := &YOLOPolicy{}
	p.Configure(cfg)
	return p
}

// Configure applies freshly resolved config. A newly relevant project
// expansion requires a fresh same-session acknowledgement.
func (p *YOLOPolicy) Configure(cfg YOLOPolicyConfig) {
	workspace := canonicalWorkspace(cfg.Workspace)
	p.mu.Lock()
	defer p.mu.Unlock()
	changedWorkspace := p.workspace != "" && p.workspace != workspace
	becameExpansion := cfg.ProjectExpansion && (!p.projectExpansion || changedWorkspace)
	p.workspace = workspace
	p.effective = cfg.Effective
	p.projectExpansion = cfg.ProjectExpansion
	if !cfg.ProjectExpansion {
		p.ack = YOLONotRequired
	} else if becameExpansion || p.ack == "" || changedWorkspace {
		p.ack = YOLORequired
	}
	if becameExpansion || changedWorkspace {
		p.startupWarningDelivered = false
	}
	if !cfg.ProjectExpansion || !cfg.Effective {
		p.startupWarning = false
		p.startupWarningDelivered = false
	} else if p.yolo && !p.interactive && (becameExpansion || changedWorkspace) {
		p.startupWarning = true
	}
}

// SetRuntimeMode updates the ordinary YOLO posture and interaction capability.
func (p *YOLOPolicy) SetRuntimeMode(yolo, interactive bool) {
	p.mu.Lock()
	enteringHeadlessYOLO := yolo && !interactive && (!p.yolo || p.interactive)
	p.yolo = yolo
	p.interactive = interactive
	if enteringHeadlessYOLO && p.effective && p.projectExpansion && !p.startupWarningDelivered {
		p.startupWarning = true
	}
	p.mu.Unlock()
}

// State returns the typed policy snapshot and any mandatory startup warning.
func (p *YOLOPolicy) State() YOLOPolicyState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	state := YOLOPolicyState{Workspace: p.workspace, Effective: p.effective, YOLO: p.yolo, Interactive: p.interactive, Acknowledgement: p.ack}
	if p.yolo && p.effective && p.projectExpansion && !p.interactive {
		state.Warnings = []Warning{projectExpansionWarning()}
	}
	return state
}

// TakeStartupWarnings consumes warnings that became mandatory on headless YOLO entry.
func (p *YOLOPolicy) TakeStartupWarnings() []Warning {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.startupWarning {
		return nil
	}
	p.startupWarning = false
	p.startupWarningDelivered = true
	return []Warning{projectExpansionWarning()}
}

// Acknowledge accepts or refuses project-expanded capability auto-approval.
func (p *YOLOPolicy) Acknowledge(accept bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.projectExpansion || p.ack != YOLORequired {
		return false
	}
	if accept {
		p.ack = YOLOAccepted
	} else {
		p.ack = YOLORefused
	}
	return true
}

// SessionState snapshots same-session authority and non-authority warning
// delivery state. It is never transcript state.
func (p *YOLOPolicy) SessionState() YOLOSessionState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return YOLOSessionState{Workspace: p.workspace, Acknowledgement: p.ack, StartupWarningDelivered: p.startupWarningDelivered}
}

// RestoreSessionState carries ephemeral state only for the same workspace.
func (p *YOLOPolicy) RestoreSessionState(state YOLOSessionState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.workspace != state.Workspace || !p.projectExpansion {
		return
	}
	if state.Acknowledgement == YOLOAccepted || state.Acknowledgement == YOLORefused {
		p.ack = state.Acknowledgement
	}
	if state.StartupWarningDelivered {
		p.startupWarningDelivered = true
		p.startupWarning = false
	}
}

// ClearSessionState expires acknowledgement on new, clear, and resume.
func (p *YOLOPolicy) ClearSessionState() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.projectExpansion {
		p.ack = YOLORequired
	} else {
		p.ack = YOLONotRequired
	}
	p.startupWarningDelivered = false
	p.startupWarning = p.yolo && !p.interactive && p.effective && p.projectExpansion
}

// DecideSandboxCapabilityAutoOnce implements AutoOncePolicy.
func (p *YOLOPolicy) DecideSandboxCapabilityAutoOnce(context.Context, Request) AutoOnceDecision {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.yolo {
		return AutoOnceDecision{Action: AutoOnceDefer}
	}
	if !p.effective {
		return AutoOnceDecision{Action: AutoOnceRunSandboxed, Diagnostic: "YOLO sandbox capability auto-approval is disabled; requested capabilities were not applied"}
	}
	if p.projectExpansion && p.interactive && p.ack != YOLOAccepted {
		status := "requires acknowledgement"
		if p.ack == YOLORefused {
			status = "was refused"
		}
		return AutoOnceDecision{Action: AutoOnceRunSandboxed, Diagnostic: "YOLO sandbox capability auto-approval " + status + "; requested capabilities were not applied"}
	}
	decision := AutoOnceDecision{Action: AutoOnceAllow}
	return decision
}

func projectExpansionWarning() Warning {
	return Warning{Code: "yolo_project_capability_expansion", Message: "project configuration enables YOLO sandbox capability auto-approval; headless execution will apply requested capability expansions for the current invocation", Mandatory: true}
}

func canonicalWorkspace(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return filepath.Clean(workspace)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return filepath.Clean(abs)
}
