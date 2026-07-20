package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
	"reasonix/internal/remote"
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
		return remoteTestCLI(args[1:])
	case "connect", "open":
		return remoteConnectCLI(args, version)
	case "status":
		return remoteStatusCLI(args[1:])
	case "forward":
		return remoteForwardCLI(args[1:])
	case "serve":
		return remoteServeCLI(args[1:])
	case "fs":
		return remoteFSCLI(args[1:])
	case "help", "-h", "--help":
		remoteUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown remote subcommand %q\n\n", args[0])
		remoteUsage()
		return 2
	}
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

func remoteAddCLI(args []string) int {
	// Positionals come first (name, target); Go's flag package stops at the
	// first non-flag argument, so the flags are parsed from what follows.
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: reasonix remote add <name> [user@]host[:port] [flags]")
		return 2
	}
	name, target := args[0], args[1]
	fs := newFlagSet("remote add")
	identity := fs.String("identity", "", "path to a private key file")
	jump := fs.String("jump", "", "ProxyJump chain (OpenSSH syntax)")
	workspace := fs.String("workspace", "", "default remote workspace directory")
	useSSHConfig := fs.Bool("use-ssh-config", false, "layer ~/.ssh/config values under unset fields")
	serveInstall := fs.String("serve-install", "auto", "serve install strategy: auto|npm|upload|never")
	passphraseEnv := fs.String("passphrase-env", "", "env var name holding the key passphrase")
	passwordEnv := fs.String("password-env", "", "env var name holding the login password")
	if err := fs.Parse(args[2:]); err != nil {
		return 2
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
		ws := h.Workspace
		if ws == "" {
			ws = "-"
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
	if err := fs.Parse(args); err != nil {
		return 2
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

func remoteTestCLI(args []string) int {
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
	res, err := client.Exec(ctx, "uname -sm && whoami")
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	fmt.Printf("connection OK\n%s", res.Stdout)
	return 0
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
  reasonix remote test <name|user@host>        # dial + auth + host-key check
  reasonix remote connect <name> [--workspace PATH] [--local-port N] [--no-serve] [--open] [--forward-only]
  reasonix remote open <name>                  # connect --open
  reasonix remote status [<name>]
  reasonix remote forward add <host> (-L|-R) <spec> | forward rm <host> <name> | forward ls <host>
  reasonix remote serve start|stop|status|logs <name> [--workspace PATH] [-n N]
  reasonix remote fs ls <name>:<path> | fs get <name>:<remote> [local] | fs put <local> <name>:<remote>`)
}
