package main

import (
	"fmt"
	"strings"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/worktree"
)

var (
	inspectDeliveryWorktree = worktree.Inspect
	createDeliveryWorktree  = worktree.Create
)

// DeliveryWorktreeOpenResult is returned after an isolated Git workspace has
// been created and opened as a normal Reasonix project.
type DeliveryWorktreeOpenResult struct {
	WorkspaceRoot string  `json:"workspaceRoot"`
	WorktreeRoot  string  `json:"worktreeRoot"`
	SourceRoot    string  `json:"sourceRoot"`
	Branch        string  `json:"branch"`
	SourceDirty   bool    `json:"sourceDirty"`
	Tab           TabMeta `json:"tab"`
}

// DeliveryWorktreeAvailability reports whether workspaceRoot can use the
// optional Git isolation path. A false result never disables Delivery itself;
// the cross-platform workspace writer lease remains the no-Git fallback.
func (a *App) DeliveryWorktreeAvailability(workspaceRoot string) worktree.Availability {
	return inspectDeliveryWorktree(a.bootContext(), workspaceRoot)
}

// CreateDeliveryWorktree creates a durable branch-backed worktree and opens it
// as a project. It never switches or modifies the source checkout, and it does
// not delete the new worktree if later UI registration fails.
func (a *App) CreateDeliveryWorktree(workspaceRoot string) (DeliveryWorktreeOpenResult, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	created, err := createDeliveryWorktree(a.bootContext(), workspaceRoot, config.DeliveryWorktreeDir())
	if err != nil {
		return DeliveryWorktreeOpenResult{}, err
	}

	var tab TabMeta
	if a.singleSurfaceLayoutEnabled() {
		tab, err = a.ensureBlankSurface("project", created.WorkspaceRoot, boot.TokenModeDelivery)
	} else {
		tab, err = a.ensureBlankTab("project", created.WorkspaceRoot, boot.TokenModeDelivery)
	}
	if err != nil {
		return DeliveryWorktreeOpenResult{}, fmt.Errorf("isolated worktree was created at %s but Reasonix could not open it: %w", created.WorktreeRoot, err)
	}
	return DeliveryWorktreeOpenResult{
		WorkspaceRoot: created.WorkspaceRoot,
		WorktreeRoot:  created.WorktreeRoot,
		SourceRoot:    created.SourceRoot,
		Branch:        created.Branch,
		SourceDirty:   created.SourceDirty,
		Tab:           tab,
	}, nil
}
