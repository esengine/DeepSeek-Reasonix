package control

import (
	"fmt"
	"strconv"
	"strings"

	"reasonix/internal/agent"
)

// ParseBranchTarget parses the arguments after "/branch". A leading positive
// integer means "branch from displayed turn N"; otherwise the whole argument is
// the optional branch name for a tip branch.
func ParseBranchTarget(args string) (turn int, name string, fromTurn bool, err error) {
	args = strings.TrimSpace(args)
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return 0, "", false, nil
	}
	n, convErr := strconv.Atoi(fields[0])
	if convErr != nil {
		return 0, args, false, nil
	}
	if n <= 0 {
		return 0, "", false, fmt.Errorf("usage: /branch [turn] [name]")
	}
	name = strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
	return n, name, true, nil
}

func (c *Controller) BranchTreeText() string {
	branches, err := c.Branches()
	if err != nil {
		return "branches: " + err.Error()
	}
	return FormatBranchTree(branches, agent.BranchID(c.SessionPath()))
}

func FormatBranchTree(branches []agent.BranchInfo, currentID string) string {
	if len(branches) == 0 {
		return "branches: none"
	}
	byID := map[string]agent.BranchInfo{}
	children := map[string][]agent.BranchInfo{}
	for _, b := range branches {
		byID[b.ID] = b
	}
	var roots []agent.BranchInfo
	for _, b := range branches {
		if b.ParentID == "" {
			roots = append(roots, b)
			continue
		}
		if _, ok := byID[b.ParentID]; !ok {
			roots = append(roots, b)
			continue
		}
		children[b.ParentID] = append(children[b.ParentID], b)
	}
	var out strings.Builder
	out.WriteString("branches:\n")
	seen := map[string]bool{}
	var walk func(agent.BranchInfo, int)
	walk = func(b agent.BranchInfo, depth int) {
		if seen[b.ID] {
			return
		}
		seen[b.ID] = true
		marker := " "
		if b.ID == currentID {
			marker = "*"
		}
		name := strings.TrimSpace(b.Name)
		if name == "" {
			name = oneLineBranch(b.Preview, 54)
		}
		if name == "" {
			name = "(untitled)"
		}
		fmt.Fprintf(&out, "%s%s %s  %s  (%d turns)\n",
			strings.Repeat("  ", depth), marker, b.ID, name, b.Turns)
		for _, child := range children[b.ID] {
			walk(child, depth+1)
		}
	}
	for _, root := range roots {
		walk(root, 0)
	}
	for _, b := range branches {
		walk(b, 0)
	}
	return strings.TrimRight(out.String(), "\n")
}

func oneLineBranch(s string, maxRunes int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes <= 1 {
		return string(r[:maxRunes])
	}
	return string(r[:maxRunes-1]) + "..."
}
