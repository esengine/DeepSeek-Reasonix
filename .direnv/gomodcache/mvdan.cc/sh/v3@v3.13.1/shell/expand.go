// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell

import (
	"os"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// Expand performs shell expansion on s as if it were within double quotes,
// using env to resolve variables. This includes parameter expansion, arithmetic
// expansion, and quote removal.
//
// If env is nil, the current environment variables are used. Empty variables
// are treated as unset; to support variables which are set but empty, use the
// [expand] package directly.
//
// Other forms of expansion are not supported in this simple API, such as
// command substitutions like $(echo foo). To support them, use the [expand] package.
//
// An error will be reported if the input string had invalid syntax.
func Expand(s string, env func(string) string) (string, error) {
	p := syntax.NewParser()
	word, err := p.Document(strings.NewReader(s))
	if err != nil {
		return "", err
	}
	if env == nil {
		env = os.Getenv
	}
	cfg := &expand.Config{Env: expand.FuncEnviron(env)}
	return expand.Document(cfg, word)
}

// Fields performs shell expansion on s as if it were a command's arguments,
// using env to resolve variables. It is similar to Expand, but includes brace
// expansion, tilde expansion, and word splitting.
//
// If env is nil, the current environment variables are used. Empty variables
// are treated as unset; to support variables which are set but empty, use the
// [expand] package directly.
//
// Other forms of expansion are not supported in this simple API, such as
// globbing and command substitutions like $(echo foo).
// To support them, use the [expand] package.
//
// An error will be reported if the input string had invalid syntax.
func Fields(s string, env func(string) string) ([]string, error) {
	p := syntax.NewParser()
	var words []*syntax.Word
	for w, err := range p.WordsSeq(strings.NewReader(s)) {
		if err != nil {
			return nil, err
		}
		words = append(words, w)
	}
	if env == nil {
		env = os.Getenv
	}
	cfg := &expand.Config{Env: expand.FuncEnviron(env)}
	return expand.Fields(cfg, words...)
}
