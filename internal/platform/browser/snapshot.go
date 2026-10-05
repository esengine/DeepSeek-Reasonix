package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxNameRunes = 160
	maxTextRunes = 300
)

// Snapshot is a page as its accessibility tree presents it: one line per
// element, indented by nesting, each actionable element carrying a ref.
type Snapshot struct {
	Tab    TabInfo
	Lines  []string
	Dialog *Dialog
}

type axValue struct {
	Value json.RawMessage `json:"value"`
}

func (v *axValue) String() string {
	if v == nil || len(v.Value) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(v.Value, &s) == nil {
		return s
	}
	return string(v.Value)
}

type axNode struct {
	NodeID           string   `json:"nodeId"`
	Ignored          bool     `json:"ignored"`
	Role             *axValue `json:"role"`
	Name             *axValue `json:"name"`
	Value            *axValue `json:"value"`
	ChildIDs         []string `json:"childIds"`
	ParentID         string   `json:"parentId"`
	BackendDOMNodeID int64    `json:"backendDOMNodeId"`
	Properties       []struct {
		Name  string  `json:"name"`
		Value axValue `json:"value"`
	} `json:"properties"`
}

// Snapshot renders a tab's page. scope, when set, is a ref whose subtree alone
// is rendered.
func (s *Session) Snapshot(ctx context.Context, tabID, scope string) (Snapshot, error) {
	t, err := s.tab(tabID)
	if err != nil {
		return Snapshot{}, err
	}
	if err := t.checkCurrentURL(); err != nil {
		return Snapshot{}, err
	}
	var tree struct {
		Nodes []axNode `json:"nodes"`
	}
	if err := t.call(ctx, "Accessibility.getFullAXTree", nil, &tree); err != nil {
		return Snapshot{}, engineFailure(err)
	}
	r := renderer{s: s, tab: t.id, nodes: make(map[string]*axNode, len(tree.Nodes))}
	var root *axNode
	for i := range tree.Nodes {
		n := &tree.Nodes[i]
		r.nodes[n.NodeID] = n
		if n.ParentID == "" && root == nil {
			root = n
		}
	}
	if scope != "" {
		target, err := s.refs.resolve(scope)
		if err != nil {
			return Snapshot{}, err
		}
		root = nil
		for i := range tree.Nodes {
			if tree.Nodes[i].BackendDOMNodeID == target.node {
				root = &tree.Nodes[i]
				break
			}
		}
		if root == nil {
			return Snapshot{}, &Failure{Code: CodeStaleRef, Ref: scope, Detail: fmt.Sprintf("%s is no longer on the page", scope)}
		}
		r.node(root, 0, "")
	} else if root != nil {
		r.children(root, 0, root.Name.String())
		t.mu.Lock()
		t.seen = seenSnapshot{lines: r.lines, document: t.navigated, valid: true}
		t.mu.Unlock()
	}
	return Snapshot{Tab: t.info(true), Lines: r.lines, Dialog: t.currentDialog()}, nil
}

// Changes is how a page differs from the last snapshot the agent saw of it.
// NewDocument means there is nothing to compare: the tab navigated since, or
// the agent never saw it whole.
type Changes struct {
	NewDocument bool
	Added       []string
	Removed     []string
}

// Refresh snapshots a tab and reports what changed since the agent's last look.
func (s *Session) Refresh(ctx context.Context, tabID string) (Snapshot, Changes, error) {
	t, err := s.tab(tabID)
	if err != nil {
		return Snapshot{}, Changes{}, err
	}
	t.mu.Lock()
	prev := t.seen
	t.mu.Unlock()
	snap, err := s.Snapshot(ctx, t.id, "")
	if err != nil {
		return Snapshot{}, Changes{}, err
	}
	t.mu.Lock()
	document := t.navigated
	t.mu.Unlock()
	if !prev.valid || prev.document != document {
		return snap, Changes{NewDocument: true}, nil
	}
	return snap, diffLines(prev.lines, snap.Lines), nil
}

// diffLines lists lines that appeared and disappeared, each in page order. A
// line that moved is neither.
func diffLines(before, after []string) Changes {
	remaining := make(map[string]int, len(before))
	for _, l := range before {
		remaining[l]++
	}
	var c Changes
	for _, l := range after {
		if remaining[l] > 0 {
			remaining[l]--
			continue
		}
		c.Added = append(c.Added, l)
	}
	for _, l := range before {
		if remaining[l] > 0 {
			remaining[l]--
			c.Removed = append(c.Removed, l)
		}
	}
	return c
}

type renderer struct {
	s     *Session
	tab   string
	nodes map[string]*axNode
	lines []string
}

func role(n *axNode) string {
	if n.Role == nil {
		return ""
	}
	return n.Role.String()
}

// transparent roles carry no meaning of their own when unnamed: their children
// are rendered in their place.
var transparentRoles = map[string]bool{"generic": true, "none": true, "presentation": true, "": true}

func (r *renderer) children(n *axNode, depth int, parentName string) {
	var text []string
	flush := func() {
		if len(text) == 0 {
			return
		}
		joined := strings.Join(text, " ")
		text = nil
		if joined == parentName {
			return
		}
		r.lines = append(r.lines, indent(depth)+"- text: "+strconv.Quote(clip(joined, maxTextRunes)))
	}
	for _, id := range n.ChildIDs {
		child := r.nodes[id]
		if child == nil {
			continue
		}
		if !child.Ignored && role(child) == "StaticText" {
			if s := strings.TrimSpace(child.Name.String()); s != "" {
				text = append(text, s)
			}
			continue
		}
		flush()
		r.node(child, depth, parentName)
	}
	flush()
}

func (r *renderer) node(n *axNode, depth int, parentName string) {
	kind := role(n)
	name := strings.TrimSpace(n.Name.String())
	switch {
	case kind == "InlineTextBox" || kind == "LineBreak":
		return
	case n.Ignored || (transparentRoles[kind] && name == ""):
		r.children(n, depth, parentName)
		return
	}
	line := indent(depth) + "- " + kind
	if name != "" {
		line += " " + strconv.Quote(clip(name, maxNameRunes))
	}
	if n.BackendDOMNodeID > 0 {
		line += " [" + r.s.refs.refFor(r.tab, n.BackendDOMNodeID) + "]"
	}
	if v := strings.TrimSpace(n.Value.String()); v != "" && v != name {
		line += " value=" + strconv.Quote(clip(v, maxNameRunes))
	}
	line += properties(n)
	if kind == "Iframe" {
		line += " (frame content is not included)"
	}
	r.lines = append(r.lines, line)
	r.children(n, depth+1, name)
}

func properties(n *axNode) string {
	var b strings.Builder
	for _, p := range n.Properties {
		v := p.Value.String()
		switch p.Name {
		case "focused", "disabled", "required", "readonly", "multiselectable":
			if v == "true" {
				b.WriteString(" " + p.Name)
			}
		case "checked", "pressed", "selected", "expanded":
			if v == "true" || v == "false" || v == "mixed" {
				b.WriteString(" " + p.Name + "=" + v)
			}
		case "invalid":
			if v != "" && v != "false" {
				b.WriteString(" invalid")
			}
		case "level":
			b.WriteString(" level=" + v)
		case "url":
			if v != "" {
				b.WriteString(" → " + clip(v, maxNameRunes))
			}
		}
	}
	return b.String()
}

func indent(depth int) string { return strings.Repeat("  ", depth) }

func clip(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit]) + "…"
}
