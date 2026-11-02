package shellparse

import "mvdan.cc/sh/v3/syntax"

// ExpandedVariables returns explicit parameter expansions, not quoted literals
// or indirect references. A command that cannot be parsed receives no secrets.
func ExpandedVariables(command string) map[string]bool {
	file, err := ParseBash(command)
	if err != nil {
		return nil
	}
	names := map[string]bool{}
	syntax.Walk(file, func(n syntax.Node) bool {
		if p, ok := n.(*syntax.ParamExp); ok && p.Param != nil && !p.Excl {
			names[p.Param.Value] = true
		}
		return true
	})
	return names
}
