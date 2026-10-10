package gitcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

// ErrRepositoryDrivers reports that an invocation was not started because the
// repository's driver configuration could not be listed or neutralized.
var ErrRepositoryDrivers = errors.New("gitcmd: repository-defined git drivers cannot be neutralized")

// repositoryScopes are the config scopes a repository's author controls;
// includes are reported under the scope of the file that includes them.
var repositoryScopes = []string{"local", "worktree"}

// driverFree are the subcommands that never convert file content or recurse
// into submodules, whatever their options: they handle refs, the index and
// tree objects, not working-tree content. Anything not named here is listed,
// so an unknown or content-reading subcommand fails closed.
var driverFree = []string{"version", "rev-parse", "symbolic-ref", "write-tree", "ls-tree", "rev-list", "show-ref", "for-each-ref", "merge-base"}

// driverOverrides returns -c entries that empty every filter and merge driver
// defined at repository scope for the repository args will operate on, and
// turn off a repository-set submodule.recurse. The
// listing runs with the same leading global options, so -C and --git-dir
// resolve the same repository the invocation will.
func driverOverrides(ctx context.Context, dir string, repoEnv, args []string) ([]string, error) {
	sub := subcommandIndex(args)
	if sub < 0 || slices.Contains(driverFree, args[sub]) {
		return nil, nil
	}
	query := []string{"config", "--name-only", "-z", "--get-regexp", `^(filter|merge)\.|^submodule\.recurse$`}
	scoped := slices.Insert(slices.Clone(query), 1, "--show-scope")
	out, err := listConfig(ctx, dir, repoEnv, args[:sub], scoped)
	if exitCode(err) == 129 {
		// git before 2.26 has no --show-scope; every definition is then
		// treated as repository-authored.
		out, err = listConfig(ctx, dir, repoEnv, args[:sub], query)
		if err == nil {
			return overridesFor(out, false)
		}
	}
	if exitCode(err) == 1 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRepositoryDrivers, err)
	}
	return overridesFor(out, true)
}

func listConfig(ctx context.Context, dir string, repoEnv, globals, query []string) ([]byte, error) {
	cmd := newCommand(ctx, Args(dir, nil, append(slices.Clone(globals), query...)...), repoEnv)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && exitCode(err) != 1 {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, err
}

func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 0
}

// overridesFor turns NUL-separated config key names — each preceded by its
// scope when scoped — into -c entries. Keys without a subsection (merge.ff)
// name no driver.
func overridesFor(out []byte, scoped bool) ([]string, error) {
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	var overrides []string
	seen := map[string]bool{}
	step := 1
	if scoped {
		step = 2
	}
	for i := 0; i+step-1 < len(fields); i += step {
		key := fields[i+step-1]
		if scoped && !slices.Contains(repositoryScopes, fields[i]) {
			continue
		}
		if key == "submodule.recurse" {
			// Only a repository-authored value is overridden, so a user's
			// own global choice stays in force outside such repositories.
			if !seen[key] {
				seen[key] = true
				overrides = append(overrides, "submodule.recurse=false")
			}
			continue
		}
		section, rest, _ := strings.Cut(key, ".")
		dot := strings.LastIndexByte(rest, '.')
		if dot <= 0 {
			continue
		}
		name := rest[:dot]
		if seen[section+"."+name] {
			continue
		}
		seen[section+"."+name] = true
		// -c splits at the first '=', so such a name cannot be addressed.
		if strings.ContainsRune(name, '=') {
			return nil, fmt.Errorf("%w: driver %q", ErrRepositoryDrivers, section+"."+name)
		}
		prefix := section + "." + name + "."
		switch section {
		case "filter":
			// An empty command is no filter; required=false keeps the
			// now-absent filter from failing the invocation.
			overrides = append(overrides, prefix+"clean=", prefix+"smudge=", prefix+"process=", prefix+"required=false")
		case "merge":
			// An empty driver fails the merge of that path as a conflict.
			overrides = append(overrides, prefix+"driver=")
		}
	}
	return overrides, nil
}
