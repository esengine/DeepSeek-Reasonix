package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/remote"
	"reasonix/internal/platform/remote/bootstrap"
)

// remoteCommand dispatches `reasonix remote <sub>`, mirroring mcpCommand.
func remoteCommand(args []string, version string) int {
	if len(args) == 0 {
		remoteUsage()
		return 2
	}
	switch args[0] {
	case "add":
		return remoteAddCLI(args[1:])
	case "list", "ls":
		return remoteListCLI()
	case "remove", "rm":
		return remoteRemoveCLI(args[1:])
	case "import":
		return remoteImportCLI(args[1:])
	case "test":
		return remoteTestCLI(args[1:], version)
	case "connect", "open":
		return remoteConnectCLI(args, version)
	case "status":
		return remoteStatusCLI(args[1:])
	case "forward":
		return remoteForwardCLI(args[1:])
	case "serve":
		return remoteServeCLI(args[1:], version)
	case "fs":
		return remoteFSCLI(args[1:])
	case "attach-workspace":
		return remoteAttachWorkspaceCLI(args[1:], version)
	case "runtime-workbench":
		return remoteRuntimeWorkbenchCLI(args[1:], version)
	case "workbench-build-id":
		return remoteWorkbenchBuildIDCLI(args[1:], version)
	case "help", "-h", "--help":
		remoteUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown remote subcommand %q\n\n", args[0])
		remoteUsage()
		return 2
	}
}

// The Remote Workbench protocol and its hidden subcommands were removed. The
// command names stay routable for one release so old scripts and launchers fail
// with an actionable message instead of "unknown subcommand"; the following
// stable release deletes the stubs and the routes entirely.
func removedWorkbenchCommand(name string) int {
	fmt.Fprintf(os.Stderr, "reasonix remote %s: Remote Workbench 已移除，请使用 `reasonix remote connect <host> --open`\n", name)
	return 1
}

func remoteAttachWorkspaceCLI(args []string, version string) int {
	return removedWorkbenchCommand("attach-workspace")
}
func remoteRuntimeWorkbenchCLI(args []string, version string) int {
	return removedWorkbenchCommand("runtime-workbench")
}
func remoteWorkbenchBuildIDCLI(args []string, version string) int {
	return removedWorkbenchCommand("workbench-build-id")
}

// editUserConfig runs mutate against the user-global config file under the edit
// lock and saves it there. Remote hosts are user-global (pinned in
// LoadForRoot), so they must never be written to a project reasonix.toml.
func editUserConfig(mutate func(*config.Config) error) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("cannot resolve user config path")
	}
	cfg := config.LoadForEdit(path)
	if cfg == nil {
		cfg = config.Default()
	}
	if err := mutate(cfg); err != nil {
		return err
	}
	return cfg.SaveTo(path)
}

const remoteAddUsage = "usage: reasonix remote add <name> [user@]host[:port] [flags]"

func remoteAddCLI(args []string) int {
	// Positionals come first (name, target); Go's flag package stops at the
	// first non-flag argument, so the flags are parsed from what follows.
	if commandHelpRequested(args, 2) {
		fmt.Fprintln(os.Stdout, remoteAddUsage)
		return 0
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, remoteAddUsage)
		return 2
	}
	name, target := args[0], args[1]
	fs := newFlagSet("remote add")
	identity := fs.String("identity", "", "path to a private key file")
	jump := fs.String("jump", "", "ProxyJump chain (OpenSSH syntax)")
	workspace := fs.String("workspace", "", "default remote workspace directory")
	useSSHConfig := fs.Bool("use-ssh-config", false, "layer ~/.ssh/config values under unset fields")
	serveInstall := fs.String("serve-install", "auto", "remote CLI install strategy: auto|npm|upload|never")
	passphraseEnv := fs.String("passphrase-env", "", "env var name holding the key passphrase")
	passwordEnv := fs.String("password-env", "", "env var name holding the login password")
	if code, ok := parseCommandFlags(fs, args[2:]); !ok {
		return code
	}
	user, host, port, err := remote.ParseTarget(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	entry := config.RemoteHostEntry{
		Name:          name,
		Host:          host,
		Port:          port,
		User:          user,
		IdentityFile:  *identity,
		ProxyJump:     *jump,
		Workspace:     *workspace,
		ServeInstall:  *serveInstall,
		UseSSHConfig:  *useSSHConfig,
		PassphraseEnv: *passphraseEnv,
		PasswordEnv:   *passwordEnv,
	}
	if err := config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		var removalCandidates []string
		if existing, ok := c.RemoteHost(entry.Name); ok {
			for _, key := range []string{existing.PasswordEnv, existing.PassphraseEnv} {
				if config.IsGeneratedRemoteCredential(entry.Name, key) {
					removalCandidates = append(removalCandidates, key)
				}
			}
		}
		if err := c.UpsertRemoteHost(entry); err != nil {
			return nil, err
		}
		return config.UnusedGeneratedRemoteCredentialChanges(c, removalCandidates), nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	fmt.Printf("added remote host %q (%s)\n", name, target)
	return 0
}

func remoteListCLI() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	if len(cfg.Remote.Hosts) == 0 {
		fmt.Println(i18n.M.RemoteNoHostsHint)
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTARGET\tWORKSPACE\tFORWARDS\tSSH-CONFIG")
	for _, h := range cfg.Remote.Hosts {
		target := h.Host
		if h.User != "" {
			target = h.User + "@" + target
		}
		if h.Port != 0 && h.Port != 22 {
			target = fmt.Sprintf("%s:%d", target, h.Port)
		}
		// One machine holds several projects; the column names the default and
		// says how many more are listed, the same way FORWARDS is a count.
		ws := h.Workspace
		if ws == "" {
			ws = "-"
		}
		if extra := len(h.WorkspaceList()) - 1; extra > 0 {
			ws = fmt.Sprintf("%s (+%d)", ws, extra)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%v\n", h.Name, target, ws, len(h.Forwards), h.UseSSHConfig)
	}
	_ = w.Flush()
	return 0
}

func remoteRemoveCLI(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote remove <name>")
		return 2
	}
	name := args[0]
	removed := false
	if err := config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		var removalCandidates []string
		if existing, ok := c.RemoteHost(name); ok {
			for _, key := range []string{existing.PasswordEnv, existing.PassphraseEnv} {
				if config.IsGeneratedRemoteCredential(name, key) {
					removalCandidates = append(removalCandidates, key)
				}
			}
		}
		removed = c.RemoveRemoteHost(name)
		return config.UnusedGeneratedRemoteCredentialChanges(c, removalCandidates), nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	if !removed {
		fmt.Fprintf(os.Stderr, "no remote host named %q\n", name)
		return 1
	}
	fmt.Printf("removed remote host %q\n", name)
	return 0
}

func remoteImportCLI(args []string) int {
	fs := newFlagSet("remote import")
	all := fs.Bool("all", false, "import every concrete ~/.ssh/config alias")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	src, err := remote.LoadUserSSHConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	candidates := src.Aliases()
	if len(candidates) == 0 {
		fmt.Println("no importable aliases found in ~/.ssh/config")
		return 0
	}
	wanted := map[string]bool{}
	for _, a := range fs.Args() {
		wanted[a] = true
	}
	imported := 0
	err = config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		for _, cand := range candidates {
			if !*all && len(wanted) > 0 && !wanted[cand.Alias] {
				continue
			}
			if !*all && len(wanted) == 0 {
				continue // neither --all nor explicit aliases: nothing to do
			}
			entry := config.RemoteHostEntry{
				Name:         cand.Alias,
				Host:         cand.Alias,
				UseSSHConfig: true,
			}
			if existing, ok := c.RemoteHost(entry.Name); ok {
				entry.PassphraseEnv = existing.PassphraseEnv
				entry.PasswordEnv = existing.PasswordEnv
				entry.Workspace = existing.Workspace
				entry.Workspaces = append([]string(nil), existing.Workspaces...)
				entry.ServeInstall = existing.ServeInstall
				entry.Forwards = append([]config.RemoteForwardEntry(nil), existing.Forwards...)
			}
			if err := c.UpsertRemoteHost(entry); err != nil {
				return nil, err
			}
			imported++
		}
		return nil, nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	if imported == 0 {
		fmt.Println("nothing imported; pass alias names or --all")
		remotePrintImportCandidates(candidates)
		return 0
	}
	fmt.Printf("imported %d host(s) from ~/.ssh/config\n", imported)
	return 0
}

func remotePrintImportCandidates(cands []remote.ImportedHost) {
	fmt.Println("available aliases:")
	for _, c := range cands {
		fmt.Printf("  %s\n", c.Alias)
	}
}

func remoteTestCLI(args []string, version string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote test <name|user@host>")
		return 2
	}
	client, cleanup, err := buildRemoteClient(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer client.Close()
	fmt.Println("connection OK")
	// Every missing piece in one session, under exactly the options connect
	// installs with. Wired differently it answers a different question: this
	// one called the release route closed for a resolver connect does pass.
	rep, err := bootstrap.Probe(ctx, client, bootstrap.Options{
		Install:         remoteInstallMode(args[0]),
		LocalBinary:     currentExecutable(),
		LocalGOOS:       runtime.GOOS,
		LocalGOARCH:     runtime.GOARCH,
		ProductVersion:  version,
		FetchBinary:     fetchRemoteCLIBinary,
		ResolveDownload: resolveRemoteCLIDownload,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	printRemoteReport(rep)
	if !rep.Ready() {
		return 1
	}
	return 0
}

// remoteInstallMode is the configured strategy when the argument named a host
// in the book; a bare user@host was never configured and takes the default.
func remoteInstallMode(name string) string {
	cfg, err := config.Load()
	if err != nil {
		return ""
	}
	entry, ok := cfg.RemoteHost(name)
	if !ok {
		return ""
	}
	return entry.ServeInstallMode()
}

func printRemoteReport(rep bootstrap.Report) {
	fmt.Printf("  machine   %s/%s, home %s\n", rep.OS, rep.Arch, rep.Home)
	switch {
	case rep.Kernel != "" && rep.Version != "":
		fmt.Printf("  reasonix  %s %s\n", rep.Kernel, rep.Version)
	case rep.Kernel != "":
		// It answered the flag probe and not the version one. Saying so beats
		// printing a path with a blank after it.
		fmt.Printf("  reasonix  %s (it did not report a version)\n", rep.Kernel)
	case rep.Outdated != "":
		fmt.Printf("  reasonix  %s — older than %s, a connect would replace it\n", rep.Outdated, bootstrap.MinPaneVersion)
	default:
		fmt.Println("  reasonix  none yet")
	}
	if rep.NPM != "" {
		fmt.Printf("  npm       %s\n", rep.NPM)
	} else {
		fmt.Println("  npm       not available")
	}
	// Indented under one heading: the rows above are what the machine has, and
	// these are ways to put a kernel on it. Flat, "npm" appeared twice meaning
	// two different things.
	label := "  install "
	for _, route := range rep.Routes {
		if route.OK() {
			fmt.Printf("%s %-9s open\n", label, route.Name)
		} else {
			fmt.Printf("%s %-9s %v\n", label, route.Name, route.Err)
		}
		label = "          "
	}
	if rep.Ready() {
		fmt.Println("ready — a connect would find or install a kernel here")
		return
	}
	fmt.Println("not ready — install reasonix on that machine, or open one of the routes above")
}

func remoteUsage() {
	fmt.Println(`Manage remote SSH hosts and their persistent serve (user-global config).

Usage:
  reasonix remote add <name> [user@]host[:port] [--identity F] [--jump SPEC]
                     [--workspace PATH] [--use-ssh-config] [--serve-install auto|npm|upload|never]
                     [--passphrase-env NAME] [--password-env NAME]
  reasonix remote list
  reasonix remote remove <name>
  reasonix remote import [alias...|--all]      # from ~/.ssh/config
  reasonix remote test <name|user@host>        # dial, auth, host key, and whether a kernel can run there
  reasonix remote connect <name> [--workspace PATH] [--local-port N] [--no-serve] [--open] [--forward-only]
  reasonix remote open <name>                  # connect --open
  reasonix remote status [<name>]
  reasonix remote forward add <host> (-L|-R) <spec> | forward rm <host> <name> | forward ls <host>
  reasonix remote serve start|stop|status|logs <name> [--workspace PATH] [-n N]
  reasonix remote fs ls <name>:<path> | fs get <name>:<remote> [local] | fs put <local> <name>:<remote>`)
}
