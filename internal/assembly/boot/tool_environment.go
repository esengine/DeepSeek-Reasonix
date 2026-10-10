package boot

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"

	"reasonix/internal/base/workspaceid"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension/sidecar"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/egress"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/observation"
	"reasonix/internal/state/sessiontemp"
	"reasonix/internal/state/trustedstate"
	"reasonix/internal/tools/builtin"
)

// toolEnvironment is what confines the built-in tools: where they may write
// and read, the bash sandbox, and the session-scoped resources they share with
// the controller.
type toolEnvironment struct {
	writeRoots      []string
	forbidReadRoots []string
	readRoots       []string // non-nil confines the read tools to these directories
	network         bool
	bash            sandbox.Spec
	bashTimeout     time.Duration
	search          builtin.SearchSpec
	sessionGuard    builtin.SessionDataGuard
	managedConfig   builtin.ManagedConfigPaths
	readPaths       *builtin.PathResolver
	sessionTemp     *sessiontemp.Manager
	egress          *egress.Proxy
	evidenceSeal    *agent.EvidenceSeal
}

func (env toolEnvironment) targetAccessCheck() tool.TargetAccessCheck {
	return (builtin.Workspace{
		WriteRoots: env.writeRoots, ReadRoots: env.readRoots, ForbidReadRoots: env.forbidReadRoots,
		SessionGuard: env.sessionGuard, ManagedConfig: env.managedConfig, SessionTemp: env.sessionTemp,
	}).TargetAccessCheck()
}

func resolveToolEnvironment(opts Options, cfg *config.Config, roots config.Roots, root string, additionalDirs []string, shell sandbox.Shell, stderr io.Writer) toolEnvironment {
	// The protected set masks a directory only if it exists when the sandbox is
	// built, so the store's directory has to be there before any command runs.
	if dir := roots.ScheduleDir(); dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
	env := toolEnvironment{
		forbidReadRoots: RuntimeForbidReadRoots(cfg, root),
		network:         cfg.Sandbox.Network,
		bashTimeout:     time.Duration(cfg.BashTimeoutSeconds()) * time.Second,
		readPaths:       builtin.NewPathResolver(),
		sessionTemp:     opts.SessionTemp,
	}
	env.writeRoots = appendUniquePaths(cfg.WriteRootsForRoot(root), additionalDirs...)
	if opts.WorkspaceOnly {
		env.writeRoots = []string{root}
	}
	if opts.Observe != nil {
		env.readRoots = []string{root}
	}
	if opts.SandboxNetworkOverride != nil {
		env.network = *opts.SandboxNetworkOverride
	}
	bashMode := cfg.BashMode()
	if override := strings.TrimSpace(opts.SandboxBashOverride); override != "" {
		bashMode = override
	}
	// Config repair outside the workspace goes through the approval-gated file
	// tools, never raw shell writes, so the bash write roots stay unwidened.
	env.managedConfig = builtin.NewManagedConfigPaths(config.ReasonixManagedConfigPaths())
	env.bash = sandbox.Spec{Mode: bashMode, WriteRoots: env.writeRoots, Pins: sandbox.PinWriteRoots(env.writeRoots), ForbidReadRoots: env.forbidReadRoots, Network: env.network,
		HostAuthorities: sandbox.ParseAuthorities(cfg.Sandbox.HostAuthorities), Shell: shell, ShellEnv: cfg.Tools.Shell.Env}
	// Agent writes into Reasonix's own session stores race the app's saves;
	// an explicit allow_write entry stays the sanctioned escape hatch.
	allowWriteRoots := cfg.AllowWriteRoots()
	if opts.WorkspaceOnly {
		allowWriteRoots = nil
	}
	env.sessionGuard = builtin.NewSessionDataGuard(roots.MemoryUserDir(), allowWriteRoots)
	env.evidenceSeal = openEvidenceSeal(roots.MemoryUserDir(), root, env.bash)
	if env.bash.Mode == "enforce" && !sandbox.Available() {
		fmt.Fprintln(stderr, "warning: "+sandbox.UnavailableMessage())
	}
	env.routeEgress(cfg, stderr)
	env.search = builtin.ResolveSearch(cfg.Tools.Search.Engine, cfg.Tools.Search.RgPath, stderr)
	// A rebuild passes the previous controller's manager, so tools and
	// controller keep one temporary generation.
	if env.sessionTemp == nil {
		env.sessionTemp = sessiontemp.New()
	}
	return env
}

var _ sandbox.EgressRoute = (*egress.Proxy)(nil)

// routeEgress confines bash's external traffic to [sandbox] allowed_domains.
// It applies only where network is on and the bash sandbox confines, so the
// list can narrow what a command reaches and never open what was shut. Where
// the platform cannot enforce it, it says so instead of implying it holds.
func (env *toolEnvironment) routeEgress(cfg *config.Config, stderr io.Writer) {
	if len(cfg.Sandbox.AllowedDomains) == 0 || !env.bash.Network || !env.bash.Enforce() || !sandbox.Available() {
		return
	}
	if !sandbox.EgressSupported() {
		fmt.Fprintln(stderr, "warning: [sandbox] allowed_domains is not enforced on this platform; bash network egress stays open")
		return
	}
	upstream := httpproxy.FromEnvironment().ProxyFunc()
	p, err := egress.Start(egress.Policy{Allow: cfg.Sandbox.AllowedDomains, Deny: cfg.Sandbox.DeniedDomains}, egress.Options{
		Upstream: func(r *http.Request) (*url.URL, error) { return upstream(r.URL) },
	})
	if err == nil && sandbox.EgressNeedsSocket() {
		if err = listenEgressSocket(p); err != nil {
			_ = p.Close()
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "warning: [sandbox] allowed_domains could not start the egress proxy (%v); bash network egress is shut\n", err)
		env.bash.Network = false
		return
	}
	env.egress = p
	env.bash.Egress = p
	env.bash.ClosedLoopbackPorts = closedLoopbackPorts(cfg.Tools.Shell.Env)
}

// closedLoopbackPorts unions the proxy ports the host environment names with
// the ones [tools.shell] env gives the confined command. Both must stay shut: a
// preset proxy would otherwise be reachable, and overriding a variable must not
// reopen the host's proxy.
func closedLoopbackPorts(shellEnv map[string]string) []int {
	ports := egress.LoopbackProxyPorts(os.Getenv)
	if len(shellEnv) == 0 {
		return ports
	}
	ports = append(ports, egress.LoopbackProxyPorts(sandbox.EffectiveGetenv(shellEnv))...)
	slices.Sort(ports)
	return slices.Compact(ports)
}

// listenEgressSocket puts the proxy's socket under the user's cache, which the
// sandbox sees; bubblewrap mounts it read-only into each confined command.
func listenEgressSocket(p *egress.Proxy) error {
	dir, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	return p.ListenUnix(filepath.Join(dir, "reasonix", "egress", egress.NewToken()+".sock"))
}

// openEvidenceSeal places Trusted Host State where the file tools' guard
// refuses writes. At every write it asks the components that start what the
// model drives — the bash sandbox, the MCP and extension launchers — whether
// any of them left a way in; only they know the integrity level.
func openEvidenceSeal(stateRoot, workspace string, bash sandbox.Spec) *agent.EvidenceSeal {
	stream := workspaceid.PathFingerprint(workspace)
	if strings.TrimSpace(stateRoot) == "" || stream == "" {
		return nil
	}
	dir := filepath.Join(stateRoot, builtin.TrustedStateDir)
	integrity := func() trustedstate.IntegrityLevel {
		if !plugin.LaunchedUnconfinedProcess() && !sidecar.LaunchedUnconfinedProcess() && sandbox.WriteProtects(bash, dir) {
			return trustedstate.HostProtected
		}
		return trustedstate.TamperEvidentOnly
	}
	store := trustedstate.Open(dir, integrity)
	return &agent.EvidenceSeal{Store: store, Stream: stream, Root: workspace, Observer: observation.NewObserver(store, observation.DefaultPolicy(stateRoot))}
}
