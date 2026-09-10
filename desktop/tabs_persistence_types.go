package main

type desktopTabEntry struct {
	ID                string  `json:"id"`
	Scope             string  `json:"scope"`
	WorkspaceRoot     string  `json:"workspaceRoot"`
	TopicID           string  `json:"topicId"`
	SessionPath       string  `json:"sessionPath,omitempty"`
	ReadOnly          bool    `json:"readOnly,omitempty"`
	TakeoverSpectator bool    `json:"takeoverSpectator,omitempty"`
	Model             string  `json:"model,omitempty"`
	Effort            *string `json:"effort,omitempty"`
	TokenMode         string  `json:"tokenMode,omitempty"`
	AgentPreset       string  `json:"agentPreset,omitempty"`
	QualityFloor      string  `json:"qualityFloor,omitempty"`
	Mode              string  `json:"mode,omitempty"`
	Goal              string  `json:"goal,omitempty"`
	ToolApprovalMode  string  `json:"toolApprovalMode,omitempty"`
	// PinnedFiles is read-only upgrade input from the unmerged tab-scoped
	// implementation. New writers persist pins beside the owning session.
	PinnedFiles []string `json:"pinnedFiles,omitempty"`
}

type desktopTabsFile struct {
	Tabs           []desktopTabEntry       `json:"tabs"`
	ActiveTab      string                  `json:"activeTab"`
	RemoteTabs     []desktopRemoteTabEntry `json:"remoteTabs,omitempty"`
	RemoteTabOrder []string                `json:"remoteTabOrder,omitempty"`
	TabOrder       []string                `json:"tabOrder,omitempty"`
}

func persistedDesktopTabEntry(tab *WorkspaceTab) desktopTabEntry {
	return desktopTabEntry{
		ID:                tab.ID,
		Scope:             tab.Scope,
		WorkspaceRoot:     tab.WorkspaceRoot,
		TopicID:           tab.TopicID,
		SessionPath:       tab.currentSessionPath(),
		ReadOnly:          tab.ReadOnly,
		TakeoverSpectator: tab.Takeover.Spectator,
		Model:             tab.model,
		Effort:            cloneStringPtr(tab.effort),
		AgentPreset:       currentTabAgentPreset(tab),
		TokenMode:         currentTabTokenMode(tab),
		QualityFloor:      tab.qualityFloor,
		Mode:              persistedTabMode(currentTabMode(tab)),
		Goal:              persistedTabGoal(tab),
		ToolApprovalMode:  persistedToolApprovalMode(currentTabToolApprovalMode(tab)),
		PinnedFiles:       tab.pendingLegacyPinnedFilesForPersistence(),
	}
}
