package boot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/observe"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
)

// ObserveOptions builds an unattended, read-only run. It is a field of Options
// and nothing else: no flag, configuration key or environment variable reaches
// it, so only a host that assembles the run can ask for the posture.
type ObserveOptions struct {
	// Pending is where a request that needs a person is parked. Required.
	Pending observe.PendingSink
	// Run is what the run tells the model about itself on its turn tail.
	Run observe.RunContext
	// Tools narrows the file-reading tools to these names; nil keeps the whole
	// ceiling. Tools that only hand the turn back to the host always stay, or the
	// run could neither conclude nor park a question.
	Tools []string
}

// observeOverrides pins every Options field the posture depends on. What the
// caller set for them is discarded, because a run that inherits a wider value
// is a run whose confinement depends on who called it.
func observeOverrides(opts Options) Options {
	if opts.Observe == nil {
		return opts
	}
	no := false
	opts.RuntimeReload = RuntimeReload{}
	opts.SharedHost = nil
	opts.ExtraPlugins = nil
	opts.AdditionalDirs = nil
	opts.PermissionAllow = nil
	opts.FileOverlay = nil
	opts.TerminalRunner = nil
	opts.HeadlessApprovalMode = control.ToolApprovalReadOnly
	opts.SandboxBashOverride = "enforce"
	opts.SandboxNetworkOverride = &no
	opts.WorkspaceOnly = true
	opts.GoalTurnsUnreachable = true
	opts.UnattendedChild = true
	opts.Ablation = ablation.New(ablation.Planner, ablation.Subagent)
	return opts
}

// newToolRegistry is the registry every tool of a build is added to. Under the
// posture it holds the ceiling from its first Add, so no registration path,
// however late, can put a tool the ceiling refuses in reach of the model.
func newToolRegistry(opts Options) *tool.Registry {
	reg := tool.NewRegistry()
	if opts.Observe != nil {
		reg.Restrict(observeAdmits(opts.Observe.Tools))
	}
	return reg
}

func observeAdmits(narrow []string) func(tool.Tool) bool {
	names := slices.Clone(narrow)
	return func(t tool.Tool) bool {
		if !observe.Admits(t) {
			return false
		}
		return len(names) == 0 || tool.ReachOf(t) == tool.ReachHostControl || slices.Contains(names, t.Name())
	}
}

func (b *builder) observeRun() *control.ObserveRun {
	if b.opts.Observe == nil {
		return nil
	}
	return &control.ObserveRun{
		Posture: observe.New(sandbox.IntegrityEnforced(b.tools.env.bash)),
		Pending: b.opts.Observe.Pending,
		Context: b.opts.Observe.Run,
	}
}

// ErrObserveRootTooBroad refuses a workspace whose read scope would take in the
// filesystem root or the user's home directory.
var ErrObserveRootTooBroad = errors.New("boot: the read-only posture will not run with a workspace that contains the user's home directory or the filesystem root")

func checkObserveRoot(root string) error {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		real = filepath.Clean(root)
	}
	if abs, err := filepath.Abs(real); err == nil {
		real = abs
	}
	if filepath.Dir(real) == real {
		return ErrObserveRootTooBroad
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return fmt.Errorf("%w: the home directory is unknown, so the scope cannot be shown to exclude it", ErrObserveRootTooBroad)
	}
	if h, err := filepath.EvalSymlinks(home); err == nil {
		home = h
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		real, home = strings.ToLower(real), strings.ToLower(home)
	}
	rel, err := filepath.Rel(real, home)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrObserveRootTooBroad
	}
	return nil
}

// ErrObserveRebuild refuses to rebuild a read-only run into an ordinary one: a
// rebuild inherits nothing of the posture, so the caller has to state it again.
var ErrObserveRebuild = errors.New("boot: a controller under the read-only posture cannot be rebuilt without it")
