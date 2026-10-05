package shellsafe

import "strings"

// argRules says, per read-only command, which arguments turn a call into one
// that writes or runs another program. An option named here in any spelling
// the program itself accepts — a clustered short flag, an abbreviated long
// one — is that option; the tables list programs, these list their escapes.
var argRules = map[string]func(sub string, args []string) bool{
	"find": func(_ string, args []string) bool {
		return hasAnyArg(args, "-exec", "-execdir", "-delete", "-ok", "-okdir", "-fls", "-fprint", "-fprint0", "-fprintf")
	},
	"sort": func(_ string, args []string) bool {
		return hasShortFlag(args, 'o') || hasLongOpt(args, "--output", 3) || hasLongOpt(args, "--compress-program", 4)
	},
	"git": gitArgsWrite,
	"go":  goArgsWrite,
	"gofmt": func(_ string, args []string) bool {
		return hasGoFlag(args, "w") || hasGoFlag(args, "cpuprofile")
	},
	"env": func(_ string, args []string) bool { return envRunsAProgram(args) },
	"rg": func(_ string, args []string) bool {
		return hasLongOpt(args, "--pre", 5) || hasLongOpt(args, "--hostname-bin", 14)
	},
	"uniq": func(_ string, args []string) bool { return len(operands(args, "-f", "-s", "-w")) > 1 },
	"file": func(_ string, args []string) bool { return hasShortFlag(args, 'C') || hasLongOpt(args, "--compile", 5) },
	"date": func(_ string, args []string) bool {
		rest := withoutValues(args, "-d", "--date", "-f", "--file", "-r", "--reference")
		return hasShortFlag(withoutPrefixed(rest, "-I"), 's') || hasLongOpt(args, "--set", 3)
	},
	"hostname": func(_ string, args []string) bool {
		return len(operands(args)) > 0 || hasShortFlag(args, 'F') || hasLongOpt(args, "--file", 4)
	},
	"npm":     func(sub string, args []string) bool { return sub == "audit" && len(args) > 0 && args[0] == "fix" },
	"kubectl": func(_ string, args []string) bool { return hasGoFlag(args, "kubeconfig") },
}

func gitArgsWrite(sub string, args []string) bool {
	switch sub {
	case "diff", "show", "log", "whatchanged":
		return hasLongOpt(args, "--output", 5)
	case "tag":
		return !gitTagIsListing(args)
	case "grep":
		return hasShortFlag(args, 'O') || hasLongOpt(args, "--open-files-in-pager", 4)
	case "reflog":
		return len(args) > 0 && args[0] != "show" && args[0] != "exists" && !strings.HasPrefix(args[0], "-")
	}
	return false
}

// goArgsWrite: -toolexec, -exec and -vettool hand the build a program to run,
// -mod may rewrite go.mod, and doc -http starts a server.
func goArgsWrite(sub string, args []string) bool {
	if sub == "env" && (hasGoFlag(args, "w") || hasGoFlag(args, "u")) {
		return true
	}
	for _, flag := range []string{"toolexec", "exec", "vettool", "mod", "http"} {
		if hasGoFlag(args, flag) {
			return true
		}
	}
	return false
}

// hasShortFlag reports a short option letter anywhere in a cluster such as
// `-no`; getopt reads the rest of the cluster as that option's value.
func hasShortFlag(args []string, letter byte) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if len(arg) > 1 && arg[0] == '-' && arg[1] != '-' && strings.IndexByte(arg[1:], letter) >= 0 {
			return true
		}
	}
	return false
}

// hasLongOpt reports canonical or any abbreviation of it at least minLen
// long, the unambiguous prefixes getopt_long and git accept.
func hasLongOpt(args []string, canonical string, minLen int) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		name, _, _ := strings.Cut(arg, "=")
		if strings.HasPrefix(name, "--") && len(name) >= minLen && strings.HasPrefix(canonical, name) {
			return true
		}
	}
	return false
}

// hasGoFlag reports a Go-style flag, one or two dashes, with or without =.
func hasGoFlag(args []string, name string) bool {
	for _, arg := range args {
		flag, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if strings.HasPrefix(arg, "-") && flag == name {
			return true
		}
	}
	return false
}

// operands are the arguments that are not options, skipping the value of
// each option in valued.
func operands(args []string, valued ...string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			return append(out, args[i+1:]...)
		case hasAnyArg([]string{arg}, valued...):
			i++
		case !strings.HasPrefix(arg, "-") || arg == "-":
			out = append(out, arg)
		}
	}
	return out
}

func withoutValues(args []string, valued ...string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if hasAnyArg([]string{args[i]}, valued...) {
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func withoutPrefixed(args []string, prefix string) []string {
	var out []string
	for _, arg := range args {
		if !strings.HasPrefix(arg, prefix) {
			out = append(out, arg)
		}
	}
	return out
}
