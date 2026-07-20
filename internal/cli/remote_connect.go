package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
	"reasonix/internal/netclient"
	"reasonix/internal/remote"
	"reasonix/internal/remote/bootstrap"
	"reasonix/internal/remote/forward"
)

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return fs
}

// buildRemoteClient resolves nameOrTarget against config + ~/.ssh/config and
// returns a not-yet-started remote.Client with terminal-based secret and
// host-key prompts. cleanup releases transient resources.
func buildRemoteClient(nameOrTarget string) (*remote.Client, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	sshCfg, err := remote.LoadUserSSHConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("load SSH config: %w", err)
	}
	host, err := remote.ResolveHost(cfg, nameOrTarget, sshCfg)
	if err != nil {
		return nil, nil, err
	}

	auth := remoteAuthForHost(host, terminalSecretPrompt)
	resolvedJumps, err := remote.ResolveJumpHosts(cfg, host.ProxyJump, sshCfg)
	if err != nil {
		return nil, nil, err
	}
	jumpHosts := make([]remote.JumpHostOptions, 0, len(resolvedJumps))
	for _, jump := range resolvedJumps {
		jumpHosts = append(jumpHosts, remote.JumpHostOptions{
			Host: jump,
			Auth: remoteAuthForHost(jump, terminalSecretPrompt),
		})
	}

	policy := &remote.HostKeyPolicy{Prompt: terminalHostKeyPrompt}

	// A misconfigured proxy is surfaced, not silently bypassed: a proxy is often
	// a policy requirement, and quietly dialing direct could exfiltrate the
	// connection around it.
	dialer, derr := netclient.NewStreamDialer(cfg.NetworkProxySpec())
	if derr != nil {
		return nil, nil, fmt.Errorf("remote: network proxy is misconfigured: %w", derr)
	}
	client, err := remote.New(remote.Options{
		Host:      host,
		Auth:      auth,
		JumpHosts: jumpHosts,
		HostKeys:  policy,
		Dialer:    dialer,
	})
	if err != nil {
		return nil, nil, err
	}
	return client, func() {}, nil
}

func remoteAuthForHost(host remote.ResolvedHost, prompt remote.SecretPrompt) remote.AuthOptions {
	auth := remote.AuthOptions{SecretPrompt: prompt}
	if host.PassphraseEnv != "" {
		env := host.PassphraseEnv
		auth.Passphrase = func() (string, error) { return config.ResolveCredential(env).Value, nil }
	}
	if host.PasswordEnv != "" {
		env := host.PasswordEnv
		auth.Password = func() (string, error) { return config.ResolveCredential(env).Value, nil }
	}
	return auth
}

func terminalSecretPrompt(_ context.Context, kind remote.SecretKind, host, identityFile string) (string, error) {
	var label string
	switch kind {
	case remote.SecretPassword:
		label = fmt.Sprintf(i18n.M.RemotePasswordPromptFmt, host)
	default:
		label = fmt.Sprintf(i18n.M.RemotePassphrasePromptFmt, host)
		if identityFile != "" {
			label += " (" + filepath.Base(identityFile) + ")"
		}
	}
	fmt.Fprint(os.Stderr, label+" ")
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("cannot prompt for %s: not a terminal", kind)
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func terminalHostKeyPrompt(_ context.Context, q remote.HostKeyQuestion) (bool, error) {
	fmt.Fprintf(os.Stderr, i18n.M.RemoteHostKeyPromptFmt+"\n", q.Host, q.KeyType, q.Fingerprint)
	fmt.Fprint(os.Stderr, "Accept and continue? [y/N] ")
	var answer string
	_, _ = fmt.Fscanln(os.Stdin, &answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

// remoteConnectCLI runs a foreground supervisor: connect, bootstrap serve,
// forward the serve port and configured forwards, and hold the tunnel until
// Ctrl-C. The remote serve keeps running after disconnect.
func remoteConnectCLI(args []string, version string) int {
	// args[0] is "connect" or "open".
	openAlias := args[0] == "open"
	fs := newFlagSet("remote connect")
	workspace := fs.String("workspace", "", "remote workspace directory")
	localPort := fs.Int("local-port", 0, "local port to bind for the serve tunnel (0 = auto)")
	noServe := fs.Bool("no-serve", false, "only establish forwards; do not bootstrap serve")
	open := fs.Bool("open", openAlias, "print/open the serve URL")
	forwardOnly := fs.Bool("forward-only", false, "apply configured forwards only; no serve")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote connect <name> [flags]")
		return 2
	}
	name := rest[0]

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	entry, _ := cfg.RemoteHost(name)
	ws := *workspace
	if ws == "" {
		ws = entry.Workspace
	}

	client, cleanup, err := buildRemoteClient(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer cleanup()

	// Live status line.
	client.Subscribe(func(ev remote.StatusEvent) {
		switch ev.Status {
		case remote.StatusConnecting:
			fmt.Fprintf(os.Stderr, i18n.M.RemoteConnectingFmt+"\n", name)
		case remote.StatusConnected:
			fmt.Fprintf(os.Stderr, i18n.M.RemoteConnectedFmt+"\n", name)
		case remote.StatusReconnecting:
			fmt.Fprintf(os.Stderr, i18n.M.RemoteReconnectingFmt+"\n", name, ev.Attempt)
		case remote.StatusDegraded:
			fmt.Fprintf(os.Stderr, i18n.M.RemoteDegradedFmt+"\n", name)
		case remote.StatusStopped:
			if ev.Err != nil {
				fmt.Fprintf(os.Stderr, "%s %v\n", i18n.M.ErrorPrefix, ev.Err)
			}
		}
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := client.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer client.Close()

	// Apply configured forwards.
	if err := applyConfiguredForwards(client, entry); err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", i18n.M.ErrorPrefix, err)
	}

	if !*noServe && !*forwardOnly {
		res, err := bootstrap.EnsureServe(ctx, client, bootstrap.Options{
			Workspace:   ws,
			Install:     entry.ServeInstallMode(),
			LocalBinary: currentExecutable(),
			LocalGOOS:   runtime.GOOS,
			LocalGOARCH: runtime.GOARCH,
			MinVersion:  bootstrap.MinServeVersion,
			Progress: func(step, detail string) {
				fmt.Fprintf(os.Stderr, i18n.M.RemoteBootstrapStepFmt+"\n", step, detail)
			},
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		localURL, ferr := forwardServe(client, res.State.Addr, *localPort, res.Token)
		if ferr != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, ferr)
			return 1
		}
		fmt.Printf(i18n.M.RemoteServeReadyFmt+"\n", localURL)
		if *open {
			_ = openInBrowser(localURL)
		}
	}

	fmt.Fprintln(os.Stderr, "Press Ctrl-C to disconnect (remote serve keeps running).")
	<-ctx.Done()
	fmt.Fprintln(os.Stderr, i18n.M.RemoteDisconnected)
	return 0
}

func applyConfiguredForwards(client *remote.Client, entry config.RemoteHostEntry) error {
	set := client.Forwards()
	var firstErr error
	for _, f := range entry.Forwards {
		dir := forward.Local
		if strings.EqualFold(f.Type, "remote") {
			dir = forward.Remote
		}
		spec := forward.Spec{Direction: dir, BindAddr: normalizeBind(f.Bind), TargetAddr: f.Target}
		if _, err := set.Add(spec); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// forwardServe adds the reserved "serve" local forward to the remote serve
// address and returns the local URL (with token).
func forwardServe(client *remote.Client, remoteAddr string, localPort int, token string) (string, error) {
	bind := "127.0.0.1:0"
	if localPort > 0 {
		bind = fmt.Sprintf("127.0.0.1:%d", localPort)
	}
	bound, err := client.Forwards().Add(forward.Spec{
		Name:       "serve",
		Direction:  forward.Local,
		BindAddr:   bind,
		TargetAddr: remoteAddr,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://%s/?token=%s", bound, token), nil
}

func normalizeBind(bind string) string {
	if !strings.Contains(bind, ":") {
		return "127.0.0.1:" + bind
	}
	return bind
}

func currentExecutable() string {
	if p, err := os.Executable(); err == nil {
		return p
	}
	return ""
}

// remoteServeCLI: serve start|stop|status|logs <name>.
func remoteServeCLI(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote serve start|stop|status|logs <name> [--workspace PATH] [-n N]")
		return 2
	}
	action := args[0]
	fs := newFlagSet("remote serve")
	workspace := fs.String("workspace", "", "remote workspace directory")
	n := fs.Int("n", 200, "log lines to show (logs)")
	if err := fs.Parse(args[2:]); err != nil {
		return 2
	}
	name := args[1]
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	entry, _ := cfg.RemoteHost(name)
	ws := *workspace
	if ws == "" {
		ws = entry.Workspace
	}

	client, cleanup, err := buildRemoteClient(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer client.Close()

	switch action {
	case "start":
		res, err := bootstrap.EnsureServe(ctx, client, bootstrap.Options{
			Workspace: ws, Install: entry.ServeInstallMode(),
			LocalBinary: currentExecutable(), LocalGOOS: runtime.GOOS, LocalGOARCH: runtime.GOARCH,
			MinVersion: bootstrap.MinServeVersion,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Printf("serve running on remote %s (pid %d)\n", res.State.Addr, res.State.PID)
		return 0
	case "stop":
		if err := bootstrap.Stop(ctx, client, ws); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Println("serve stopped")
		return 0
	case "status":
		st, alive, err := bootstrap.Status(ctx, client, ws)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		if st.PID == 0 {
			fmt.Println("no serve recorded for this workspace")
			return 0
		}
		fmt.Printf("pid=%d addr=%s alive=%v workspace=%s\n", st.PID, st.Addr, alive, st.Workspace)
		return 0
	case "logs":
		if err := bootstrap.Logs(ctx, client, ws, *n, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown serve action %q\n", action)
		return 2
	}
}

// remoteStatusCLI prints configured host summaries; with a name it also does a
// brief liveness probe.
func remoteStatusCLI(args []string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	if len(args) == 0 {
		return remoteListCLI()
	}
	name := args[0]
	entry, ok := cfg.RemoteHost(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "no remote host named %q\n", name)
		return 1
	}
	fmt.Printf("host %s: %s@%s workspace=%s\n", entry.Name, entry.User, entry.Host, entry.Workspace)
	return 0
}

// remoteForwardCLI manages persisted forward rules (applied on next connect).
func remoteForwardCLI(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote forward add <host> (-L|-R) <spec> | rm <host> <name> | ls <host>")
		return 2
	}
	switch args[0] {
	case "ls":
		return remoteForwardLs(args[1])
	case "add":
		return remoteForwardAdd(args[1:])
	case "rm":
		return remoteForwardRm(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown forward action %q\n", args[0])
		return 2
	}
}

func remoteForwardLs(host string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	entry, ok := cfg.RemoteHost(host)
	if !ok {
		fmt.Fprintf(os.Stderr, "no remote host named %q\n", host)
		return 1
	}
	if len(entry.Forwards) == 0 {
		fmt.Println("no forwards configured")
		return 0
	}
	for _, f := range entry.Forwards {
		fmt.Printf("%s\t%s -> %s\n", f.Type, f.Bind, f.Target)
	}
	return 0
}

func remoteForwardAdd(args []string) int {
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote forward add <host> (-L|-R) <spec>")
		return 2
	}
	host := args[0]
	dir, err := forward.ParseDirection(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	spec, err := forward.ParseShorthand(dir, args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	if spec.NonLoopbackBind() {
		fmt.Fprintln(os.Stderr, "warning: bind address is not loopback; the forward will be reachable off-machine")
	}
	ftype := "local"
	if dir == forward.Remote {
		ftype = "remote"
	}
	found := false
	err = editUserConfig(func(c *config.Config) error {
		entry, ok := c.RemoteHost(host)
		if !ok {
			return fmt.Errorf("no remote host named %q", host)
		}
		entry.Forwards = append(entry.Forwards, config.RemoteForwardEntry{Type: ftype, Bind: spec.BindAddr, Target: spec.TargetAddr})
		found = true
		return c.UpsertRemoteHost(entry)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	_ = found
	fmt.Printf("added %s forward %s -> %s to %q (takes effect on next connect)\n", ftype, spec.BindAddr, spec.TargetAddr, host)
	return 0
}

func remoteForwardRm(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote forward rm <host> <bind>")
		return 2
	}
	host, bind := args[0], args[1]
	removed := false
	err := editUserConfig(func(c *config.Config) error {
		entry, ok := c.RemoteHost(host)
		if !ok {
			return fmt.Errorf("no remote host named %q", host)
		}
		kept := entry.Forwards[:0]
		for _, f := range entry.Forwards {
			if f.Bind == bind || normalizeBind(f.Bind) == normalizeBind(bind) {
				removed = true
				continue
			}
			kept = append(kept, f)
		}
		entry.Forwards = kept
		return c.UpsertRemoteHost(entry)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	if !removed {
		fmt.Fprintf(os.Stderr, "no forward bound to %q on %q\n", bind, host)
		return 1
	}
	fmt.Printf("removed forward %s from %q\n", bind, host)
	return 0
}

// remoteFSCLI: fs ls|get|put with <name>:<path> operands.
func remoteFSCLI(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote fs ls <name>:<path> | get <name>:<remote> [local] | put <local> <name>:<remote>")
		return 2
	}
	switch args[0] {
	case "ls":
		return remoteFSLs(args[1])
	case "get":
		return remoteFSGet(args[1:])
	case "put":
		return remoteFSPut(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown fs action %q\n", args[0])
		return 2
	}
}

func splitHostPath(s string) (host, p string, ok bool) {
	i := strings.Index(s, ":")
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

func withRemoteFS(name string, fn func(ctx context.Context, client *remote.Client) int) int {
	client, cleanup, err := buildRemoteClient(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer client.Close()
	return fn(ctx, client)
}

func remoteFSLs(operand string) int {
	host, p, ok := splitHostPath(operand)
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote fs ls <name>:<path>")
		return 2
	}
	return withRemoteFS(host, func(ctx context.Context, client *remote.Client) int {
		fsys, err := client.SFTP()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		entries, err := fsys.List(ctx, p)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		for _, e := range entries {
			suffix := ""
			if e.IsDir {
				suffix = "/"
			}
			fmt.Printf("%s%s\n", e.Name, suffix)
		}
		return 0
	})
}

func remoteFSGet(args []string) int {
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote fs get <name>:<remote> [local]")
		return 2
	}
	host, remotePath, ok := splitHostPath(args[0])
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote fs get <name>:<remote> [local]")
		return 2
	}
	localPath := path.Base(remotePath)
	if len(args) >= 2 {
		localPath = args[1]
	}
	return withRemoteFS(host, func(ctx context.Context, client *remote.Client) int {
		fsys, err := client.SFTP()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		// Stream the full file to disk — never the capped preview reader, which
		// would silently truncate large downloads.
		out, err := os.Create(localPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		n, err := fsys.Download(ctx, remotePath, out)
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(localPath)
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Printf("wrote %s (%d bytes)\n", localPath, n)
		return 0
	})
}

func remoteFSPut(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote fs put <local> <name>:<remote>")
		return 2
	}
	localPath := args[0]
	host, remotePath, ok := splitHostPath(args[1])
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote fs put <local> <name>:<remote>")
		return 2
	}
	in, err := os.Open(localPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer in.Close()
	return withRemoteFS(host, func(ctx context.Context, client *remote.Client) int {
		fsys, err := client.SFTP()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		n, err := fsys.UploadAtomic(ctx, remotePath, in, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Printf("uploaded %s -> %s (%d bytes)\n", localPath, remotePath, n)
		return 0
	})
}

func openInBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	c := exec.Command(cmd, args...)
	return c.Start()
}
