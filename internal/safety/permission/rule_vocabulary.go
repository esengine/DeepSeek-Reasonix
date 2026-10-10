// rule_vocabulary.go — whether a rule can reach any tool at all.
package permission

import (
	"errors"
	"fmt"
)

// ErrUnknownTool is what a rule naming no tool is refused with. The refusal
// carries the list, the rule and the name, so a caller never reads the sentence.
var ErrUnknownTool = errors.New("permission rule names no registered tool")

// UnknownToolError is the typed form of ErrUnknownTool.
type UnknownToolError struct {
	List string
	Rule string
	Tool string
}

func (e *UnknownToolError) Error() string {
	return fmt.Sprintf("permission rule %q in %s names no registered tool %q", e.Rule, e.List, e.Tool)
}

func (e *UnknownToolError) Is(target error) bool { return target == ErrUnknownTool }

// Vocabulary is the set of tool names a rule's Tool is matched against.
type Vocabulary struct {
	Names []string
	// Open reports a name in a namespace a connection supplies after startup. A
	// rule there may match tools that are not connected yet, so it is not judged.
	Open func(name string) bool
}

// Check reports the rule when no tool in the vocabulary can ever match it, and
// nil when it can, when it is in an open namespace, or when it is not a rule.
// It uses the matcher the gate runs, so the two cannot disagree.
func (v Vocabulary) Check(list, raw string) error {
	r, ok := ParseRule(raw)
	if !ok {
		return nil
	}
	if list == "deny" && r.Subject == "" {
		if _, legacy := legacyBarePowerShellDenyCmdlet(r.Tool); legacy {
			return nil
		}
	}
	if v.Open != nil && v.Open(r.Tool) {
		return nil
	}
	for _, name := range v.Names {
		if ruleToolMatches(r.Tool, name) {
			return nil
		}
	}
	return &UnknownToolError{List: list, Rule: raw, Tool: r.Tool}
}
