package main

import (
	"fmt"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

// TurnNodeInfo is the wire-ready representation of one turn node for the
// desktop conversation-tree panel.
type TurnNodeInfo struct {
	BranchID      string `json:"branchId"`
	BranchName    string `json:"branchName"`
	Turn          int    `json:"turn"`
	Prompt        string `json:"prompt"`
	Response      string `json:"response"`
	PrefixChars   int    `json:"prefixChars"`
	IsCurrent     bool   `json:"isCurrent"`
	Depth         int    `json:"depth"`
	HasFork       bool   `json:"hasFork"`
	ChildCount    int    `json:"childCount"`
	Key           string `json:"key"`
	ParentKey     string `json:"parentKey"`
	RootKey       string `json:"rootKey"`
	RootIndex     int    `json:"rootIndex"`
	Scope         string `json:"scope"`
	WorkspaceRoot string `json:"workspaceRoot"`
	TopicID       string `json:"topicId"`
	TopicTitle    string `json:"topicTitle"`
}

// TurnTreeData is the full turn-tree payload sent to the frontend.
type TurnTreeData struct {
	Roots      []TurnNodeInfo `json:"roots"`
	Nodes      []TurnNodeInfo `json:"nodes"`
	CurrentKey string         `json:"currentKey"`
}

func (a *App) TurnTree() (TurnTreeData, error) {
	return a.TurnTreeForTab("")
}

func (a *App) TurnTreeForTab(tabID string) (TurnTreeData, error) {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return TurnTreeData{}, fmt.Errorf("tab is not ready")
	}
	return turnTreeData(ctrl)
}

func turnTreeData(ctrl *control.Controller) (TurnTreeData, error) {
	tree, err := ctrl.TurnTree()
	if err != nil {
		return TurnTreeData{}, err
	}
	branchNames := map[string]string{}
	branchMeta := map[string]agent.BranchMeta{}
	if branches, err := agent.ListBranches(ctrl.SessionDir()); err == nil {
		for _, b := range branches {
			if b.Name != "" {
				branchNames[b.ID] = b.Name
			}
			branchMeta[b.ID] = b.BranchMeta
		}
	}
	flat := tree.FlattenCurrentRoot()
	currentRootKey := ""
	for _, n := range flat {
		if n.Key == tree.CurrentKey {
			currentRootKey = n.RootKey
			break
		}
	}
	if currentRootKey == "" {
		return TurnTreeData{Roots: []TurnNodeInfo{}, Nodes: []TurnNodeInfo{}, CurrentKey: ""}, nil
	}
	nodes := make([]TurnNodeInfo, 0, len(flat))
	infoByKey := make(map[string]TurnNodeInfo, len(flat))
	for _, n := range flat {
		if currentRootKey != "" && n.RootKey != currentRootKey {
			continue
		}
		meta := branchMeta[n.BranchID]
		info := TurnNodeInfo{
			BranchID:      n.BranchID,
			BranchName:    branchNames[n.BranchID],
			Turn:          n.Turn,
			Prompt:        n.Prompt,
			Response:      n.Response,
			PrefixChars:   n.PrefixChars,
			IsCurrent:     n.IsCurrent,
			Depth:         n.Depth,
			HasFork:       n.HasFork,
			ChildCount:    n.ChildCount,
			Key:           n.Key,
			ParentKey:     n.ParentKey,
			RootKey:       n.RootKey,
			RootIndex:     n.RootIndex,
			Scope:         meta.DefaultScope(),
			WorkspaceRoot: meta.WorkspaceRoot,
			TopicID:       meta.TopicID,
			TopicTitle:    meta.TopicTitle,
		}
		nodes = append(nodes, info)
		infoByKey[info.Key] = info
	}
	roots := make([]TurnNodeInfo, 0, len(tree.Roots))
	for _, root := range tree.Roots {
		if currentRootKey != "" && agent.NodeKey(root.BranchID, root.Turn) != currentRootKey {
			continue
		}
		if info, ok := infoByKey[agent.NodeKey(root.BranchID, root.Turn)]; ok {
			roots = append(roots, info)
		}
	}
	return TurnTreeData{Roots: roots, Nodes: nodes, CurrentKey: tree.CurrentKey}, nil
}

func (a *App) JumpToTurn(branchID string, turn int) error {
	return a.JumpToTurnForTab("", branchID, turn)
}

func (a *App) JumpToTurnForTab(tabID, branchID string, turn int) error {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return fmt.Errorf("tab is not ready")
	}
	return ctrl.JumpToTurn(branchID, turn)
}

func (a *App) PersistTurnPreviewForTab(tabID string) error {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return fmt.Errorf("tab is not ready")
	}
	return ctrl.MaterializeJumpPreview()
}
