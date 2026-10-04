package shellsafe

import "strings"

type DeleteExtent struct {
	Targets        []string
	Recursive      bool
	UnknownOption  bool
	DynamicCommand bool
}

func AnalyzeRecursiveDelete(name string, args []string, powerShell bool) DeleteExtent {
	base, args, known := unwrapDeleteCommand(name, args)
	if !known {
		return recursiveArgumentDelete(args, powerShell)
	}
	switch base {
	case "cmd":
		return cmdDelete(args)
	case "xargs":
		return recursiveArgumentDelete(args, powerShell)
	case "find":
		return findDelete(args)
	case "rm", "remove-item", "ri", "del", "erase", "rd", "rmdir", "":
		return directDelete(base, args, powerShell)
	default:
		return DeleteExtent{}
	}
}

func recursiveArgumentDelete(args []string, powerShell bool) DeleteExtent {
	for i, arg := range args {
		e := AnalyzeRecursiveDelete(arg, args[i+1:], powerShell)
		if e.Recursive {
			e.Targets = []string{""}
			return e
		}
	}
	return DeleteExtent{}
}

func cmdDelete(args []string) DeleteExtent {
	if len(args) == 0 || !strings.EqualFold(args[0], "/c") {
		return DeleteExtent{}
	}
	if len(args) == 2 {
		args = strings.Fields(args[1])
	} else {
		args = args[1:]
	}
	if len(args) == 0 {
		return DeleteExtent{}
	}
	recursive, complex := false, false
	for _, arg := range args {
		recursive = recursive || strings.EqualFold(arg, "/s")
		complex = complex || strings.ContainsAny(arg, "^%&|\r\n")
	}
	if recursive && complex {
		return DeleteExtent{Recursive: true, DynamicCommand: true}
	}
	return AnalyzeRecursiveDelete(args[0], args[1:], true)
}

func findDelete(args []string) DeleteExtent {
	e := DeleteExtent{}
	var extra []string
	for i, arg := range args {
		switch arg {
		case "-delete":
			e.Recursive = true
		case "-follow", "-L":
			e.UnknownOption = true
		case "-exec", "-execdir":
			if i+1 >= len(args) {
				continue
			}
			nested := findExecDelete(args[i+1:])
			e.Recursive = e.Recursive || nested.Recursive
			e.UnknownOption = e.UnknownOption || nested.UnknownOption
			if nested.Recursive {
				extra = append(extra, nested.Targets...)
				if arg == "-execdir" {
					extra = append(extra, "")
				}
			}
		}
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		e.Targets = append(e.Targets, arg)
	}
	if len(e.Targets) == 0 {
		e.Targets = []string{"."}
	}
	e.Targets = append(e.Targets, extra...)
	return e
}

func findExecDelete(args []string) DeleteExtent {
	tail := args[1:]
	for j, v := range tail {
		if v == ";" || v == "+" {
			tail = tail[:j]
			break
		}
	}
	e := AnalyzeRecursiveDelete(args[0], tail, false)
	var targets []string
	for _, target := range e.Targets {
		if target != "{}" {
			targets = append(targets, target)
		}
	}
	e.Targets = targets
	return e
}
