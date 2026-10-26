package serve

import (
	"context"
	"errors"
	"slices"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/session/control"
)

// Refusal codes one folder of an open_projects call can carry; the call itself
// still answers for the others.
const (
	codeOpenNotReadable = "open_projects.not_openable"
	codeOpenListFull    = "workspace.limit_reached"
	codeOpenListUnwrite = "open_projects.list_unwritable"
	codeOpenTrustFailed = "open_projects.trust_failed"
	codeOpenUntrusted   = "dispatch.untrusted"
	codeOpenAlreadyAuto = "open_projects.auto_workspace"
)

// openProjects adds the folders the user approved to their project list and
// trusts them. Each is vetted the way an extra workspace root is: the folder a
// person should never grant wholesale is refused here whatever the card said.
func openProjects(ctx context.Context, from *control.Controller, paths []string) ([]tool.OpenedProject, error) {
	cfg := config.LoadForEdit(config.UserConfigPath())
	out := make([]tool.OpenedProject, 0, len(paths))
	for _, p := range paths {
		out = append(out, openOneProject(ctx, cfg, from, p))
	}
	return out, nil
}

func openOneProject(ctx context.Context, cfg *config.Config, from *control.Controller, path string) tool.OpenedProject {
	got := tool.OpenedProject{Path: path}
	if _, err := cfg.VetReadableRoot(from.WorkspaceRoot(), path); err != nil {
		got.Code = codeOpenNotReadable
		var root *config.WorkspaceRootError
		if errors.As(err, &root) {
			got.Code = string(root.Code)
		}
		return got
	}
	if isAutoWorkspace(path) {
		got.Code = codeOpenAlreadyAuto
		return got
	}
	if trust := from.TrustOfWorkspace(path); trust == config.WorkspaceTrustDeclined {
		got.Code = codeOpenUntrusted
		return got
	}
	added := false
	err := updateWorkspaceList(ctx, true, func(list *workspaceList) error {
		if slices.Contains(list.Paths, path) {
			return nil
		}
		if workspaceListFull(len(rememberedProjects(list.Paths))) && !hasWorkspaceProject(list.Paths, path) {
			return errWorkspaceListFull
		}
		list.Paths = append(list.Paths, path)
		added = true
		return nil
	})
	switch {
	case errors.Is(err, errWorkspaceListFull):
		got.Code = codeOpenListFull
		return got
	case err != nil:
		got.Code = codeOpenListUnwrite
		return got
	}
	if err := from.TrustFolder(path); err != nil {
		if added {
			forgetWorkspace(path)
		}
		got.Code = codeOpenTrustFailed
		if errors.Is(err, control.ErrDispatchUntrusted) {
			got.Code = codeOpenUntrusted
		}
		return got
	}
	got.Added = added
	return got
}
