package main

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/worktree"
)

var (
	inspectDeliveryWorktree  = worktree.Inspect
	createDeliveryWorktree   = worktree.Create
	rollbackDeliveryWorktree = worktree.RollbackCreate
)

// IsolatedWorktreeOpenResult is returned after an isolated Git workspace has
// been created and opened as a normal Reasonix project.
type IsolatedWorktreeOpenResult struct {
	WorkspaceRoot string  `json:"workspaceRoot"`
	WorktreeRoot  string  `json:"worktreeRoot"`
	SourceRoot    string  `json:"sourceRoot"`
	Branch        string  `json:"branch"`
	SourceDirty   bool    `json:"sourceDirty"`
	Tab           TabMeta `json:"tab"`
}

// DeliveryWorktreeOpenResult is the deprecated alias of
// IsolatedWorktreeOpenResult kept bound for one compatibility version.
type DeliveryWorktreeOpenResult = IsolatedWorktreeOpenResult

// IsolatedWorktreeAvailability reports whether workspaceRoot can use the
// optional Git isolation path. A false result never disables writing itself;
// the cross-platform workspace writer lease remains the no-Git fallback.
func (a *App) IsolatedWorktreeAvailability(workspaceRoot string) worktree.Availability {
	return inspectDeliveryWorktree(a.bootContext(), workspaceRoot)
}

// CreateIsolatedWorktree creates a durable branch-backed worktree and opens it
// as a project. It never switches or modifies the source checkout, and it does
// not delete the new worktree if later UI registration fails. The opened tab
// infers the delivery quality floor (switchable to standard at any time).
func (a *App) CreateIsolatedWorktree(workspaceRoot string) (IsolatedWorktreeOpenResult, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	created, err := createDeliveryWorktree(a.bootContext(), workspaceRoot, config.DeliveryWorktreeDir())
	if err != nil {
		return IsolatedWorktreeOpenResult{}, err
	}

	var tab TabMeta
	if a.singleSurfaceLayoutEnabled() {
		tab, err = a.ensureBlankSurface("project", created.WorkspaceRoot)
	} else {
		tab, err = a.ensureBlankTab("project", created.WorkspaceRoot)
	}
	if err != nil {
		return IsolatedWorktreeOpenResult{}, fmt.Errorf("isolated worktree was created at %s but Reasonix could not open it: %w", created.WorktreeRoot, err)
	}
	return IsolatedWorktreeOpenResult{
		WorkspaceRoot: created.WorkspaceRoot,
		WorktreeRoot:  created.WorktreeRoot,
		SourceRoot:    created.SourceRoot,
		Branch:        created.Branch,
		SourceDirty:   created.SourceDirty,
		Tab:           tab,
	}, nil
}

// DeliveryWorktreeAvailability is the deprecated alias of
// IsolatedWorktreeAvailability, kept bound for one compatibility version.
func (a *App) DeliveryWorktreeAvailability(workspaceRoot string) worktree.Availability {
	return a.IsolatedWorktreeAvailability(workspaceRoot)
}

// CreateDeliveryWorktree is the deprecated alias of CreateIsolatedWorktree,
// kept bound for one compatibility version.
func (a *App) CreateDeliveryWorktree(workspaceRoot string) (DeliveryWorktreeOpenResult, error) {
	return a.CreateIsolatedWorktree(workspaceRoot)
}
