package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/hook"
	"reasonix/internal/session/control"
)

type trustOptions struct {
	dir    string
	yes    bool
	revoke bool
}

func trustCommand(args []string) int {
	return runTrust(args, bufio.NewScanner(os.Stdin), os.Stdout, isInteractive())
}

// runTrust trusts a workspace folder — edits inside it then run without asking
// where the OS sandbox confines them — and approves the programs its own
// files name, as they stand now. Both are recorded under the Reasonix home.
func runTrust(args []string, in *bufio.Scanner, out io.Writer, interactive bool) int {
	opts, err := parseTrustOptions(args)
	if err != nil {
		fmt.Fprintln(out, err)
		return 2
	}
	root := boot.ResolveWorkspaceRoot(opts.dir)
	roots := config.Roots{}
	store := config.NewProjectProgramStore(roots.Home())
	grants := config.NewProjectGrantStore(roots.Home())
	if opts.revoke {
		if err := store.Revoke(root); err != nil {
			fmt.Fprintln(out, "revoke:", err)
			return 1
		}
		if err := grants.SetTrust(root, config.WorkspaceTrustUndecided); err != nil {
			fmt.Fprintln(out, "revoke:", err)
			return 1
		}
		fmt.Fprintf(out, "Revoked the folder trust and every program approval for %s.\n", root)
		return 0
	}
	pending, err := pendingProjectPrograms(roots, root)
	if err != nil {
		fmt.Fprintln(out, "load:", err)
		return 1
	}
	trust, _ := grants.Trust(root)
	canTrust := control.TrustableFolder(root)
	if !canTrust {
		fmt.Fprintf(out, "%s is a filesystem root, holds a home directory, or holds Reasonix's own files; it is not trusted as a whole.\n", root)
	}
	if len(pending) == 0 && (trust == config.WorkspaceTrusted || !canTrust) {
		fmt.Fprintf(out, "Nothing in %s is waiting for approval.\n", root)
		return 0
	}
	if trust != config.WorkspaceTrusted && canTrust {
		fmt.Fprintf(out, "Trusting %s lets edits and shell commands in it run without asking, in the terminal UI and in `reasonix run`.\n%s\n", root, trustScope)
	}
	if len(pending) > 0 {
		fmt.Fprintf(out, "%s names programs Reasonix would run on your machine:\n", root)
		for _, p := range pending {
			fmt.Fprintf(out, "  [%s] %s\n      %s\n      declaration: %s\n", p.Kind, p.Name, p.Detail, p.Declaration)
			for _, f := range p.Files {
				fmt.Fprintf(out, "      file (content checked): %s\n", f)
			}
		}
	}
	if !opts.yes {
		if !interactive {
			fmt.Fprintln(out, "Nothing approved. Review the above, then run `reasonix trust --yes` here to approve.")
			return 1
		}
		if answer := ask(in, out, "Trust this folder and approve what it names, as it is now?", "y/N"); !strings.EqualFold(strings.TrimSpace(answer), "y") {
			fmt.Fprintln(out, "Nothing approved.")
			return 1
		}
	}
	if len(pending) > 0 {
		if err := store.Approve(root, pending...); err != nil {
			fmt.Fprintln(out, "approve:", err)
			return 1
		}
	}
	if canTrust {
		if err := grants.SetTrust(root, config.WorkspaceTrusted); err != nil {
			fmt.Fprintln(out, "trust:", err)
			return 1
		}
	}
	fmt.Fprintln(out, "Approved. Any change to a program it names needs approval again.")
	return 0
}

func pendingProjectPrograms(roots config.Roots, root string) ([]config.ProjectProgram, error) {
	cfg, err := roots.LoadForRootReadOnly(root)
	if err != nil {
		return nil, err
	}
	pending := cfg.PendingProjectPrograms()
	if p, ok := hook.PendingProjectHooks(hook.LoadOptions{ProjectRoot: root, ReasonixHomeDir: roots.Home()}); ok {
		pending = append(pending, p)
	}
	return pending, nil
}

func parseTrustOptions(args []string) (trustOptions, error) {
	var opts trustOptions
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--yes" || arg == "-y":
			opts.yes = true
		case arg == "--revoke":
			opts.revoke = true
		case arg == "--dir" && i+1 < len(args):
			i++
			opts.dir = args[i]
		case strings.HasPrefix(arg, "--dir="):
			opts.dir = strings.TrimPrefix(arg, "--dir=")
		default:
			return opts, fmt.Errorf("usage: reasonix trust [--dir PATH] [--yes|--revoke] (unknown argument %q)", arg)
		}
	}
	if opts.yes && opts.revoke {
		return opts, fmt.Errorf("usage: reasonix trust [--dir PATH] [--yes|--revoke] (choose one)")
	}
	return opts, nil
}
