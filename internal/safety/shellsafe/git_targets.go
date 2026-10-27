package shellsafe

import (
	"slices"
	"strings"

	"reasonix/internal/base/shellparse"
)

// gitContentReaders are the read subcommands that look at file content or the
// working tree, so a repository's own configuration can make them run a
// program. The others handle refs and objects and never convert content.
var gitContentReaders = []string{"diff", "log", "show", "blame", "whatchanged", "grep", "status", "ls-files", "cat-file", "describe", "stash"}

// GitContentReadTargets lists, for each git call in command that reads content,
// the directory its -C names ("" for the working directory). unknown is true
// when the call cannot be tied to a repository the host can inspect: another
// repository chosen with --git-dir, config handed in with -c, or a directory
// change earlier in the same statement.
func GitContentReadTargets(command string) (dirs []string, unknown bool) {
	leaves, _, ok := shellparse.CompoundLeaves(command)
	if !ok {
		argv, malformed := shellparse.StaticFields(command)
		if malformed != "" {
			return nil, false
		}
		leaves = [][]string{argv}
	}
	moved := false
	for _, argv := range leaves {
		if len(argv) == 0 {
			continue
		}
		switch ExecutableBase(argv[0]) {
		case "cd", "pushd", "popd":
			moved = true
			continue
		case "git":
		default:
			continue
		}
		dir, sub, redirected := gitCallTarget(argv)
		if !slices.Contains(gitContentReaders, sub) {
			continue
		}
		if redirected || moved {
			unknown = true
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs, unknown
}

// gitCallTarget reads git's global options up to the subcommand.
func gitCallTarget(argv []string) (dir, sub string, redirected bool) {
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "--no-pager" || arg == "-P":
		case arg == "-C" && i+1 < len(argv):
			i++
			dir = argv[i]
		case strings.HasPrefix(arg, "-"):
			// An option this does not know may take the next word as its value,
			// so the subcommand is the first later word that names a reader.
			for _, rest := range argv[i+1:] {
				if slices.Contains(gitContentReaders, strings.ToLower(rest)) {
					return dir, strings.ToLower(rest), true
				}
			}
			return dir, "", true
		default:
			return dir, strings.ToLower(arg), redirected
		}
	}
	return dir, "", redirected
}
