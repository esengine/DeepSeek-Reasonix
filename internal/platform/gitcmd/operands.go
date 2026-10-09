package gitcmd

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrOptionRefused reports that an argument is an option that makes git run a
// program the caller did not name. Command and CommandWithConfig refuse it at
// the exit, so a value that reached an argument list from a request, a
// manifest or a ref cannot turn into one however its caller built the list.
var ErrOptionRefused = errors.New("gitcmd: option runs a program and is refused")

var dashed = regexp.MustCompile(`^-.`)

// globalRefused are global options that load configuration or code from a
// place the caller did not name.
var globalRefused = []string{"-c", "--config-env", "--exec-path"}

// longRefused are long options that start a program or write a named file, for
// every subcommand. git accepts any unambiguous prefix of a long option, so
// each is matched by prefix.
var longRefused = []string{
	"--upload-pack", "--receive-pack", "--exec", "--config", "--ext-diff",
	"--textconv", "--output", "--open-files-in-pager", "--remote",
}

// shortRefused are short option letters that do the same, per subcommand. A
// letter is refused anywhere in a bundle such as -qu, because git reads the
// rest of the bundle as that option's value.
var shortRefused = map[string]string{
	"clone":     "uc",
	"fetch":     "u",
	"pull":      "u",
	"ls-remote": "u",
	"grep":      "O",
	"rebase":    "x",
}

// remoteOK are subcommands where --remote names a ref filter or a submodule
// mode rather than a program source.
var remoteOK = map[string]bool{"branch": true, "submodule": true, "for-each-ref": true}

// screen returns args unchanged unless one is a refused option. Arguments
// after "--" are operands and are never options. Every argument is classified
// by dashed first, so only a value that is not an option reaches out unchecked.
func screen(args []string) ([]string, error) {
	sub := subcommandIndex(args)
	out := make([]string, 0, len(args))
	operands := false
	for i, a := range args {
		if !dashed.MatchString(a) {
			out = append(out, a)
			continue
		}
		if operands {
			out = append(out, a)
			continue
		}
		if a == "--" {
			operands = true
			out = append(out, a)
			continue
		}
		if refusedOption(args, sub, i) {
			return nil, fmt.Errorf("%w: %s", ErrOptionRefused, a)
		}
		out = append(out, a)
	}
	return out, nil
}

func refusedOption(args []string, sub, i int) bool {
	name, _, _ := strings.Cut(args[i], "=")
	if sub < 0 || i < sub {
		return name == "-c" || globalName(name)
	}
	if i == sub {
		return false
	}
	if strings.HasPrefix(name, "--") {
		for _, denied := range longRefused {
			if len(name) > 2 && strings.HasPrefix(denied, name) {
				return denied != "--remote" || !remoteOK[args[sub]]
			}
		}
		return false
	}
	return strings.ContainsAny(name[1:], shortRefused[args[sub]])
}

func globalName(name string) bool {
	for _, g := range globalRefused[1:] {
		if len(name) > 2 && strings.HasPrefix(g, name) {
			return true
		}
	}
	return false
}
