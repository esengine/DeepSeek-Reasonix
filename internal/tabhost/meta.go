package tabhost

// TabMeta is the frontend-facing shape of one tab.
// JSON tags match desktop.TabMeta / desktop frontend types.TabMeta.
type TabMeta struct {
	ID                string         `json:"id"`
	Scope             string         `json:"scope"`
	WorkspaceRoot     string         `json:"workspaceRoot"`
	WorkspaceName     string         `json:"workspaceName"`
	WorkspacePath     string         `json:"workspacePath,omitempty"`
	GitBranch         string         `json:"gitBranch,omitempty"`
	IsolatedWorktree  bool           `json:"isolatedWorktree,omitempty"`
	TopicID           string         `json:"topicId"`
	TopicTitle        string         `json:"topicTitle"`
	SessionPath       string         `json:"sessionPath,omitempty"`
	ReadOnly          bool           `json:"readOnly,omitempty"`
	ProjectColor      string         `json:"projectColor,omitempty"`
	Label             string         `json:"label"`
	Ready             bool           `json:"ready"`
	Runtime           map[string]any `json:"runtime,omitempty"`
	Running           bool           `json:"running"`
	PendingPrompt     bool           `json:"pendingPrompt,omitempty"`
	BackgroundJobs    int            `json:"backgroundJobs,omitempty"`
	CancelRequested   bool           `json:"cancelRequested,omitempty"`
	Cancellable       bool           `json:"cancellable"`
	Mode              string         `json:"mode"`
	CollaborationMode string         `json:"collaborationMode"`
	ToolApprovalMode  string         `json:"toolApprovalMode"`
	TokenMode         string         `json:"tokenMode"`
	Goal              string         `json:"goal,omitempty"`
	GoalStatus        string         `json:"goalStatus,omitempty"`
	StartupErr        string         `json:"startupErr,omitempty"`
	Active            bool           `json:"active"`
	Cwd               string         `json:"cwd"`
}

// CreateTabOpts configures a new tab.
type CreateTabOpts struct {
	Scope         string // "project" | "global"
	WorkspaceRoot string
	TopicID       string
	TopicTitle    string
	SessionPath   string // optional resume path
	ReadOnly      bool
	Label         string
}

// ScopeProject and ScopeGlobal match desktop tab scopes.
const (
	ScopeProject = "project"
	ScopeGlobal  = "global"
)
