package shellsafe

import (
	"strings"

	"reasonix/internal/base/shellparse"
)

func UnwrapDeleteCommand(name string, args []string) (string, []string, bool) {
	return unwrapDeleteCommand(name, args)
}

func unwrapDeleteCommand(name string, args []string) (string, []string, bool) {
	base := ExecutableBase(name)
	for {
		switch base {
		case "command", "env", "exec", "builtin", "sudo", "nohup":
		default:
			return base, args, true
		}
		wrapper := base
		for len(args) > 0 {
			arg := args[0]
			if wrapper == "command" && (arg == "-v" || arg == "-V") {
				return "", nil, true
			}
			if arg == "--" {
				args = args[1:]
				break
			}
			if wrapper == "env" && shellparse.IsAssignment(arg) || wrapper == "command" && arg == "-p" {
				args = args[1:]
				continue
			}
			if arg == "" {
				return "", args, false
			}
			if strings.HasPrefix(arg, "-") {
				switch arg {
				case "-E", "-n", "-i", "--ignore-environment", "--non-interactive":
					args = args[1:]
					continue
				case "-u", "-g", "--user", "--group":
					if len(args) < 2 {
						return "", args, false
					}
					args = args[2:]
					continue
				default:
					return "", args, false
				}
			}
			break
		}
		if len(args) == 0 {
			return "", nil, true
		}
		base, args = ExecutableBase(args[0]), args[1:]
	}
}
