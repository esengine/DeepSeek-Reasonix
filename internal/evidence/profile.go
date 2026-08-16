package evidence

import "strings"

// EffectReason is a privacy-safe, argument-free classification note.
type EffectReason string

const (
	ReasonReadOnly        EffectReason = "read_only"
	ReasonWorkspaceWrite  EffectReason = "workspace_write"
	ReasonRepoMetadata    EffectReason = "repository_metadata"
	ReasonHostState       EffectReason = "host_state"
	ReasonExternalState   EffectReason = "external_state"
	ReasonOpaqueWriter    EffectReason = "opaque_writer"
	ReasonDestructive     EffectReason = "destructive"
	ReasonUnknown         EffectReason = "unknown"
	ReasonHintReadOnly    EffectReason = "annotation_read_only"
	ReasonHintDestructive EffectReason = "annotation_destructive"
)

// TargetKind classifies one concrete action target.
type TargetKind uint8

const (
	TargetUnspecified TargetKind = iota
	TargetFile
	TargetDirectory
	TargetRepo
	TargetHost
	TargetExternal
)

// Target is one resolved path or state domain the call touches.
type Target struct {
	Path string
	Kind TargetKind
}

// TargetKey is a comparable identity for an action target.
type TargetKey string

// Key returns a stable identity. Empty paths collapse by kind only.
func (t Target) Key() TargetKey {
	path := strings.TrimSpace(t.Path)
	if path == "" {
		return TargetKey(targetKindName(t.Kind))
	}
	return TargetKey(targetKindName(t.Kind) + ":" + normalizeRiskPath(path))
}

func targetKindName(k TargetKind) string {
	switch k {
	case TargetFile:
		return "file"
	case TargetDirectory:
		return "dir"
	case TargetRepo:
		return "repo"
	case TargetHost:
		return "host"
	case TargetExternal:
		return "external"
	default:
		return "target"
	}
}

// EffectProfile is the host's concrete classification of one tool invocation.
type EffectProfile struct {
	Known          bool
	ReadOnly       bool
	WorkspaceWrite bool
	RepoMetadata   bool
	HostState      bool
	ExternalState  bool
	Destructive    bool
	Irreversible   bool
	Privileged     bool
	ExecutesCode   bool
	UsesNetwork    bool
	Targets        []Target
	Reason         EffectReason
}

// Clone copies Targets so callers cannot share a mutable slice.
func (p EffectProfile) Clone() EffectProfile {
	if len(p.Targets) > 0 {
		p.Targets = append([]Target(nil), p.Targets...)
	}
	return p
}

// MutatesState reports whether the call can change durable state.
func (p EffectProfile) MutatesState() bool {
	if !p.Known {
		return true
	}
	return p.WorkspaceWrite || p.RepoMetadata || p.HostState || p.ExternalState
}

// OpaqueWriter reports a write-capable call whose effects are not proven.
func (p EffectProfile) OpaqueWriter() bool {
	return !p.Known && !p.ReadOnly
}

// TargetKeys copies comparable identities for the classified targets.
func (p EffectProfile) TargetKeys() []TargetKey {
	if len(p.Targets) == 0 {
		return nil
	}
	out := make([]TargetKey, len(p.Targets))
	for i, t := range p.Targets {
		out[i] = t.Key()
	}
	return out
}

// ToolEffects projects the profile onto the existing policy/evidence boundary.
func (p EffectProfile) ToolEffects() ToolEffects {
	return ToolEffects{
		StateMutation:      p.MutatesState(),
		WorkspaceMutation:  !p.Known || p.WorkspaceWrite || p.RepoMetadata,
		ContentMutation:    !p.Known || p.WorkspaceWrite,
		RepositoryMutation: p.RepoMetadata,
		Known:              p.Known,
		Reason:             p.displayReason(),
	}
}

func (p EffectProfile) displayReason() string {
	switch p.Reason {
	case ReasonRepoMetadata:
		return "repository metadata write"
	case ReasonWorkspaceWrite:
		return "workspace content write"
	case ReasonHostState:
		return "host state write"
	case ReasonExternalState:
		return "external state write"
	case ReasonOpaqueWriter:
		return "opaque writer"
	case ReasonDestructive:
		return "destructive write"
	case ReasonUnknown:
		return "unknown command"
	default:
		return string(p.Reason)
	}
}

// CallHint is the optional host-only preview collected before classification.
type CallHint struct {
	Present      bool
	Known        bool
	ReadOnly     bool
	Destructive  bool
	Privileged   bool
	UsesNetwork  bool
	ExecutesCode bool
	Targets      []string
}

// EffectInput is one concrete invocation to classify.
type EffectInput struct {
	ToolName       string
	Args           []byte
	StaticReadOnly bool
	Hint           CallHint
	ActualPaths    []string
}
