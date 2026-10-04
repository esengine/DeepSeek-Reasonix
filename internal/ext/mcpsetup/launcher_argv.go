package mcpsetup

import (
	"slices"
	"strings"
)

type launcherFlag uint8

const (
	launcherBoolean launcherFlag = iota
	launcherValue
	launcherShell
	launcherModule
)

type commandLauncher struct {
	commands map[string][]string
	flags    map[string]launcherFlag
	image    bool
}

var containerLauncher = commandLauncher{
	commands: map[string][]string{"run": nil, "container": {"run"}},
	image:    true,
	flags: map[string]launcherFlag{
		"rm": launcherBoolean, "i": launcherBoolean, "interactive": launcherBoolean,
		"t": launcherBoolean, "tty": launcherBoolean, "d": launcherBoolean, "detach": launcherBoolean,
		"q": launcherBoolean, "quiet": launcherBoolean, "P": launcherBoolean, "publish-all": launcherBoolean,
		"init": launcherBoolean, "read-only": launcherBoolean, "sig-proxy": launcherBoolean,
		"no-healthcheck": launcherBoolean,
		"e":              launcherValue, "env": launcherValue, "env-file": launcherValue,
		"v": launcherValue, "volume": launcherValue, "mount": launcherValue,
		"u": launcherValue, "user": launcherValue, "w": launcherValue, "workdir": launcherValue,
		"p": launcherValue, "publish": launcherValue, "name": launcherValue,
		"network": launcherValue, "platform": launcherValue, "entrypoint": launcherValue, "pull": launcherValue,
		"c": launcherValue, "context": launcherValue, "H": launcherValue, "host": launcherValue,
		"n": launcherValue, "namespace": launcherValue, "root": launcherValue,
	},
}

var packageLauncher = commandLauncher{
	flags: map[string]launcherFlag{
		"p": launcherValue, "package": launcherValue, "node-options": launcherValue,
		"y": launcherBoolean, "yes": launcherBoolean, "no": launcherBoolean,
		"quiet": launcherBoolean, "q": launcherBoolean, "color": launcherBoolean,
		"no-install": launcherBoolean, "ignore-existing": launcherBoolean, "bun": launcherBoolean,
		"c": launcherShell, "call": launcherShell,
	},
}

var pythonLauncher = commandLauncher{
	flags: map[string]launcherFlag{
		"m": launcherModule, "c": launcherShell,
		"W": launcherValue, "X": launcherValue, "check-hash-based-pycs": launcherValue,
		"b": launcherBoolean, "B": launcherBoolean, "d": launcherBoolean, "E": launcherBoolean,
		"i": launcherBoolean, "I": launcherBoolean, "O": launcherBoolean, "P": launcherBoolean,
		"q": launcherBoolean, "s": launcherBoolean, "S": launcherBoolean, "u": launcherBoolean,
		"v": launcherBoolean, "x": launcherBoolean,
	},
}

var nodeLauncher = commandLauncher{
	flags: map[string]launcherFlag{
		"r": launcherValue, "require": launcherValue, "import": launcherValue,
		"env-file": launcherValue, "env-file-if-exists": launcherValue,
		"conditions": launcherValue, "C": launcherValue,
		"e": launcherShell, "eval": launcherShell, "p": launcherShell, "print": launcherShell,
		"inspect": launcherBoolean, "inspect-brk": launcherBoolean, "trace-warnings": launcherBoolean,
	},
}

var uvLauncher = commandLauncher{
	flags: map[string]launcherFlag{
		"directory": launcherValue, "project": launcherValue, "config-file": launcherValue,
		"cache-dir": launcherValue, "color": launcherValue, "allow-insecure-host": launcherValue,
		"p": launcherValue, "python": launcherValue, "from": launcherValue,
		"w": launcherValue, "with": launcherValue, "with-requirements": launcherValue,
		"offline": launcherBoolean, "no-config": launcherBoolean, "no-cache": launcherBoolean,
		"q": launcherBoolean, "quiet": launcherBoolean, "v": launcherBoolean, "verbose": launcherBoolean,
		"no-progress": launcherBoolean, "locked": launcherBoolean, "frozen": launcherBoolean,
		"no-sync": launcherBoolean,
	},
}

var commandLaunchers = map[string]commandLauncher{
	"docker": containerLauncher, "podman": containerLauncher, "nerdctl": containerLauncher,
	"npx": packageLauncher, "bunx": packageLauncher,
	"python": pythonLauncher, "python3": pythonLauncher, "py": pythonLauncher,
	"node": nodeLauncher, "uvx": uvLauncher,
	"uv": {commands: map[string][]string{"run": nil}, flags: uvLauncher.flags},
	"pnpm": {
		commands: map[string][]string{"dlx": nil},
		flags: map[string]launcherFlag{
			"package": launcherValue, "allow-build": launcherValue, "reporter": launcherValue,
			"dir": launcherValue, "C": launcherValue, "filter": launcherValue,
			"silent": launcherBoolean, "s": launcherBoolean, "shell-mode": launcherBoolean,
			"c": launcherBoolean, "global": launcherBoolean, "g": launcherBoolean,
		},
	},
	"npm": {
		commands: map[string][]string{"exec": nil, "x": nil},
		flags: map[string]launcherFlag{
			"package": launcherValue, "workspace": launcherValue, "w": launcherValue,
			"prefix": launcherValue, "cache": launcherValue, "userconfig": launcherValue, "registry": launcherValue,
			"yes": launcherBoolean, "y": launcherBoolean, "no": launcherBoolean,
			"p": launcherBoolean, "parseable": launcherBoolean,
			"call": launcherShell, "c": launcherShell,
		},
	},
	"go": {
		commands: map[string][]string{"run": nil},
		flags: map[string]launcherFlag{
			"C": launcherValue, "p": launcherValue, "covermode": launcherValue, "coverpkg": launcherValue,
			"asmflags": launcherValue, "buildmode": launcherValue, "compiler": launcherValue,
			"gccgoflags": launcherValue, "gcflags": launcherValue, "installsuffix": launcherValue,
			"ldflags": launcherValue, "mod": launcherValue, "modfile": launcherValue,
			"overlay": launcherValue, "pgo": launcherValue, "pkgdir": launcherValue,
			"tags": launcherValue, "toolexec": launcherValue, "exec": launcherValue,
			"a": launcherBoolean, "n": launcherBoolean, "race": launcherBoolean,
			"msan": launcherBoolean, "asan": launcherBoolean, "cover": launcherBoolean,
			"v": launcherBoolean, "work": launcherBoolean, "x": launcherBoolean,
			"buildvcs": launcherBoolean, "json": launcherBoolean, "linkshared": launcherBoolean,
			"modcacherw": launcherBoolean, "trimpath": launcherBoolean,
		},
	},
}

func launcherCommandOperand(args []string, launcher commandLauncher) string {
	options, needsCommand := true, len(launcher.commands) != 0
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if options && arg == "--" {
			options = false
			continue
		}
		if options && strings.HasPrefix(arg, "-") {
			kind, value, width, ok := launcherOption(arg, launcher)
			if !ok || i+width >= len(args) {
				return ""
			}
			if width != 0 {
				value = args[i+width]
			}
			if kind == launcherModule {
				return value
			}
			i += width
			continue
		}
		if needsCommand {
			suffix, ok := launcher.commands[arg]
			if !ok || i+len(suffix) >= len(args) || !slices.Equal(args[i+1:i+1+len(suffix)], suffix) {
				return ""
			}
			i += len(suffix)
			needsCommand = false
			continue
		}
		return arg
	}
	return ""
}

func launcherOption(arg string, launcher commandLauncher) (launcherFlag, string, int, bool) {
	key, value, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
	if key == "" {
		return 0, "", 0, false
	}
	if kind, ok := launcher.flags[key]; ok {
		if kind == launcherShell {
			return 0, "", 0, false
		}
		if kind != launcherBoolean && !inline {
			return kind, "", 1, true
		}
		return kind, value, 0, true
	}
	if strings.HasPrefix(arg, "--") {
		return launcherBoolean, "", 0, len(launcher.commands) == 0
	}
	for i := 1; i < len(arg); i++ {
		kind, ok := launcher.flags[arg[i:i+1]]
		if !ok && len(launcher.commands) != 0 || kind == launcherShell {
			return 0, "", 0, false
		}
		if kind != launcherBoolean {
			if i+1 == len(arg) {
				return kind, "", 1, true
			}
			return kind, strings.TrimPrefix(arg[i+1:], "="), 0, true
		}
	}
	return launcherBoolean, "", 0, true
}
