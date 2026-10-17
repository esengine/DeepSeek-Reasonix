package hook

import "reasonix/internal/contract/config"

type Snapshot struct {
	Hook          ResolvedHook           `json:"hook"`
	Approval      *config.ProjectProgram `json:"approval,omitempty"`
	Async         bool                   `json:"async,omitempty"`
	PayloadFormat string                 `json:"payloadFormat,omitempty"`
	Argv          []string               `json:"argv,omitempty"`
	ExecutionMode ExecutionMode          `json:"executionMode,omitempty"`
	Shell         string                 `json:"shell,omitempty"`
}

func (h ResolvedHook) Snapshot() Snapshot {
	return Snapshot{Hook: h, Approval: h.approval, Async: h.Async, PayloadFormat: h.PayloadFormat, Argv: h.Argv, ExecutionMode: h.ExecutionMode, Shell: h.Shell}
}

func (s Snapshot) Restore() ResolvedHook {
	h := s.Hook
	h.approval, h.Async, h.PayloadFormat = s.Approval, s.Async, s.PayloadFormat
	h.Argv, h.ExecutionMode, h.Shell = s.Argv, s.ExecutionMode, s.Shell
	return h
}
