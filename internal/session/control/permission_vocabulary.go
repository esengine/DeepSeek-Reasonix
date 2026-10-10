// permission_vocabulary.go — which saved permission rules can match a tool.
package control

import (
	"errors"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/safety/permission"
)

// PermissionVocabulary is every tool name a rule can be matched against now:
// the built-ins, the use_capability proxy assembly adds, and whatever the live
// registry holds. Tools an MCP server or extension supplies after startup are
// reported open, not unknown.
func (c *Controller) PermissionVocabulary() permission.Vocabulary {
	var names []string
	for _, e := range tool.BuiltinContractEntries() {
		names = append(names, e.Name)
	}
	names = append(names, new(usecap.UseCapabilityTool).Name())
	for _, e := range c.AllToolContractEntries() {
		names = append(names, e.Name)
	}
	return permission.Vocabulary{Names: names, Open: tool.IsConnectionName}
}

// DormantRules lists the rules in lists that no tool can match, in the order
// the gate reads the lists.
func DormantRules(v permission.Vocabulary, lists PermissionLists) []event.DormantPermissionRule {
	var out []event.DormantPermissionRule
	for _, l := range []struct {
		name  string
		rules []string
	}{{"deny", lists.Deny}, {"ask", lists.Ask}, {"allow", lists.Allow}} {
		for _, rule := range l.rules {
			var u *permission.UnknownToolError
			if errors.As(v.Check(l.name, rule), &u) {
				out = append(out, event.DormantPermissionRule{List: u.List, Rule: u.Rule, Tool: u.Tool})
			}
		}
	}
	return out
}
