package main

// TabMeta is the frontend-facing shape of one tab.
type TabMeta struct {
	ID               string        `json:"id"`
	Scope            string        `json:"scope"`
	WorkspaceRoot    string        `json:"workspaceRoot"`
	WorkspaceName    string        `json:"workspaceName"`
	WorkspacePath    string        `json:"workspacePath,omitempty"`
	GitBranch        string        `json:"gitBranch,omitempty"`
	IsolatedWorktree bool          `json:"isolatedWorktree,omitempty"`
	Remote           *RemoteTabRef `json:"remote,omitempty"`
	// RemoteState seeds restored remote shells before their first state event.
	RemoteState       string             `json:"remoteState,omitempty"`
	TopicID           string             `json:"topicId"`
	TopicTitle        string             `json:"topicTitle"`
	SessionPath       string             `json:"sessionPath,omitempty"`
	SessionRevision   int64              `json:"sessionRevision,omitempty"`
	SessionDigest     string             `json:"sessionDigest,omitempty"`
	SessionGeneration uint64             `json:"sessionGeneration,omitempty"`
	ReadOnly          bool               `json:"readOnly,omitempty"`
	ProjectColor      string             `json:"projectColor,omitempty"`
	Label             string             `json:"label"`
	Ready             bool               `json:"ready"`
	Runtime           SessionRuntimeView `json:"runtime"`
	Running           bool               `json:"running"`
	TurnStartedAt     int64              `json:"turnStartedAt,omitempty"`
	PendingPrompt     bool               `json:"pendingPrompt,omitempty"`
	RemoteControlled  bool               `json:"remoteControlled,omitempty"`
	BackgroundJobs    int                `json:"backgroundJobs,omitempty"`
	CancelRequested   bool               `json:"cancelRequested,omitempty"`
	Cancellable       bool               `json:"cancellable"`
	TurnID            string             `json:"turnId,omitempty"`
	TurnStatus        string             `json:"turnStatus,omitempty"`
	TurnEventSeq      uint64             `json:"turnEventSeq,omitempty"`
	TurnReplayAfter   uint64             `json:"turnReplayAfterSeq,omitempty"`
	Mode              string             `json:"mode"`
	CollaborationMode string             `json:"collaborationMode"`
	ToolApprovalMode  string             `json:"toolApprovalMode"`
	TokenMode         string             `json:"tokenMode"`
	AgentPreset       string             `json:"agentPreset,omitempty"`
	QualityFloor      string             `json:"qualityFloor,omitempty"`
	FloorInferred     bool               `json:"floorInferred,omitempty"`
	Goal              string             `json:"goal,omitempty"`
	GoalStatus        string             `json:"goalStatus,omitempty"`
	Recovered         bool               `json:"recovered,omitempty"`
	RecoveryReason    string             `json:"recoveryReason,omitempty"`
	RecoveryDigest    string             `json:"recoveryDigest,omitempty"`
	RecoveryParentID  string             `json:"recoveryParentId,omitempty"`
	StartupErr        string             `json:"startupErr,omitempty"`
	Active            bool               `json:"active"`
	Cwd               string             `json:"cwd"`
}
