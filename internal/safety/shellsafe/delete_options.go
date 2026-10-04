package shellsafe

import "strings"

func directDelete(base string, args []string, powerShell bool) DeleteExtent {
	e := DeleteExtent{}
	options := true
	psParameters := powerShell && (base == "remove-item" || base == "ri" || base == "rm" || base == "del" || base == "")
	cmdParameters := powerShell && (base == "del" || base == "erase" || base == "rd" || base == "rmdir")
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if options && arg == "--" {
			options = false
			continue
		}
		if options && strings.HasPrefix(arg, "-") {
			if psParameters {
				i += powerShellDeleteOption(&e, arg, args[i+1:])
			} else {
				posixDeleteOption(&e, arg)
			}
			continue
		}
		if cmdParameters && strings.HasPrefix(arg, "/") {
			cmdDeleteOption(&e, arg)
			continue
		}
		e.Targets = append(e.Targets, arg)
	}
	if !e.Recursive {
		return DeleteExtent{}
	}
	if len(e.Targets) == 0 {
		e.Targets = []string{""}
	}
	return e
}

func powerShellDeleteOption(e *DeleteExtent, arg string, rest []string) int {
	key, value, inline := strings.Cut(strings.TrimPrefix(strings.ToLower(arg), "-"), ":")
	switch {
	case key != "" && strings.HasPrefix("recurse", key):
		e.Recursive = e.Recursive || value != "$false"
	case key != "" && strings.HasPrefix("force", key):
	case key == "literalpath" || key == "path":
	case key == "erroraction" || key == "ea" || key == "warningaction" || key == "wa" || key == "informationaction" || key == "confirm":
		if !inline {
			if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
				return 1
			}
			e.UnknownOption = true
		}
	case key == "verbose" || key == "debug" || key == "whatif":
	default:
		e.UnknownOption = true
	}
	return 0
}

func posixDeleteOption(e *DeleteExtent, arg string) {
	if strings.HasPrefix(arg, "--") {
		if strings.HasPrefix("--recursive", arg) {
			e.Recursive = true
		} else if arg != "--force" && arg != "--verbose" && arg != "--dir" {
			e.UnknownOption = true
		}
		return
	}
	e.Recursive = e.Recursive || strings.ContainsAny(arg[1:], "rR")
	e.UnknownOption = e.UnknownOption || strings.Trim(arg[1:], "rfRvidI") != ""
}

func cmdDeleteOption(e *DeleteExtent, arg string) {
	switch strings.ToLower(arg) {
	case "/s":
		e.Recursive = true
	case "/q", "/f":
	default:
		e.UnknownOption = true
	}
}
