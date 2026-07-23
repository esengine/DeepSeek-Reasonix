package protocol

import "strings"

type NoResult struct{}

type RuntimeTarget struct {
	WorkspaceID WorkspaceID `json:"workspaceId"`
	SessionID   SessionID   `json:"sessionId"`
}

func (t RuntimeTarget) Validate() error {
	if strings.TrimSpace(string(t.WorkspaceID)) == "" || strings.TrimSpace(string(t.SessionID)) == "" {
		return validationError("workspaceId and sessionId must be non-empty opaque strings")
	}
	return nil
}

type HostMutation struct {
	RequestID         RequestID `json:"requestId"`
	ExpectedHostEpoch HostEpoch `json:"expectedHostEpoch"`
}

type SessionMutation struct {
	RequestID            RequestID     `json:"requestId"`
	ExpectedHostEpoch    HostEpoch     `json:"expectedHostEpoch"`
	Target               RuntimeTarget `json:"target"`
	ExpectedRuntimeEpoch RuntimeEpoch  `json:"expectedRuntimeEpoch"`
}

type SessionRecordMutation struct {
	RequestID         RequestID     `json:"requestId"`
	ExpectedHostEpoch HostEpoch     `json:"expectedHostEpoch"`
	Target            RuntimeTarget `json:"target"`
}

type RuntimeQuery struct {
	ExpectedHostEpoch    HostEpoch     `json:"expectedHostEpoch"`
	Target               RuntimeTarget `json:"target"`
	ExpectedRuntimeEpoch RuntimeEpoch  `json:"expectedRuntimeEpoch"`
}

type InitializeParams struct {
	BuildID          BuildID          `json:"buildId"`
	ClientInstanceID ClientInstanceID `json:"clientInstanceId"`
	ResumeLeaseID    LeaseID          `json:"resumeLeaseId,omitempty"`
	Workspace        string           `json:"workspace" validate:"nonempty"`
}

type LeaseInfo struct {
	LeaseID        LeaseID `json:"leaseId"`
	TTLMillis      int     `json:"ttlMs" validate:"min=1"`
	PingIntervalMs int     `json:"pingIntervalMs" validate:"min=1"`
}

type HostInfo struct {
	OS             string `json:"os" validate:"nonempty"`
	Arch           string `json:"arch" validate:"nonempty"`
	ShellKind      string `json:"shellKind" validate:"nonempty"`
	SandboxBackend string `json:"sandboxBackend" validate:"nonempty"`
}

type InitializeResult struct {
	BuildID      BuildID      `json:"buildId"`
	HostEpoch    HostEpoch    `json:"hostEpoch"`
	Lease        LeaseInfo    `json:"lease"`
	Host         HostInfo     `json:"host"`
	Capabilities Capabilities `json:"capabilities"`
}

type PingParams struct {
	LeaseID LeaseID `json:"leaseId"`
}

type PingResult struct {
	HostEpoch HostEpoch `json:"hostEpoch"`
	LeaseTTL  int       `json:"leaseTtlMs" validate:"min=1"`
}

type DetachParams struct {
	LeaseID LeaseID `json:"leaseId"`
}

type DetachResult struct {
	Detached bool `json:"detached" validate:"true"`
}

type HostCapabilitiesParams struct {
	ExpectedHostEpoch HostEpoch `json:"expectedHostEpoch"`
}

type HostCapabilitiesResult struct {
	HostEpoch    HostEpoch    `json:"hostEpoch"`
	Capabilities Capabilities `json:"capabilities"`
}

type WorkspaceBrowseParams struct {
	ExpectedHostEpoch HostEpoch    `json:"expectedHostEpoch"`
	DirectoryRef      DirectoryRef `json:"directoryRef,omitempty"`
	TypedPath         string       `json:"typedPath,omitempty"`
	Cursor            Cursor       `json:"cursor,omitempty"`
	Limit             *int         `json:"limit,omitempty" validate:"min=1,max=1000"`
}

func (p WorkspaceBrowseParams) Validate() error {
	if p.DirectoryRef != "" && p.TypedPath != "" {
		return validationError("directoryRef and typedPath are mutually exclusive")
	}
	return nil
}

type DirectoryItem struct {
	DirectoryRef DirectoryRef `json:"directoryRef"`
	Name         string       `json:"name" validate:"nonempty"`
	DisplayPath  string       `json:"displayPath" validate:"nonempty"`
	ParentRef    DirectoryRef `json:"parentRef,omitempty"`
}

type WorkspaceBrowseResult struct {
	Directory  DirectoryItem   `json:"directory"`
	Entries    []DirectoryItem `json:"entries"`
	HasMore    bool            `json:"hasMore"`
	NextCursor Cursor          `json:"nextCursor,omitempty"`
}

type WorkspaceOpenParams struct {
	HostMutation
	PrimaryDirectoryRef DirectoryRef `json:"primaryDirectoryRef"`
}

type WorkspaceSummary struct {
	WorkspaceID WorkspaceID `json:"workspaceId"`
	Name        string      `json:"name" validate:"nonempty"`
	DisplayPath string      `json:"displayPath" validate:"nonempty"`
}

type WorkspaceOpenDisposition string

const (
	WorkspaceOpened      WorkspaceOpenDisposition = "opened"
	WorkspaceAlreadyOpen WorkspaceOpenDisposition = "already_open"
)

type WorkspaceOpenResult struct {
	Workspace   WorkspaceSummary         `json:"workspace"`
	Disposition WorkspaceOpenDisposition `json:"disposition"`
}

type WorkspaceListParams struct {
	ExpectedHostEpoch HostEpoch `json:"expectedHostEpoch"`
	Cursor            Cursor    `json:"cursor,omitempty"`
	Limit             *int      `json:"limit,omitempty" validate:"min=1,max=1000"`
}

type WorkspaceListResult struct {
	Items      []WorkspaceSummary `json:"items"`
	HasMore    bool               `json:"hasMore"`
	NextCursor Cursor             `json:"nextCursor,omitempty"`
}

type WorkspaceCloseParams struct {
	HostMutation
	WorkspaceID WorkspaceID `json:"workspaceId"`
}

type WorkspaceCloseDisposition string

const (
	WorkspaceClosed        WorkspaceCloseDisposition = "closed"
	WorkspaceAlreadyClosed WorkspaceCloseDisposition = "already_closed"
)

type WorkspaceCloseResult struct {
	Disposition WorkspaceCloseDisposition `json:"disposition"`
}

type WorkspaceCatalogParams struct {
	ExpectedHostEpoch HostEpoch   `json:"expectedHostEpoch"`
	WorkspaceID       WorkspaceID `json:"workspaceId"`
}

type EffortCatalog struct {
	Supported bool     `json:"supported"`
	Default   string   `json:"default,omitempty"`
	Levels    []string `json:"levels"`
}

type ModelCatalogItem struct {
	Ref      ModelRef      `json:"ref"`
	Provider string        `json:"provider" validate:"nonempty"`
	Model    string        `json:"model" validate:"nonempty"`
	Effort   EffortCatalog `json:"effort"`
}

type ResolvedProfile struct {
	Model             string            `json:"model" validate:"nonempty"`
	Effort            string            `json:"effort" validate:"nonempty"`
	CollaborationMode CollaborationMode `json:"collaborationMode"`
	TokenMode         TokenMode         `json:"tokenMode"`
	ToolApprovalMode  ToolApprovalMode  `json:"toolApprovalMode"`
}

type WorkspaceCatalogResult struct {
	Revision           CatalogRevision     `json:"revision"`
	Models             []ModelCatalogItem  `json:"models"`
	CollaborationModes []CollaborationMode `json:"collaborationModes"`
	TokenModes         []TokenMode         `json:"tokenModes"`
	ToolApprovalModes  []ToolApprovalMode  `json:"toolApprovalModes"`
	DefaultProfile     ResolvedProfile     `json:"defaultProfile"`
}

type HostConfigSummaryParams struct {
	ExpectedHostEpoch HostEpoch `json:"expectedHostEpoch"`
}

type EffectiveScope struct {
	Name   string `json:"name" validate:"nonempty"`
	Active bool   `json:"active"`
}

type ConfigDisplayPath struct {
	Scope       string `json:"scope" validate:"nonempty"`
	DisplayPath string `json:"displayPath" validate:"nonempty"`
}

type FeatureState struct {
	Feature   string `json:"feature" validate:"nonempty"`
	Available bool   `json:"available"`
	Summary   string `json:"summary,omitempty"`
}

type CLIHint struct {
	Label   string `json:"label" validate:"nonempty"`
	Command string `json:"command" validate:"controlledCommand"`
}

type HostConfigSummaryResult struct {
	Revision        CatalogRevision     `json:"revision"`
	EffectiveScopes []EffectiveScope    `json:"effectiveScopes"`
	DisplayPaths    []ConfigDisplayPath `json:"displayPaths"`
	FeatureStates   []FeatureState      `json:"featureStates"`
	CLIHints        []CLIHint           `json:"cliHints"`
}
