package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reasonix/internal/frontend/hostenv"
	"strings"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/surface"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/session/control"
)

// AllowWorkspaceSwitch grants POST /workspace. It is off until a host asks for
// it, and no config file can turn it on: a server reachable over the network
// would otherwise let any client repoint the agent at any directory it can
// read. The desktop shell asks because its only client is its own window.
func (s *Server) AllowWorkspaceSwitch() { s.grants.workspaceSwitch = true }

// SessionDirFor is where root's transcripts live. Exported because the shell
// has to build its first controller with the same answer a later switch uses,
// or the session rail would list one project's history while driving another.
func SessionDirFor(root string) string {
	if dir := config.ProjectSessionDir(root); dir != "" {
		return dir
	}
	return config.SessionDir()
}

func (s *Server) workspaces(w http.ResponseWriter, r *http.Request) {
	type workspaceView struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	current := s.ctl().WorkspaceRoot()
	recents := []workspaceView{}
	for _, p := range Workspaces() {
		if p == current {
			continue
		}
		recents = append(recents, workspaceView{Path: p, Name: fileutil.RootName(p)})
	}
	writeJSONCached(w, r, struct {
		Current  string          `json:"current"`
		Switch   bool            `json:"canSwitch"`
		Isolate  bool            `json:"canIsolate"`
		Recents  []workspaceView `json:"recents"`
		Isolated bool            `json:"isolated,omitempty"`
	}{
		Current:  current,
		Switch:   s.grants.at(r).workspaceSwitch,
		Isolate:  s.grants.at(r).workspaceSwitch && worktree.Inspect(r.Context(), workspaceRepo(s.ctl())).Available,
		Recents:  recents,
		Isolated: worktree.IsManagedPath(current, config.DeliveryWorktreeDir()),
	})
}

// workspace repoints the whole runtime at another folder. The conversation is
// deliberately not carried: the transcript, the memory index, and the skill set
// all belong to the project that produced them.
func (s *Server) workspace(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).workspaceSwitch {
		refuse(w, http.StatusForbidden, "workspace.changing_disabled", "this server may not change its workspace", nil)
		return
	}
	var body struct {
		Path     string             `json:"path"`
		Isolate  bool               `json:"isolate"`
		Worktree *worktreeSelection `json:"worktree,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}

	s.bindMu.Lock()
	defer s.bindMu.Unlock()

	dir := strings.TrimSpace(body.Path)
	published := false
	if controllerHasActiveRuntimeWork(s.ctl()) {
		writeErr(w, http.StatusConflict, busyErr("busy.change_workspace", "cannot change the workspace while active work or background jobs are running"))
		return
	}
	if body.Worktree != nil && (!body.Isolate || body.Worktree.Ref == "" || body.Worktree.SourceRoot != s.ctl().WorkspaceRoot() || !emptyConversation(s.ctl())) {
		refuse(w, http.StatusConflict, "workspace.worktree_stale", "start a new session in the source workspace before creating a worktree", nil)
		return
	}
	if body.Isolate {
		ref := ""
		if body.Worktree != nil {
			ref = body.Worktree.Ref
		}
		managedRoot := config.DeliveryWorktreeDir()
		res, err := worktree.CreateFrom(r.Context(), workspaceRepo(s.ctl()), managedRoot, ref)
		if err != nil {
			if errors.Is(err, worktree.ErrSourceUnknown) {
				refuse(w, http.StatusConflict, "workspace.worktree_source_missing", "the source branch is no longer available; choose another branch", nil)
				return
			}
			writeErr(w, http.StatusUnprocessableEntity, err)
			return
		}
		repo := workspaceRepo(s.ctl())
		defer func() {
			if !published {
				if err := worktree.DiscardUnpublished(context.WithoutCancel(r.Context()), repo, managedRoot, res); err != nil {
					slog.Warn("retain unpublished worktree after rollback failure", "path", res.WorkspaceRoot, "err", err)
				}
			}
		}()
		dir = res.WorkspaceRoot
		if body.Worktree != nil {
			if err := control.CarryDraftAttachments(body.Worktree.SourceRoot, dir, body.Worktree.Attachments); err != nil {
				writeErr(w, http.StatusUnprocessableEntity, err)
				return
			}
		}
	}
	dir, err := resolveWorkspaceDir(dir)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.switchWorkspaceLocked(r.Context(), dir); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	published = true
	writeJSON(w, struct {
		WorkspaceRoot string `json:"workspaceRoot"`
	}{dir})
}

func resolveWorkspaceDir(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("missing path")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	return abs, nil
}

// switchWorkspaceLocked builds a replacement runtime rooted at dir and publishes
// it. bindMu must be held. The outgoing controller keeps serving until the
// replacement is fully built, so a failure leaves the session untouched.
func (s *Server) switchWorkspaceLocked(ctx context.Context, dir string) error {
	return s.switchWorkspaceCommit(ctx, dir, func(publish func() error) error { return publish() })
}

func (s *Server) switchWorkspaceCommit(ctx context.Context, dir string, commit func(func() error) error) error {
	cur := s.ctl()
	if cur.WorkspaceRoot() == dir {
		return nil
	}
	if controllerHasActiveRuntimeWork(cur) {
		return busyErr("busy.change_workspace", "cannot change the workspace while active work or background jobs are running")
	}
	// The outgoing conversation stays in its own project's session dir; persist
	// it before letting go, because nothing carries it forward.
	if err := cur.Snapshot(); err != nil {
		slog.Warn("serve: snapshot before workspace switch", "err", err)
	}

	newCtrl, err := s.buildForWorkspace(ctx, dir, currentModelRef(cur))
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}
	newCtrl.EnableInteractiveApproval()
	if cur.Posture().Defaulted {
		newCtrl.ApplyDefaultPosture()
	} else {
		newCtrl.SetToolApprovalMode(cur.ToolApprovalMode())
	}
	newCtrl.SetPlanMode(cur.PlanMode())
	if current, ok := cur.(*control.Controller); ok {
		if err := newCtrl.SetModelMode(current.ModelMode()); err != nil {
			newCtrl.Close()
			return err
		}
	}
	newCtrl.SetOnSessionRecovered(sessionLeaseRecoveryHandler(s.leases))

	if err := commit(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := addRememberedWorkspace(ctx, dir); err != nil {
			return err
		}
		return s.publishController(cur, newCtrl)
	}); err != nil {
		newCtrl.Close()
		return err
	}
	s.nameWorkspaceHolder(newCtrl)

	// Session file names are timestamps, unique per project but not across
	// them, so the title cache has to move with the directory.
	s.titles.setDir(newCtrl.SessionDir())
	// No session file exists yet in the new project; releasing is what an empty
	// path means to the keeper.
	if err := s.rebindSessionLeaseFor(newCtrl.SessionPath(), newCtrl); err != nil {
		slog.Warn("serve: rebind session lease after workspace switch", "err", err)
	}
	s.resetSessionView()

	cur.Close()
	return nil
}

// statsSurface labels the usage records this server's rebuilds write. Zero is
// Serve: a server no hub claimed is the bare frontend, and a rebuild that
// guessed would file a window's turns under a frontend nobody ran.
func (s *Server) statsSurface() surface.Surface { return s.surface.Or(surface.Serve) }

// rebuildOptions carries the live controller's workspace into a model, effort
// or extension rebuild. Left to boot's defaults the root re-resolves from the
// process working directory and sessions fall back to the global dir, so the
// switch would quietly serve another project's conversations.
func (s *Server) rebuildOptions(cur control.SessionAPI, ref string) boot.Options {
	opts := boot.Options{Model: ref, Sink: s.rebuildSink(), NativeSurfaces: true, Stderr: os.Stderr, StatsSource: s.statsSurface(), FeedbackSurface: feedbackSurface(s.statsSurface()), AccountsURL: accountsBaseURL(), ProviderResolver: s.resolver, CleanupPendingReconciler: BackgroundCleanupReconciler}
	opts.VerificationClient = hostenv.VerificationClient()
	if s.lastBuild != nil {
		opts.ComputerDriver = s.lastBuild.ComputerDriver
	}
	if cur == nil {
		return opts
	}
	opts.NativeSurfaces = cur.NativeSurfaces()
	opts.AgentPreset = cur.AgentPreset()
	opts.WorkspaceRoot, opts.SessionDir = cur.WorkspaceRoot(), cur.SessionDir()
	// Keep the logical session's private temporary directory across the rebuild.
	if ctrl, ok := cur.(*control.Controller); ok && ctrl != nil {
		opts.Composition = ctrl.CompositionBinding()
		opts.Home = ctrl.PresetStore().Home
		opts.SessionTemp = ctrl.SessionTemp()
		opts.WorkspaceRepo = ctrl.WorkspaceRepo()
		opts.BrowserSession = ctrl.BrowserSession()
		opts.RootEnvironment = ctrl.RootEnvironment()
		opts.SessionEnvironmentPath = ctrl.SessionPath()
		opts.DelegationScheduler = ctrl.DelegationScheduler()
	}
	opts.RuntimeReload = s.reuseFromLastBuild()
	return opts
}

// reloadOptions is the opposite of a switch. A switch reuses the running
// sidecars and the discovered assembly because the extensions did not move; a
// reload exists precisely because what is on disk did move, so reusing either
// would hand the old code back and the user would keep restarting the app.
// Owner survives — it identifies the session lineage, not the extension state.
func (s *Server) reloadOptions(cur control.SessionAPI, ref string) boot.Options {
	opts := s.rebuildOptions(cur, ref)
	opts.RuntimeReload = boot.RuntimeReload{ForceFullRebuild: true, Owner: opts.Owner}
	return opts
}

// AdoptRuntime records the generation a host assembled before handing the
// controller over, so the first model or effort switch reuses it instead of
// paying for a cold assembly. Hosts that build with boot.Build have no
// generation to hand over and simply skip this.
func (s *Server) AdoptRuntime(res *boot.BuildResult) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	s.lastBuild = res
}

// reuseFromLastBuild carries the serving generation into its replacement:
// extension sidecars keep running, the dependency graph is not re-walked, and
// discovery (skills, commands, hooks) is reused when the plan says the
// extension surface did not move. ForceFullRebuild because a switch must end
// up on a new controller — the patched-subgraph path keeps the live one.
func (s *Server) reuseFromLastBuild() boot.RuntimeReload {
	prev := s.lastBuild
	if prev == nil {
		return boot.RuntimeReload{}
	}
	reload := boot.RuntimeReload{
		ForceFullRebuild:   true,
		Extensions:         prev.Extensions,
		Owner:              prev.Owner,
		PreviousSnapshot:   prev.Snapshot,
		PreviousDispatcher: prev.Dispatcher,
		PreviousPlan:       prev.Plan,
		ReuseAssembly:      prev.Assembly,
	}
	if prev.Plan != nil {
		reload.Graph = prev.Plan.Graph
	}
	if prev.Snapshot != nil {
		reload.Generation = prev.Snapshot.Generation()
	}
	return reload
}

// build returns the replacement controller for a model switch, using the
// injected builder in tests and boot.Build in production.
func (s *Server) build(ctx context.Context, ref string) (*control.Controller, error) {
	if s.buildController != nil {
		return s.buildController(ctx, ref)
	}
	opts := s.rebuildOptions(s.ctl(), ref)
	if cur, ok := s.ctl().(*control.Controller); ok && cur != nil {
		opts.Jobs, opts.WorkspaceLease = cur.JobManager(), cur.WorkspaceLeaseOwner()
	}
	res, err := boot.BuildRuntime(ctx, opts)
	if err != nil {
		return nil, err
	}
	s.lastBuild = res
	return res.Controller, nil
}

// rebuildWith runs one rebuild. adopt records the new generation as the base
// for later reuse: a reload must, because it replaced the sidecars and the old
// manager is about to be closed, and reusing a closed one later would serve a
// generation that no longer exists.
func (s *Server) rebuildWith(ctx context.Context, old *control.Controller, ref string, opts boot.Options, adopt bool) (*control.Controller, error) {
	if s.rebuildController != nil {
		return s.rebuildController(ctx, old, ref)
	}
	res, err := boot.Rebuild(ctx, old, opts)
	if err != nil {
		return nil, err
	}
	if adopt {
		s.lastBuild = res
	}
	return res.Controller, nil
}

func (s *Server) buildForWorkspace(ctx context.Context, dir, ref string) (*control.Controller, error) {
	if s.buildWorkspaceController != nil {
		return s.buildWorkspaceController(ctx, dir, ref)
	}
	return boot.Build(ctx, s.workspaceOptions(dir, ref))
}

// workspaceOptions builds the runtime that moves this pane to another root.
// The outgoing conversation stays in its project; host capabilities and the
// pane sink survive the move.
func (s *Server) workspaceOptions(dir, ref string) boot.Options {
	opts := s.rebuildOptions(nil, ref)
	opts.WorkspaceRoot, opts.SessionDir = dir, SessionDirFor(dir)
	if ctl := s.ctl(); ctl != nil {
		opts.NativeSurfaces = ctl.NativeSurfaces()
		opts.AgentPreset = ctl.AgentPreset()
		if current, ok := ctl.(*control.Controller); ok {
			effort := current.RuntimeSelection().Effort
			opts.EffortOverride = &effort
		}
	}
	return opts
}
