package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/serve"
	"reasonix/internal/frontend/termrender"
	"reasonix/internal/frontend/tui"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

// tuiBase is the route prefix of the one runtime a terminal session drives.
// The host name is never resolved: the in-process transport answers it.
const tuiBase = "http://reasonix.local/rt/r1"

// runTUI starts the terminal UI on a kernel in this process. The UI reaches
// the kernel through the same routes Studio uses, over a transport that opens
// no port, so there is nothing on the machine to authenticate against.
func runTUI(args []string, version string) int {
	defer closeCLIUsageCatalogs()
	f := newTUIFlags()
	if code, ok := parseCommandFlags(f.fs, normalizeOptionalResumeArg(args)); !ok {
		return code
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "reasonix tui needs an interactive terminal; use `reasonix run` for scripts")
		return 2
	}
	profile, err := f.runtimeProfile()
	if err != nil {
		return tuiUsageError(err)
	}
	permissions, allowed, permissionsSet, err := f.permissions()
	if err != nil {
		return tuiUsageError(err)
	}
	if rc := chdirTo(*f.dir); rc != 0 {
		return rc
	}
	workspaceRoot, err := workspaceRootForDir(*f.dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	resumePath, ambiguous, err := tuiResolveResume(workspaceRoot, *f.resume, *f.cont, *f.copy)
	if err != nil {
		reportResumeQueryError(os.Stderr, err)
		return 1
	}
	termrender.ConfigureThemeFromConfigForTTYOutput()
	restoreLog := routeLogsAwayFromTerminal()
	defer restoreLog()

	ctx := context.Background()
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	var resumed *sessionstore.Session
	if resumePath != "" {
		if err := leases.Rebind(resumePath); err != nil {
			if errors.Is(err, sessionstore.ErrSessionLeaseHeld) {
				err = errors.New(control.SessionInUseMessage(err) + "; " + control.SessionLeaseCloseHint)
			}
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		if resumed, err = loadResumableSession(resumePath); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
	}
	bc := serve.NewBroadcaster()
	cfg, _ := config.Load()
	ctrl, err := setupProfileWithOverrides(ctx, *f.model, *f.maxSteps, false, withNotifications(bc, cfg), profile, cliBuildOverrides{
		Version: version, WorkspaceRoot: workspaceRoot, OnSessionRecovered: cliSessionRecoveredHandler(leases),
		Effort: f.effortOverride(), PermissionAllow: allowed, AdditionalDirs: f.addDirs,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer ctrl.Close()
	SetTaskJobKiller(ctrlKillerAdapter{ctrl})
	if resumed != nil {
		_ = ctrl.Resume(resumed, resumePath)
	}
	if permissionsSet {
		ctrl.SetToolApprovalMode(permissions.approval)
	}
	if permissions.plan {
		ctrl.SetPlanMode(true)
	}
	ctrl.EnsureSessionPath()
	if err := rebindCLIControllerAuthority(leases, ctrl); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, control.SessionInUseMessage(err)+"; "+control.SessionLeaseCloseHint)
		return 1
	}
	serveCfg := config.ServeConfig{AuthMode: "none"}
	hub := serve.NewHub(serve.HubOptions{Serve: serveCfg})
	defer hub.Shutdown()
	adoptFirstPane(hub, ctrl, bc, bc, serveCfg, leases)

	err = tui.Run(ctx, tui.Options{
		Client:      &tui.Client{HTTP: hub.InProcessClient(), Base: tuiBase},
		Prompt:      strings.Join(f.fs.Args(), " "),
		Restore:     resumed != nil,
		PickSession: *f.resume == resumePickerSentinel || ambiguous != nil,
		PickAmong:   ambiguousSessionPaths(ambiguous),
		Inline:      *f.inline,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	return 0
}

// tuiResolveResume answers which session the UI starts in. A query several
// sessions match is not an error here: the UI offers those matches in its
// picker, unless --copy is set, since the picker resumes in place.
func tuiResolveResume(workspaceRoot, resume string, cont, copySession bool) (string, *ambiguousSessionQueryError, error) {
	resumePath, err := tuiResumePath(workspaceRoot, resume, cont)
	var ambiguous *ambiguousSessionQueryError
	if errors.As(err, &ambiguous) && !copySession {
		return "", ambiguous, nil
	}
	if err == nil && copySession {
		resumePath, err = tuiCopyResume(resumePath)
	}
	return resumePath, nil, err
}

func ambiguousSessionPaths(e *ambiguousSessionQueryError) []string {
	if e == nil {
		return nil
	}
	paths := make([]string, len(e.Matches))
	for i, s := range e.Matches {
		paths[i] = s.Path
	}
	return paths
}

func tuiResumePath(workspaceRoot, resume string, cont bool) (string, error) {
	sessionDir := resolveCLISessionDirFor(workspaceRoot)
	if q := strings.TrimSpace(resume); q != "" {
		return resolveSessionQuery(sessionDir, q)
	}
	if !cont {
		return "", nil
	}
	reclaimCLIRecoveryBranches(sessionDir)
	session, ok := mostRecentSession(sessionDir)
	if !ok {
		return "", errors.New(i18n.M.NoSessionToResume)
	}
	return session.Path, nil
}

// routeLogsAwayFromTerminal sends the kernel's logging to a file for as long
// as the UI owns the screen: a line written to stderr lands in the middle of
// the frame the UI is drawing.
func routeLogsAwayFromTerminal() func() {
	prev := slog.Default()
	path := filepath.Join(config.ReasonixHomeDir(), "logs", "tui.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		slog.SetDefault(slog.New(slog.DiscardHandler))
		return func() { slog.SetDefault(prev) }
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		slog.SetDefault(slog.New(slog.DiscardHandler))
		return func() { slog.SetDefault(prev) }
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(f, nil)))
	return func() {
		slog.SetDefault(prev)
		_ = f.Close()
	}
}

// tuiCopyResume gives --copy a session of its own to continue in.
func tuiCopyResume(resumePath string) (string, error) {
	if resumePath == "" || resumePath == resumePickerSentinel {
		return "", errors.New("--copy requires --resume or --continue")
	}
	return copySessionForWriting(resumePath)
}
