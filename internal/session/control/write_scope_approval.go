package control

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
)

// WriteScopeApprovalTool is the internal Tool name used when a file tool is
// asked to write outside the workspace write scope. It is a fresh human
// decision, and its folder grants live in approvalManager.writeDirs rather than
// in the generic session grants, which for a tool this narrow would be
// tool-wide: one click must open one folder, not every folder.
const WriteScopeApprovalTool = "write_scope"

// DeleteScopeApprovalTool is the internal Tool name used when a shell command
// recursively deletes paths outside the workspace and its granted folders. It is
// a fresh human decision and offers only "this command": there is no folder
// grant, because a grant for a parent would let the next delete reach all of it.
const DeleteScopeApprovalTool = "delete_scope"

// ApproveWriteScope is the card a file tool puts up when a write lands outside
// the workspace write scope. Three answers: this write only; this folder for the
// rest of the session; this folder written down for this project. A wider
// answer is honoured only for a folder that passes the vetting a stored grant
// has to pass, whatever the frontend sent — the card never offers the others.
func (m fileWriteApprover) ApproveWriteScope(ctx context.Context, req tool.WriteScopeRequest) (bool, string, error) {
	args, _ := json.Marshal(map[string]string{"path": req.Path, "dir": req.Dir})
	reply, err := m.c.requestApprovalDecision(ctx, approvalRequest{
		tool: WriteScopeApprovalTool, subject: req.Path, args: args, fresh: true,
		reason: fmt.Sprintf(i18n.M.WriteScopeReasonFmt, req.Dir),
	})
	if err != nil {
		return false, "approval aborted", err
	}
	if !reply.allow {
		return false, i18n.M.ScopeDeclined, nil
	}
	if reply.persist && m.c.writeScopePersistable(req.Dir) {
		m.c.emitRememberResult(m.c.rememberWriteDir(req.Dir))
	}
	return true, "", nil
}

// noteWriteScopeReply turns an allowing answer that asked for more than this one
// write into a folder grant for the session, when the folder may be granted.
func (c *Controller) noteWriteScopeReply(file string, r approvalReply) {
	dir := filepath.Dir(file)
	if r.allow && (r.session || r.persist) && c.writeScopeSessionable(dir) {
		c.approval.grantWriteDir(dir)
	}
}

func (m fileWriteApprover) WriteScopeSessionAllowed(_ context.Context, req tool.WriteScopeRequest) bool {
	return m.c.approval.writeDirAllows(req.Dir)
}

// writeScopeSessionable is whether a folder may be allowed for the session: an
// existing folder the person owns that is not a home directory, a drive root,
// Reasonix's own storage, a credential directory or one on the forbid list.
func (c *Controller) writeScopeSessionable(dir string) bool {
	primary := c.WorkspaceRoot()
	if primary == "" || !filepath.IsAbs(dir) {
		return false
	}
	cfg, err := config.LoadForRootReadOnly(primary)
	if err != nil {
		return false
	}
	_, err = cfg.VetReadableRoot(primary, dir)
	return err == nil
}

// writeScopePersistable is whether the folder may also be recorded as one of
// this project's extra folders: everything the session grant needs, and clear of
// the project's own write roots.
func (c *Controller) writeScopePersistable(dir string) bool {
	primary := c.WorkspaceRoot()
	if !c.writeScopeSessionable(dir) {
		return false
	}
	cfg, err := config.LoadForRootReadOnly(primary)
	if err != nil {
		return false
	}
	_, err = cfg.VetExtraRoot(primary, dir, cfg.ExtraRoots())
	return err == nil
}

func (c *Controller) rememberWriteDir(dir string) RememberResult {
	store := config.NewProjectGrantStore(config.Roots{}.Home())
	resolved, err := c.AddWorkspaceRoot(dir)
	if err != nil {
		return RememberResult{Rule: dir, Path: store.Path(), Err: err}
	}
	return RememberResult{Rule: resolved, Path: store.Path(), Saved: true}
}

// approvalGrants is what a card may offer beyond "once": the tool's own contract,
// narrowed to what can be kept now. A write-scope card is narrowed by its folder.
func (c *Controller) approvalGrants(tool, subject string, fresh, requireHuman bool) (session, persist bool) {
	if tool == WriteScopeApprovalTool {
		dir := filepath.Dir(subject)
		return c.writeScopeSessionable(dir), c.writeScopePersistable(dir)
	}
	session, persist = approvalGrantsForRequest(tool, fresh, requireHuman)
	return session, c.offersPersist(tool, persist)
}

func (a *approvalManager) grantWriteDir(dir string) {
	a.mu.Lock()
	added := !slices.Contains(a.writeDirs, dir)
	if added {
		a.writeDirs = append(a.writeDirs, dir)
	}
	changed := a.onGrantChange
	a.mu.Unlock()
	if added && changed != nil {
		changed()
	}
}

// writeDirAllows is whether dir is a folder allowed for this session, or lies
// below one. Fresh human decisions are never answered by a posture, so this is
// the only thing a write outside the scope consults.
func (a *approvalManager) writeDirAllows(dir string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, granted := range a.writeDirs {
		if folderHolds(granted, dir) {
			return true
		}
	}
	return false
}

// folderHolds is whether dir is folder or lies below it. Both are resolved
// paths; the spelling is folded only where the filesystem folds it.
func folderHolds(folder, dir string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		folder, dir = strings.ToLower(folder), strings.ToLower(dir)
	}
	rel, err := filepath.Rel(filepath.Clean(folder), filepath.Clean(dir))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ApproveDeleteScope puts a recursive delete outside the scope to the person. A
// target that no approval may open — Reasonix's own storage, a credential
// directory, a home directory, a filesystem root — is refused without asking.
func (m fileWriteApprover) ApproveDeleteScope(ctx context.Context, req tool.DeleteScopeRequest) (bool, string, error) {
	for _, path := range req.Paths {
		if _, refused := config.RootsForHome("").FolderGrantRefusal(path); refused {
			return false, "", tool.ErrDeleteTargetProtected
		}
	}
	args, _ := json.Marshal(map[string][]string{"paths": req.Paths})
	reply, err := m.c.requestApprovalDecision(ctx, approvalRequest{
		tool: DeleteScopeApprovalTool, subject: strings.Join(req.Paths, ", "), args: args, fresh: true,
		reason: fmt.Sprintf(i18n.M.DeleteScopeReasonFmt, strings.Join(req.Paths, ", ")),
	})
	if err != nil {
		return false, "approval aborted", err
	}
	if !reply.allow {
		return false, i18n.M.ScopeDeclined, nil
	}
	return true, "", nil
}
