// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"maps"
	mathrand "math/rand/v2"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

func newOverlayEnviron(parent expand.Environ, background bool) *overlayEnviron {
	oenv := &overlayEnviron{}
	if !background {
		oenv.parent = parent
	} else {
		// We could do better here if the parent is also an overlayEnviron;
		// measure with profiles or benchmarks before we choose to do so.
		for name, vr := range parent.Each {
			oenv.Set(name, vr)
		}
	}
	return oenv
}

// overlayEnviron is our main implementation of [expand.WriteEnviron].
type overlayEnviron struct {
	// parent is non-nil if [values] is an overlay over a parent environment
	// which we can safely reuse without data races, such as non-background subshells
	// or function calls.
	parent expand.Environ

	// values maps normalized variable names, per [overlayEnviron.normalize].
	values map[string]namedVariable

	// We need to know if the current scope is a function's scope, because
	// functions can modify global variables. When true, [parent] must not be nil.
	funcScope bool
}

// namedVariable records the original name of a variable for platforms
// where variable names are matched in a case-insensitive way.
type namedVariable struct {
	// TODO(v4): consider adding this field to [expand.Variable],
	// as a general way for a variable to report its original name.
	// This can be useful for GOOS=windows with case insensitive env vars,
	// as otherwise it's not possible to Environ.Get a var
	// and know what was its original name without looping over Environ.Each.
	Name string
	expand.Variable
}

func (o *overlayEnviron) normalize(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

func (o *overlayEnviron) Get(name string) expand.Variable {
	normalized := o.normalize(name)
	if vr, ok := o.values[normalized]; ok {
		return vr.Variable
	}
	if o.parent != nil {
		return o.parent.Get(name)
	}
	return expand.Variable{}
}

func (o *overlayEnviron) Set(name string, vr expand.Variable) error {
	normalized := o.normalize(name)
	prev, inOverlay := o.values[normalized]
	// Manipulation of a global var inside a function.
	if o.funcScope && !vr.Local && !prev.Local {
		// In a function, the parent environment is ours, so it's always read-write.
		return o.parent.(expand.WriteEnviron).Set(name, vr)
	}
	if !inOverlay && o.parent != nil {
		prev.Variable = o.parent.Get(name)
	}

	if o.values == nil {
		o.values = make(map[string]namedVariable)
	}
	if vr.Kind == expand.KeepValue {
		vr.Kind = prev.Kind
		vr.Str = prev.Str
		vr.List = prev.List
		vr.Map = prev.Map
	} else if prev.ReadOnly {
		return fmt.Errorf("readonly variable")
	}
	if !vr.IsSet() { // unsetting
		if prev.Local {
			vr.Local = true
			o.values[normalized] = namedVariable{name, vr}
			return nil
		}
		delete(o.values, normalized)
	}
	// modifying the entire variable
	vr.Local = prev.Local || vr.Local
	o.values[normalized] = namedVariable{name, vr}
	return nil
}

func (o *overlayEnviron) Each(f func(name string, vr expand.Variable) bool) {
	if o.parent != nil {
		o.parent.Each(f)
	}
	for _, vr := range o.values {
		if !f(vr.Name, vr.Variable) {
			return
		}
	}
}

func execEnv(env expand.Environ) []string {
	list := make([]string, 0, 64)
	for name, vr := range env.Each {
		if !vr.IsSet() {
			// If a variable is set globally but unset in the
			// runner, we need to ensure it's not part of the final
			// list. Seems like zeroing the element is enough.
			// This is a linear search, but this scenario should be
			// rare, and the number of variables shouldn't be large.
			for i, kv := range list {
				if strings.HasPrefix(kv, name+"=") {
					list[i] = ""
				}
			}
		}
		if vr.Exported && vr.Kind == expand.String {
			list = append(list, name+"="+vr.String())
		}
	}
	return list
}

func (r *Runner) lookupVar(name string) expand.Variable {
	if name == "" {
		panic("variable name must not be empty")
	}
	var vr expand.Variable
	switch name {
	case "#":
		vr.Kind, vr.Str = expand.String, strconv.Itoa(len(r.Params))
	case "@", "*":
		vr.Kind = expand.Indexed
		if r.Params == nil {
			// r.Params may be nil but positional parameters always exist
			vr.List = []string{}
		} else {
			vr.List = r.Params
		}
	case "!":
		if n := len(r.bgProcs); n > 0 {
			vr.Kind, vr.Str = expand.String, "g"+strconv.Itoa(n)
		}
	case "?":
		vr.Kind, vr.Str = expand.String, strconv.Itoa(int(r.lastExit.code))
	case "$":
		vr.Kind, vr.Str = expand.String, strconv.Itoa(os.Getpid())
	case "PPID":
		vr.Kind, vr.Str = expand.String, strconv.Itoa(os.Getppid())
	case "RANDOM": // not for cryptographic use
		vr.Kind, vr.Str = expand.String, strconv.Itoa(mathrand.IntN(32767))
		// TODO: support setting RANDOM to seed it
	case "SRANDOM": // pseudo-random generator from the system
		var p [4]byte
		cryptorand.Read(p[:])
		n := binary.NativeEndian.Uint32(p[:])
		vr.Kind, vr.Str = expand.String, strconv.FormatUint(uint64(n), 10)
	case "DIRSTACK":
		vr.Kind, vr.List = expand.Indexed, r.dirStack
	case "0":
		vr.Kind = expand.String
		if r.filename != "" {
			vr.Str = r.filename
		} else {
			vr.Str = "gosh"
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i := int(name[0] - '1'); i < len(r.Params) {
			vr.Kind = expand.String
			vr.Str = r.Params[i]
		}
	}
	if vr.Kind != expand.Unknown {
		vr.Set = true
		return vr
	}
	if vr := r.writeEnv.Get(name); vr.Declared() {
		return vr
	}
	return expand.Variable{}
}

func (r *Runner) envGet(name string) string {
	return r.lookupVar(name).String()
}

func (r *Runner) delVar(name string) {
	if err := r.writeEnv.Set(name, expand.Variable{}); err != nil {
		r.errf("%s: %v\n", name, err)
		r.exit.code = 1
		return
	}
}

func (r *Runner) setVarString(name, value string) {
	r.setVar(name, expand.Variable{Set: true, Kind: expand.String, Str: value})
}

func (r *Runner) setVar(name string, vr expand.Variable) {
	if r.opts[optAllExport] {
		vr.Exported = true
	}
	if err := r.writeEnv.Set(name, vr); err != nil {
		r.errf("%s: %v\n", name, err)
		r.exit.code = 1
		return
	}
}

func (r *Runner) setVarWithIndex(prev expand.Variable, name string, index syntax.ArithmExpr, vr expand.Variable) {
	if vr.Kind == expand.String && index == nil {
		// When assigning a string to an array, fall back to the
		// zero value for the index.
		switch prev.Kind {
		case expand.Indexed:
			index = &syntax.Word{Parts: []syntax.WordPart{
				&syntax.Lit{Value: "0"},
			}}
		case expand.Associative:
			index = &syntax.Word{Parts: []syntax.WordPart{
				&syntax.DblQuoted{},
			}}
		}
	}
	if index == nil {
		r.setVar(name, vr)
		return
	}

	// from the syntax package, we know that value must be a string if index
	// is non-nil; nested arrays are forbidden.
	valStr := vr.Str

	var list []string
	switch prev.Kind {
	case expand.String:
		list = append(list, prev.Str)
	case expand.Indexed:
		// TODO: only clone when inside a subshell and getting a var from outside for the first time
		list = slices.Clone(prev.List)
	case expand.Associative:
		// if the existing variable is already an AssocArray, try our
		// best to convert the key to a string
		w, ok := index.(*syntax.Word)
		if !ok {
			return
		}
		k := r.literal(w)

		// TODO: only clone when inside a subshell and getting a var from outside for the first time
		prev.Map = maps.Clone(prev.Map)
		if prev.Map == nil {
			prev.Map = make(map[string]string)
		}
		prev.Map[k] = valStr
		r.setVar(name, prev)
		return
	}
	k := r.arithm(index)
	for len(list) < k+1 {
		list = append(list, "")
	}
	list[k] = valStr
	prev.Kind = expand.Indexed
	prev.List = list
	r.setVar(name, prev)
}

func (r *Runner) setFunc(name string, body *syntax.Stmt) {
	if r.Funcs == nil {
		r.Funcs = make(map[string]*syntax.Stmt, 4)
	}
	r.Funcs[name] = body
}

func stringIndex(index syntax.ArithmExpr) bool {
	w, ok := index.(*syntax.Word)
	if !ok || len(w.Parts) != 1 {
		return false
	}
	switch w.Parts[0].(type) {
	case *syntax.DblQuoted, *syntax.SglQuoted:
		return true
	}
	return false
}

// TODO: make assignVal and [setVar] consistent with the [expand.WriteEnviron] interface

func (r *Runner) assignVal(name string, prev expand.Variable, as *syntax.Assign, valType string) (string, expand.Variable) {
	if n, v := prev.Resolve(r.writeEnv); n != "" {
		name, prev = n, v
	}
	prev.Set = true
	if as.Value != nil {
		s := r.literal(as.Value)
		if !as.Append {
			prev.Kind = expand.String
			if valType == "-n" {
				prev.Kind = expand.NameRef
			}
			prev.Str = s
			return name, prev
		}
		switch prev.Kind {
		case expand.String, expand.Unknown:
			prev.Kind = expand.String
			prev.Str += s
		case expand.Indexed:
			if len(prev.List) == 0 {
				prev.List = append(prev.List, "")
			}
			prev.List[0] += s
		case expand.Associative:
			// TODO
		}
		return name, prev
	}
	if as.Array == nil {
		// don't return the zero value, as that's an unset variable
		prev.Kind = expand.String
		if valType == "-n" {
			prev.Kind = expand.NameRef
		}
		prev.Str = ""
		return name, prev
	}
	// Array assignment.
	elems := as.Array.Elems
	if valType == "" {
		valType = "-a" // indexed
		if len(elems) > 0 && stringIndex(elems[0].Index) {
			valType = "-A" // associative
		}
	}
	if valType == "-A" {
		amap := make(map[string]string, len(elems))
		for _, elem := range elems {
			k := r.literal(elem.Index.(*syntax.Word))
			amap[k] = r.literal(elem.Value)
		}
		if !as.Append {
			prev.Kind = expand.Associative
			prev.Map = amap
			return name, prev
		}
		// TODO
		return name, prev
	}
	// Evaluate values for each array element.
	elemValues := make([]struct {
		index  int
		values []string
	}, len(elems))
	var index, maxIndex int
	for i, elem := range elems {
		if elem.Index != nil {
			// Index resets our index with a literal value.
			index = r.arithm(elem.Index)
			elemValues[i].values = []string{r.literal(elem.Value)}
		} else {
			// Implicit index, advancing for every word.
			elemValues[i].values = r.fields(elem.Value)
		}
		elemValues[i].index = index
		index += len(elemValues[i].values)
		maxIndex = max(maxIndex, index)
	}
	// Flatten down the values.
	strs := make([]string, maxIndex)
	for _, ev := range elemValues {
		for i, str := range ev.values {
			strs[ev.index+i] = str
		}
	}
	if !as.Append {
		prev.Kind = expand.Indexed
		prev.List = strs
		return name, prev
	}
	switch prev.Kind {
	case expand.Unknown:
		prev.Kind = expand.Indexed
		prev.List = strs
	case expand.String:
		prev.Kind = expand.Indexed
		prev.List = append([]string{prev.Str}, strs...)
	case expand.Indexed:
		prev.List = append(prev.List, strs...)
	case expand.Associative:
		// TODO
	default:
		// Should only happen if we forgot a case above.
		panic(fmt.Sprintf("unexpected conversion of kind %d", prev.Kind))
	}
	return name, prev
}
