package main

import (
	"fmt"
	"go/ast"
	"strconv"
	"strings"
)

// Claude caches only where cache_control marks a breakpoint, and that field is
// serialized by internal/model/anthropic alone. The same model reached
// through an openai-kind entry is not an error and not a visible wire
// difference — it is full input rate on every prompt token, forever, and the
// only symptom is a cache_read that never arrives.
const claudeModel = "claude"

// openaiShaped names the dialects whose request bodies have nowhere to put a
// cache_control breakpoint.
var openaiShaped = map[string]bool{"openai": true, "responses": true}

// providerDialect is one declared provider entry: the dialect it speaks and the
// models it offers, either written inline or named by a package-level slice.
type providerDialect struct {
	file, kind, modelsRef string
	line                  int
	models                []string
}

// dialectRefs collects the provider entries a file declares and the
// package-level string slices their Models fields may name.
func dialectRefs(s *sourceFile) ([]providerDialect, map[string][]string) {
	// A fixture builds the broken shape on purpose to assert what it does.
	if strings.HasSuffix(s.rel, "_test.go") {
		return nil, nil
	}
	vars := map[string][]string{}
	var entries []providerDialect
	ast.Inspect(s.file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.ValueSpec:
			for i, name := range node.Names {
				if i < len(node.Values) {
					if lit, ok := node.Values[i].(*ast.CompositeLit); ok {
						if got := stringElems(lit); len(got) > 0 {
							vars[name.Name] = got
						}
					}
				}
			}
		case *ast.CompositeLit:
			if entry, ok := providerEntryLit(s, node); ok {
				entries = append(entries, entry)
			}
		}
		return true
	})
	return entries, vars
}

// providerEntryLit reads a provider entry by the fields it sets, not by the
// type it names: inside []ProviderEntry{{...}} the elements carry no type.
func providerEntryLit(s *sourceFile, lit *ast.CompositeLit) (providerDialect, bool) {
	entry := providerDialect{file: s.rel, line: s.fset.Position(lit.Pos()).Line}
	var kind, models bool
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Kind":
			if name, ok := stringValue(kv.Value); ok {
				entry.kind, kind = name, true
			}
		case "Model":
			models = true
			if name, ok := stringValue(kv.Value); ok {
				entry.models = append(entry.models, name)
			}
		case "Models":
			models = true
			switch value := kv.Value.(type) {
			case *ast.CompositeLit:
				entry.models = append(entry.models, stringElems(value)...)
			case *ast.Ident:
				entry.modelsRef = value.Name
			}
		}
	}
	return entry, kind && models
}

// checkClaudeDialect flags a Claude model offered on a dialect that cannot
// carry cache_control. Where a gateway speaks only OpenAI the entry is debt the
// baseline records, and recording it is the point: the cost is otherwise
// invisible.
func checkClaudeDialect(entries []providerDialect, vars map[string][]string) []Finding {
	var out []Finding
	for _, entry := range entries {
		if !openaiShaped[entry.kind] {
			continue
		}
		models := entry.models
		if entry.modelsRef != "" {
			models = append(models, vars[entry.modelsRef]...)
		}
		for _, model := range models {
			if !strings.Contains(strings.ToLower(model), claudeModel) {
				continue
			}
			out = append(out, Finding{entry.file, entry.line, ruleClaudeDialect, fmt.Sprintf(
				"%q on the %q dialect: cache_control ships only from internal/model/anthropic, so every prompt token bills at full input rate",
				model, entry.kind), 1})
			break
		}
	}
	return out
}

func stringElems(lit *ast.CompositeLit) []string {
	var out []string
	for _, el := range lit.Elts {
		if value, ok := stringValue(el); ok {
			out = append(out, value)
		}
	}
	return out
}

func stringValue(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind.String() != "STRING" {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}
