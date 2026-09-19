package main

import (
	"bytes"
	"encoding/json"

	"reasonix/internal/boot"
	"reasonix/internal/control"
)

type desktopTabEntry struct {
	ID                string  `json:"id"`
	Scope             string  `json:"scope"`
	WorkspaceRoot     string  `json:"workspaceRoot"`
	WorkspaceID       string  `json:"workspaceId,omitempty"`
	TopicID           string  `json:"topicId"`
	SessionPath       string  `json:"sessionPath,omitempty"`
	SessionID         string  `json:"sessionId,omitempty"`
	CreateOperationID string  `json:"createOperationId,omitempty"`
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
	PinnedFiles        []string `json:"pinnedFiles,omitempty"`
	extra              map[string]json.RawMessage
	restoreBlocked     bool
	restoreBlockReason string
}

type desktopTabsFile struct {
	Tabs           []desktopTabEntry       `json:"tabs"`
	ActiveTab      string                  `json:"activeTab"`
	RemoteTabs     []desktopRemoteTabEntry `json:"remoteTabs,omitempty"`
	RemoteTabOrder []string                `json:"remoteTabOrder,omitempty"`
	TabOrder       []string                `json:"tabOrder,omitempty"`
	extra          map[string]json.RawMessage
}

type desktopTabWorkspace struct {
	ID string
}

func persistedDesktopTabEntry(tab *WorkspaceTab) desktopTabEntry {
	return desktopTabEntry{
		ID:                tab.ID,
		Scope:             tab.Scope,
		WorkspaceRoot:     tab.WorkspaceRoot,
		WorkspaceID:       tab.SessionWorkspace.ID,
		TopicID:           tab.TopicID,
		SessionPath:       tab.currentSessionPath(),
		SessionID:         tab.SessionID,
		CreateOperationID: tab.PendingCreateOperationID,
		ReadOnly:          tab.ReadOnly,
		TakeoverSpectator: tab.Takeover.Spectator,
		Model:             tab.model,
		Effort:            cloneStringPtr(tab.effort),
		AgentPreset:       boot.AgentPresetStandard,
		TokenMode:         boot.TokenModeFull,
		QualityFloor:      control.QualityFloorStandard,
		Mode:              persistedTabMode(currentTabMode(tab)),
		Goal:              persistedTabGoal(tab),
		ToolApprovalMode:  persistedToolApprovalMode(currentTabToolApprovalMode(tab)),
		PinnedFiles:       tab.pendingLegacyPinnedFilesForPersistence(),
		extra:             cloneDesktopJSONFields(tab.persistenceExtra),
	}
}

func (entry *desktopTabEntry) UnmarshalJSON(body []byte) error {
	type plain desktopTabEntry
	var decoded plain
	if err := json.Unmarshal(body, &decoded); err != nil {
		return err
	}
	extra, err := desktopUnknownJSONFields(body,
		"id", "scope", "workspaceRoot", "workspaceId", "topicId", "sessionPath", "sessionId", "createOperationId",
		"readOnly", "takeoverSpectator", "model", "effort", "tokenMode", "agentPreset", "qualityFloor", "mode",
		"goal", "toolApprovalMode", "pinnedFiles",
	)
	if err != nil {
		return err
	}
	*entry = desktopTabEntry(decoded)
	entry.extra = extra
	return nil
}

func (entry desktopTabEntry) MarshalJSON() ([]byte, error) {
	type plain desktopTabEntry
	body, err := json.Marshal(plain(entry))
	if err != nil {
		return nil, err
	}
	return mergeDesktopUnknownJSONFields(body, entry.extra)
}

func (entry *desktopRemoteTabEntry) UnmarshalJSON(body []byte) error {
	type plain desktopRemoteTabEntry
	var decoded plain
	if err := json.Unmarshal(body, &decoded); err != nil {
		return err
	}
	extra, err := desktopUnknownJSONFields(body,
		"id", "hostId", "workspace", "topicTitle", "model", "sessionName", "sessionPath", "sessionId", "sessionReset",
	)
	if err != nil {
		return err
	}
	*entry = desktopRemoteTabEntry(decoded)
	entry.extra = extra
	return nil
}

func (entry desktopRemoteTabEntry) MarshalJSON() ([]byte, error) {
	type plain desktopRemoteTabEntry
	body, err := json.Marshal(plain(entry))
	if err != nil {
		return nil, err
	}
	return mergeDesktopUnknownJSONFields(body, entry.extra)
}

func (file *desktopTabsFile) UnmarshalJSON(body []byte) error {
	type plain desktopTabsFile
	var decoded plain
	if err := json.Unmarshal(body, &decoded); err != nil {
		return err
	}
	extra, err := desktopUnknownJSONFields(body, "tabs", "activeTab", "remoteTabs", "remoteTabOrder", "tabOrder")
	if err != nil {
		return err
	}
	*file = desktopTabsFile(decoded)
	file.extra = extra
	return nil
}

func (file desktopTabsFile) MarshalJSON() ([]byte, error) {
	type plain desktopTabsFile
	body, err := json.Marshal(plain(file))
	if err != nil {
		return nil, err
	}
	return mergeDesktopUnknownJSONFields(body, file.extra)
}

func desktopUnknownJSONFields(body []byte, known ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	for _, key := range known {
		delete(fields, key)
	}
	return fields, nil
}

func mergeDesktopUnknownJSONFields(known []byte, extra map[string]json.RawMessage) ([]byte, error) {
	if len(extra) == 0 {
		return known, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(known, &fields); err != nil {
		return nil, err
	}
	for key, value := range extra {
		if _, exists := fields[key]; !exists {
			fields[key] = bytes.Clone(value)
		}
	}
	return json.Marshal(fields)
}

func cloneDesktopJSONFields(fields map[string]json.RawMessage) map[string]json.RawMessage {
	if len(fields) == 0 {
		return nil
	}
	clone := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		clone[key] = bytes.Clone(value)
	}
	return clone
}
