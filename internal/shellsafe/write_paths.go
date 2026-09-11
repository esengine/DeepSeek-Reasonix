package shellsafe

import (
	"mvdan.cc/sh/v3/syntax"
	"reasonix/internal/shellparse"
	"strings"
)

// StaticWritePaths proves the complete write surface of a deliberately small
// shell subset. Directory permissions and model-provided scopes are never
// evidence of the actual command's targets. Unrecognized forms stay opaque.
func StaticWritePaths(command string) ([]string, bool) {
	f, err := shellparse.ParseBash(command)
	if err != nil || len(f.Stmts) != 1 {
		return nil, false
	}
	s := f.Stmts[0]
	if s.Background || s.Coprocess || s.Disown || s.Negated {
		return nil, false
	}
	c, ok := s.Cmd.(*syntax.CallExpr)
	if !ok || len(c.Assigns) > 0 || len(c.Args) == 0 || len(s.Redirs) == 0 {
		return nil, false
	}
	name, ok := shellparse.StaticWord(c.Args[0])
	if !ok || (name != "echo" && name != "printf") {
		return nil, false
	}
	for _, arg := range c.Args {
		if _, ok := shellparse.StaticWord(arg); !ok {
			return nil, false
		}
	}
	var paths []string
	for _, r := range s.Redirs {
		if r.Op != syntax.RdrOut && r.Op != syntax.AppOut {
			return nil, false
		}
		path, ok := shellparse.StaticWord(r.Word)
		if !ok || path == "" || strings.ContainsAny(path, "*?[{~") {
			return nil, false
		}
		paths = append(paths, path)
	}
	return paths, true
}
