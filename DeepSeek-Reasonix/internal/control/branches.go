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
	var walk func(agent.BranchInfo, string, bool, int)
	walk = func(b agent.BranchInfo, prefix string, last bool, depth int) {
		if seen[b.ID] {
			return
		}
		seen[b.ID] = true
		joint := "├─"
		childPrefix := prefix + "│  "
		if last {
			joint = "└─"
			childPrefix = prefix + "   "
		}
		current := ""
		if b.ID == currentID {
			current = "  current"
		}
		fmt.Fprintf(&out, "%s%s %s  %s  %s%s\n",
			prefix, joint, shortBranchID(b.ID), branchTitle(b, depth), turnText(b.Turns), current)
		for i, child := range children[b.ID] {
			walk(child, childPrefix, i == len(children[b.ID])-1, depth+1)
		}
	}
	for i, root := range roots {
		walk(root, "", i == len(roots)-1, 0)
	}
	for _, b := range branches {
		walk(b, "", true, 0)
	}
	return strings.TrimRight(out.String(), "\n")
}

func branchTitle(b agent.BranchInfo, depth int) string {
	title := strings.TrimSpace(b.Name)
	if title == "" {
		title = strings.TrimSpace(b.Preview)
	}
	if label, ok := structuredBranchLabel(title); ok {
		return label
	}
	maxRunes := 32 - depth*4
	if maxRunes < 18 {
		maxRunes = 18
	}
	title = oneLineBranch(title, maxRunes)
	if title == "" {
		return "(untitled)"
	}
	return title
}

func structuredBranchLabel(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	switch s[0] {
	case '{':
		lower := strings.ToLower(s)
		switch {
		case strings.Contains(lower, `"msg"`) && strings.Contains(lower, "success"):
			return "JSON response: success", true
		case strings.Contains(lower, `"error"`) || strings.Contains(lower, `"errors"`):
			return "JSON payload: error", true
		default:
			return "JSON object", true
		}
	case '[':
		return "JSON array", true
	default:
		return "", false
	}
}

func turnText(n int) string {
	if n == 1 {
		return "1 turn"
	}
	return fmt.Sprintf("%d turns", n)
}

func shortBranchID(id string) string {
	if len(id) >= 16 && numeric(id[:8]) && id[8] == '-' && numeric(id[9:15]) && id[15] == '.' {
		fracEnd := 16
		for fracEnd < len(id) && fracEnd < 19 && id[fracEnd] >= '0' && id[fracEnd] <= '9' {
			fracEnd++
		}
		if fracEnd > 16 {
			return id[4:8] + "-" + id[9:15] + "." + id[16:fracEnd]
		}
		return id[4:8] + "-" + id[9:15]
	}
	return oneLineBranch(id, 18)
}

func numeric(s string) bool {
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return s != ""
}

func oneLineBranch(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
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
