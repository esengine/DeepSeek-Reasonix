package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/hook"
)

type trustOptions struct {
	dir    string
	yes    bool
	revoke bool
}

func trustCommand(args []string) int {
	return runTrust(args, bufio.NewScanner(os.Stdin), os.Stdout, isInteractive())
}

// runTrust lists the programs a workspace's own files name and records the
// person's approval of them, as they stand now, under their Reasonix home.
func runTrust(args []string, in *bufio.Scanner, out io.Writer, interactive bool) int {
	opts, err := parseTrustOptions(args)
	if err != nil {
		fmt.Fprintln(out, err)
		return 2
	}
	root := boot.ResolveWorkspaceRoot(opts.dir)
	store := config.NewProjectProgramStore(config.ReasonixHomeDir())
	if opts.revoke {
		if err := store.Revoke(root); err != nil {
			fmt.Fprintln(out, "revoke:", err)
			return 1
		}
		fmt.Fprintf(out, "Revoked every program approval for %s.\n", root)
		return 0
	}
	pending, err := pendingProjectPrograms(root)
	if err != nil {
		fmt.Fprintln(out, "load:", err)
		return 1
	}
	if len(pending) == 0 {
		fmt.Fprintf(out, "Nothing in %s is waiting for approval.\n", root)
		return 0
	}
	fmt.Fprintf(out, "%s names programs Reasonix would run on your machine:\n", root)
	for _, p := range pending {
		fmt.Fprintf(out, "  [%s] %s\n      %s\n      declaration: %s\n", p.Kind, p.Name, p.Detail, p.Declaration)
		for _, f := range p.Files {
			fmt.Fprintf(out, "      file (content checked): %s\n", f)
		}
	}
	if !opts.yes {
		if !interactive {
			fmt.Fprintln(out, "Nothing approved. Review them, then run `reasonix trust --yes` here to approve.")
			return 1
		}
		if answer := ask(in, out, "Approve them as they are now?", "y/N"); !strings.EqualFold(strings.TrimSpace(answer), "y") {
			fmt.Fprintln(out, "Nothing approved.")
			return 1
		}
	}
	if err := store.Approve(root, pending...); err != nil {
		fmt.Fprintln(out, "approve:", err)
		return 1
	}
	fmt.Fprintln(out, "Approved. Any change to them needs approval again.")
	return 0
}

func pendingProjectPrograms(root string) ([]config.ProjectProgram, error) {
	cfg, err := config.LoadForRootReadOnly(root)
	if err != nil {
		return nil, err
	}
	pending := cfg.PendingProjectPrograms()
	if p, ok := hook.PendingProjectHooks(hook.LoadOptions{ProjectRoot: root}); ok {
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
