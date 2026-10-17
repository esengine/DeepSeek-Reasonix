package hook

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
	"reasonix/internal/base/shellparse"
	"reasonix/internal/contract/config"
)

func (h ResolvedHook) ResourcePaths(cwd string) []string {
	base := h.ResourceCwd(cwd)
	words := hookResourceWords(h.Command)
	if h.ExecutionMode == ExecutionExec {
		words = append([]string{h.Command}, h.Argv...)
	}
	if h.ContextFile != "" {
		words = append(words, h.ContextFile)
	}
	root := filepath.VolumeName(base) + string(filepath.Separator)
	files := config.NewProjectProgram(config.ProjectProgramHooks, "retained-hook", "", root, base, nil, words).Files
	out := []string{}
	for _, file := range files {
		if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() {
			out = append(out, file)
		}
	}
	return out
}

func hookResourceWords(command string) []string {
	file, err := shellparse.ParseBash(command)
	if err != nil {
		return commandWords(command)
	}
	words := []string{}
	syntax.Walk(file, func(node syntax.Node) bool {
		if call, ok := node.(*syntax.CallExpr); ok {
			for _, arg := range call.Args {
				if value, ok := shellparse.StaticWord(arg); ok && strings.ContainsAny(value, `/\.`) {
					words = append(words, value)
				}
			}
		}
		return true
	})
	return words
}

func (h ResolvedHook) ResourceCwd(cwd string) string {
	if h.Cwd == "" {
		return cwd
	}
	if filepath.IsAbs(h.Cwd) {
		return h.Cwd
	}
	return filepath.Join(cwd, h.Cwd)
}

// WithRetainedCheck checks retained bytes and the original, revocable grant.
func (h ResolvedHook) WithRetainedCheck(check func() error) ResolvedHook {
	h.retainedCheck = check
	return h
}

func (h ResolvedHook) RewriteResourcePaths(cwd string, paths map[string]string) (ResolvedHook, error) {
	resolve := func(word string) string {
		path := word
		if !filepath.IsAbs(path) {
			path = filepath.Join(h.ResourceCwd(cwd), path)
		}
		if target, ok := paths[filepath.Clean(path)]; ok {
			return target
		}
		return word
	}
	h.ContextFile = resolve(h.ContextFile)
	if h.ExecutionMode == ExecutionExec {
		h.Command = resolve(h.Command)
		h.Argv = append([]string(nil), h.Argv...)
		for i, word := range h.Argv {
			h.Argv[i] = resolve(word)
		}
		return h, nil
	}
	file, err := shellparse.ParseBash(h.Command)
	if err != nil {
		return h, err
	}
	var rewriteErr error
	syntax.Walk(file, func(node syntax.Node) bool {
		word, ok := node.(*syntax.Word)
		if !ok {
			return true
		}
		value, static := shellparse.StaticWord(word)
		if !static || resolve(value) == value {
			return true
		}
		quoted, err := syntax.Quote(resolve(value), syntax.LangBash)
		if err != nil {
			rewriteErr = err
			return false
		}
		parsed, err := shellparse.ParseBash("retain " + quoted)
		if err != nil {
			rewriteErr = err
			return false
		}
		word.Parts = parsed.Stmts[0].Cmd.(*syntax.CallExpr).Args[1].Parts
		return false
	})
	if rewriteErr != nil {
		return h, rewriteErr
	}
	var output bytes.Buffer
	if err := syntax.NewPrinter().Print(&output, file); err != nil {
		return h, err
	}
	h.Command = strings.TrimSuffix(output.String(), "\n")
	return h, nil
}
