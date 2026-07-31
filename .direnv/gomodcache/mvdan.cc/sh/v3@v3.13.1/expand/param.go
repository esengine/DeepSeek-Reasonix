// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"mvdan.cc/sh/v3/pattern"
	"mvdan.cc/sh/v3/syntax"
)

func nodeLit(node syntax.Node) string {
	if word, ok := node.(*syntax.Word); ok {
		return word.Lit()
	}
	return ""
}

// UnsetParameterError is returned when a parameter expansion encounters an
// unset variable and [Config.NoUnset] has been set.
type UnsetParameterError struct {
	Node    *syntax.ParamExp
	Message string
}

func (u UnsetParameterError) Error() string {
	return fmt.Sprintf("%s: %s", u.Node.Param.Value, u.Message)
}

func overridingUnset(pe *syntax.ParamExp) bool {
	if pe.Exp == nil {
		return false
	}
	switch pe.Exp.Op {
	case syntax.AlternateUnset, syntax.AlternateUnsetOrNull,
		syntax.DefaultUnset, syntax.DefaultUnsetOrNull,
		syntax.ErrorUnset, syntax.ErrorUnsetOrNull,
		syntax.AssignUnset, syntax.AssignUnsetOrNull:
		return true
	}
	return false
}

func (cfg *Config) paramExp(pe *syntax.ParamExp) (string, error) {
	oldParam := cfg.curParam
	cfg.curParam = pe
	defer func() { cfg.curParam = oldParam }()

	name := pe.Param.Value
	index := pe.Index
	switch name {
	case "@", "*":
		index = &syntax.Word{Parts: []syntax.WordPart{
			&syntax.Lit{Value: name},
		}}
	}
	var vr Variable
	switch name {
	case "LINENO":
		// This is the only parameter expansion that the environment
		// interface cannot satisfy.
		line := uint64(cfg.curParam.Pos().Line())
		vr = Variable{Set: true, Kind: String, Str: strconv.FormatUint(line, 10)}
	default:
		vr = cfg.Env.Get(name)
	}
	orig := vr
	if n, v := vr.Resolve(cfg.Env); n != "" {
		name, vr = n, v
	}
	if cfg.NoUnset && !vr.IsSet() && !overridingUnset(pe) {
		return "", UnsetParameterError{
			Node:    pe,
			Message: "unbound variable",
		}
	}

	var sliceOffset, sliceLen int
	if pe.Slice != nil {
		var err error
		if pe.Slice.Offset != nil {
			sliceOffset, err = Arithm(cfg, pe.Slice.Offset)
			if err != nil {
				return "", err
			}
		}
		if pe.Slice.Length != nil {
			sliceLen, err = Arithm(cfg, pe.Slice.Length)
			if err != nil {
				return "", err
			}
		}
	}

	var (
		str   string
		elems []string

		indexAllElements bool // true if var has been accessed with * or @ index
		callVarInd       = true
	)

	switch nodeLit(index) {
	case "@", "*":
		switch vr.Kind {
		case Unknown:
			elems = nil
			indexAllElements = true
		case Indexed:
			indexAllElements = true
			callVarInd = false
			elems = cfg.sliceElems(pe, vr.List, name == "@" || name == "*")
			str = strings.Join(elems, " ")
		}
	}
	if callVarInd {
		var err error
		str, err = cfg.varInd(vr, index)
		if err != nil {
			return "", err
		}
	}
	if !indexAllElements {
		elems = []string{str}
	}

	switch {
	case pe.Length:
		n := len(elems)
		switch nodeLit(index) {
		case "@", "*":
		default:
			n = utf8.RuneCountInString(str)
		}
		str = strconv.Itoa(n)
	case pe.Excl:
		var strs []string
		switch {
		case pe.Names != 0:
			strs = cfg.namesByPrefix(pe.Param.Value)
		case orig.Kind == NameRef:
			strs = append(strs, orig.Str)
		case pe.Index != nil && vr.Kind == Indexed:
			for i, e := range vr.List {
				if e != "" {
					strs = append(strs, strconv.Itoa(i))
				}
			}
		case pe.Index != nil && vr.Kind == Associative:
			strs = slices.AppendSeq(strs, maps.Keys(vr.Map))
		case !vr.IsSet():
			return "", fmt.Errorf("invalid indirect expansion")
		case str == "":
			return "", nil
		default:
			vr = cfg.Env.Get(str)
			strs = append(strs, vr.String())
		}
		slices.Sort(strs)
		str = strings.Join(strs, " ")
	case pe.Width:
		return "", fmt.Errorf("unsupported")
	case pe.IsSet:
		return "", fmt.Errorf("unsupported")
	case pe.Slice != nil:
		if callVarInd {
			slicePos := func(n int) int {
				if n < 0 {
					n = len(str) + n
					if n < 0 {
						n = len(str)
					}
				} else if n > len(str) {
					n = len(str)
				}
				return n
			}
			if pe.Slice.Offset != nil {
				str = str[slicePos(sliceOffset):]
			}
			if pe.Slice.Length != nil {
				str = str[:slicePos(sliceLen)]
			}
		} // else, elems are already sliced
	case pe.Repl != nil:
		orig, err := Pattern(cfg, pe.Repl.Orig)
		if err != nil {
			return "", err
		}
		if orig == "" {
			break // nothing to replace
		}
		with, err := Literal(cfg, pe.Repl.With)
		if err != nil {
			return "", err
		}
		n := 1
		if pe.Repl.All {
			n = -1
		}
		locs := findAllIndex(orig, str, n)
		sb := cfg.strBuilder()
		last := 0
		for _, loc := range locs {
			sb.WriteString(str[last:loc[0]])
			sb.WriteString(with)
			last = loc[1]
		}
		sb.WriteString(str[last:])
		str = sb.String()
	case pe.Exp != nil:
		arg, err := Literal(cfg, pe.Exp.Word)
		if err != nil {
			return "", err
		}
		switch op := pe.Exp.Op; op {
		case syntax.AlternateUnsetOrNull:
			if str == "" {
				break
			}
			fallthrough
		case syntax.AlternateUnset:
			if vr.IsSet() {
				str = arg
			}
		case syntax.DefaultUnset:
			if vr.IsSet() {
				break
			}
			fallthrough
		case syntax.DefaultUnsetOrNull:
			if str == "" {
				str = arg
			}
		case syntax.ErrorUnset:
			if vr.IsSet() {
				break
			}
			fallthrough
		case syntax.ErrorUnsetOrNull:
			if str == "" {
				return "", UnsetParameterError{
					Node:    pe,
					Message: arg,
				}
			}
		case syntax.AssignUnset:
			if vr.IsSet() {
				break
			}
			fallthrough
		case syntax.AssignUnsetOrNull:
			if str == "" {
				if err := cfg.envSet(name, arg); err != nil {
					return "", err
				}
				str = arg
			}
		case syntax.RemSmallPrefix, syntax.RemLargePrefix,
			syntax.RemSmallSuffix, syntax.RemLargeSuffix:
			suffix := op == syntax.RemSmallSuffix || op == syntax.RemLargeSuffix
			small := op == syntax.RemSmallPrefix || op == syntax.RemSmallSuffix
			for i, elem := range elems {
				elems[i] = removePattern(elem, arg, suffix, small)
			}
			str = strings.Join(elems, " ")
		case syntax.UpperFirst, syntax.UpperAll,
			syntax.LowerFirst, syntax.LowerAll:

			caseFunc := unicode.ToLower
			if op == syntax.UpperFirst || op == syntax.UpperAll {
				caseFunc = unicode.ToUpper
			}
			all := op == syntax.UpperAll || op == syntax.LowerAll

			// empty string means '?'; nothing to do there
			expr, err := pattern.Regexp(arg, 0)
			if err != nil {
				return str, nil
			}
			rx := regexp.MustCompile(expr)

			for i, elem := range elems {
				rs := []rune(elem)
				for ri, r := range rs {
					if rx.MatchString(string(r)) {
						rs[ri] = caseFunc(r)
						if !all {
							break
						}
					}
				}
				elems[i] = string(rs)
			}
			str = strings.Join(elems, " ")
		case syntax.OtherParamOps:
			switch arg {
			case "Q":
				str, err = syntax.Quote(str, syntax.LangBash)
				if err != nil {
					// Is this even possible? If a user runs into this panic,
					// it's most likely a bug we need to fix.
					panic(err)
				}
			case "E":
				tail := str
				var rns []rune
				for tail != "" {
					var rn rune
					rn, _, tail, _ = strconv.UnquoteChar(tail, 0)
					rns = append(rns, rn)
				}
				str = string(rns)
			case "a":
				// ${var@a} returns variable attribute flags.
				// We use orig (before nameref resolve) for the attributes.
				str = orig.Flags()
			case "A":
				// ${var@A} returns a declare statement that recreates the variable.
				flags := orig.Flags()
				quoted, err := syntax.Quote(str, syntax.LangBash)
				if err != nil {
					return "", err
				}
				if flags == "" {
					str = fmt.Sprintf("%s=%s", name, quoted)
				} else {
					str = fmt.Sprintf("declare -%s %s=%s", flags, name, quoted)
				}
			case "P":
				// TODO: implement prompt expansion (\u, \h, \w, etc.).
			default:
				panic(fmt.Sprintf("unexpected @%s param expansion", arg))
			}
		}
	}
	return str, nil
}

func removePattern(str, pat string, fromEnd, shortest bool) string {
	var mode pattern.Mode
	if shortest {
		mode |= pattern.Shortest
	}
	expr, err := pattern.Regexp(pat, mode)
	if err != nil {
		return str
	}
	switch {
	case fromEnd && shortest:
		// use .* to get the right-most shortest match
		expr = ".*(" + expr + ")$"
	case fromEnd:
		// simple suffix
		expr = "(" + expr + ")$"
	default:
		// simple prefix
		expr = "^(" + expr + ")"
	}
	// no need to check error as Translate returns one
	rx := regexp.MustCompile(expr)
	if loc := rx.FindStringSubmatchIndex(str); loc != nil {
		// remove the original pattern (the submatch)
		str = str[:loc[2]] + str[loc[3]:]
	}
	return str
}

func (cfg *Config) varInd(vr Variable, idx syntax.ArithmExpr) (string, error) {
	if idx == nil {
		return vr.String(), nil
	}
	switch vr.Kind {
	case String:
		n, err := Arithm(cfg, idx)
		if err != nil {
			return "", err
		}
		if n == 0 {
			return vr.Str, nil
		}
	case Indexed:
		switch nodeLit(idx) {
		case "*", "@":
			return strings.Join(vr.List, " "), nil
		}
		i, err := Arithm(cfg, idx)
		if err != nil {
			return "", err
		}
		if i < 0 {
			return "", fmt.Errorf("negative array index")
		}
		if i < len(vr.List) {
			return vr.List[i], nil
		}
	case Associative:
		switch lit := nodeLit(idx); lit {
		case "@", "*":
			strs := slices.Sorted(maps.Values(vr.Map))
			if lit == "*" {
				return cfg.ifsJoin(strs), nil
			}
			return strings.Join(strs, " "), nil
		}
		val, err := Literal(cfg, idx.(*syntax.Word))
		if err != nil {
			return "", err
		}
		return vr.Map[val], nil
	}
	return "", nil
}

func (cfg *Config) namesByPrefix(prefix string) []string {
	var names []string
	for name := range cfg.Env.Each {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	return names
}
