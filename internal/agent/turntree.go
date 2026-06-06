package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"reasonix/internal/provider"
)

const (
	turnPromptSummaryRunes   = 160
	turnResponseSummaryRunes = 240
)

// TurnNode represents one conversational turn as a node in the session branch
// tree. Every user message marks a turn boundary; the tree links them linearly
// within a branch and attaches forks as children of the turn they fork from.
type TurnNode struct {
	// Identity
	BranchID string `json:"branch_id"` // owning .jsonl file base name (no ext)
	Turn     int    `json:"turn"`      // zero-based turn index within the branch

	// Content
	Prompt      string `json:"prompt"`       // first user-message content, normalized to one line
	Response    string `json:"response"`     // last assistant-message content in the turn, normalized to one line
	PrefixChars int    `json:"prefix_chars"` // exact content chars through this complete turn

	// Position in the session file: the message index where this turn's user
	// message sits, and the exclusive end index of the complete turn. Together
	// with BranchID these boundaries give deterministic reload.
	MessageIndex    int `json:"message_index"`
	EndMessageIndex int `json:"end_message_index"`

	// Metadata
	Time      time.Time `json:"time"`
	IsCurrent bool      `json:"is_current,omitempty"` // the active position in the current session

	// Tree links
	Next     *TurnNode   `json:"-"` // next turn in the same branch (linear successor)
	Children []*TurnNode `json:"-"` // forks that branch from this turn

	// Internal: the JSONL file path this node was parsed from.
	filePath string
}

// TurnTree collects all session branches into a navigable turn-level tree.
type TurnTree struct {
	Roots      []*TurnNode          `json:"roots"`
	Nodes      []*TurnNode          `json:"nodes"` // flat list for pickers
	nodeByKey  map[string]*TurnNode // key = "branchID:turn"
	CurrentKey string               // branchID:turn of the active node
}

// NodeKey returns a stable lookup key for a turn node.
func NodeKey(branchID string, turn int) string {
	return fmt.Sprintf("%s:%d", branchID, turn)
}

// BuildTurnTree scans a session directory, loads every .jsonl + .meta, discovers
// turn boundaries (user messages), and assembles the TurnTree. currentPath is the
// active session file.
func BuildTurnTree(dir, currentPath string) (*TurnTree, error) {
	branches, err := ListBranches(dir)
	if err != nil {
		return nil, err
	}
	if len(branches) == 0 {
		return &TurnTree{}, nil
	}

	currentID := BranchID(currentPath)

	tree := &TurnTree{
		nodeByKey: make(map[string]*TurnNode),
	}

	// Phase 1: for each branch, parse its turns from the .jsonl and create the
	// visible chain of unique TurnNodes. Child branches persist the inherited
	// prefix in their own .jsonl file, so skip messages before ForkMessageIndex;
	// otherwise the same historical text would appear twice in the tree.
	branchChains := make(map[string][]*TurnNode) // branchID -> visible turns
	allChains := make(map[string][]*TurnNode)    // branchID -> all parsed turns
	branchMeta := make(map[string]BranchInfo)    // branchID → meta

	for _, b := range branches {
		id := b.ID
		branchMeta[id] = b
		nodes, err := parseTurns(b.Path, id)
		if err != nil {
			continue // skip unparseable
		}
		allChains[id] = nodes
		if b.ParentID != "" {
			nodes = visibleBranchTurns(nodes, b.ForkMessageIndex)
		}
		// Chain them linearly
		for i := 0; i < len(nodes)-1; i++ {
			nodes[i].Next = nodes[i+1]
		}
		for _, n := range nodes {
			tree.nodeByKey[NodeKey(id, n.Turn)] = n
			tree.Nodes = append(tree.Nodes, n)
		}
		branchChains[id] = nodes
	}

	// Phase 2: link child branches to their parent turn. Branches can be created
	// from another branch before it has any unique turns; in that case, resolve
	// the parent through inherited metadata to the nearest visible ancestor node.
	for id, nodes := range branchChains {
		meta := branchMeta[id]
		if meta.ParentID == "" {
			continue // root branch
		}
		parentNode := resolveVisibleParentNode(meta.ParentID, meta.ForkTurn, branchMeta, tree.nodeByKey)
		if parentNode != nil {
			if len(nodes) > 0 {
				parentNode.Children = append(parentNode.Children, nodes[0])
			}
		}
	}

	// Phase 3: identify roots — branches with no parent or whose parent turn
	// doesn't exist.
	rootIDs := map[string]bool{}
	for _, b := range branches {
		if b.ParentID == "" {
			rootIDs[b.ID] = true
			continue
		}
		if resolveVisibleParentNode(b.ParentID, b.ForkTurn, branchMeta, tree.nodeByKey) == nil {
			rootIDs[b.ID] = true
		}
	}
	for id, nodes := range branchChains {
		if rootIDs[id] && len(nodes) > 0 {
			tree.Roots = append(tree.Roots, nodes[0])
		}
	}

	// Sort roots by creation time
	sort.Slice(tree.Roots, func(i, j int) bool {
		bi := branchMeta[tree.Roots[i].BranchID]
		bj := branchMeta[tree.Roots[j].BranchID]
		if bi.CreatedAt.Equal(bj.CreatedAt) {
			return tree.Roots[i].BranchID < tree.Roots[j].BranchID
		}
		return bi.CreatedAt.Before(bj.CreatedAt)
	})

	sortChildren(tree.Roots, branchMeta)

	// Phase 4: mark current node. If the active branch contains no unique turns
	// yet (for example immediately after jumping to a historical node), show the
	// current position on its parent fork node.
	currentMeta := branchMeta[currentID]
	currentKey := ""
	if nodes := branchChains[currentID]; len(nodes) > 0 {
		currentKey = NodeKey(nodes[len(nodes)-1].BranchID, nodes[len(nodes)-1].Turn)
	} else if currentMeta.ParentID != "" {
		if parentNode := resolveVisibleParentNode(currentMeta.ParentID, currentMeta.ForkTurn, branchMeta, tree.nodeByKey); parentNode != nil {
			currentKey = NodeKey(parentNode.BranchID, parentNode.Turn)
		}
	} else if nodes := allChains[currentID]; len(nodes) > 0 {
		currentKey = NodeKey(nodes[len(nodes)-1].BranchID, nodes[len(nodes)-1].Turn)
	}
	for _, n := range tree.Nodes {
		if NodeKey(n.BranchID, n.Turn) == currentKey {
			n.IsCurrent = true
			tree.CurrentKey = NodeKey(n.BranchID, n.Turn)
		}
	}

	return tree, nil
}

func resolveVisibleParentNode(branchID string, turn int, branchMeta map[string]BranchInfo, nodes map[string]*TurnNode) *TurnNode {
	seen := map[string]bool{}
	for branchID != "" {
		key := NodeKey(branchID, turn)
		if node, ok := nodes[key]; ok {
			return node
		}
		if seen[key] {
			return nil
		}
		seen[key] = true
		meta, ok := branchMeta[branchID]
		if !ok || meta.ParentID == "" || turn > meta.ForkTurn {
			return nil
		}
		branchID = meta.ParentID
	}
	return nil
}

func visibleBranchTurns(nodes []*TurnNode, forkMessageIndex int) []*TurnNode {
	if forkMessageIndex <= 0 {
		return nodes
	}
	first := len(nodes)
	for i, n := range nodes {
		if n.MessageIndex >= forkMessageIndex {
			first = i
			break
		}
	}
	if first >= len(nodes) {
		return nil
	}
	return nodes[first:]
}

func sortChildren(roots []*TurnNode, branchMeta map[string]BranchInfo) {
	seen := map[string]bool{}
	var walk func(*TurnNode)
	walk = func(n *TurnNode) {
		if n == nil {
			return
		}
		key := NodeKey(n.BranchID, n.Turn)
		if seen[key] {
			return
		}
		seen[key] = true
		sort.Slice(n.Children, func(i, j int) bool {
			bi := branchMeta[n.Children[i].BranchID]
			bj := branchMeta[n.Children[j].BranchID]
			if bi.CreatedAt.Equal(bj.CreatedAt) {
				return n.Children[i].BranchID < n.Children[j].BranchID
			}
			return bi.CreatedAt.Before(bj.CreatedAt)
		})
		walk(n.Next)
		for _, child := range n.Children {
			walk(child)
		}
	}
	for _, root := range roots {
		walk(root)
	}
}

// FindNode locates a node by branchID and turn.
func (t *TurnTree) FindNode(branchID string, turn int) *TurnNode {
	return t.nodeByKey[NodeKey(branchID, turn)]
}

// parseTurns reads a single .jsonl session file and extracts turn boundaries.
// A turn starts at each user message and ends immediately before the next user
// message. The full user text is kept stable; renderers decide how much to show.
func parseTurns(path, branchID string) ([]*TurnNode, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var nodes []*TurnNode
	dec := json.NewDecoder(f)
	idx := 0
	turn := 0
	prefixChars := 0

	for {
		var m provider.Message
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode turn %s: %w", path, err)
		}
		if m.Role == provider.RoleUser {
			if len(nodes) > 0 && nodes[len(nodes)-1].EndMessageIndex == 0 {
				nodes[len(nodes)-1].EndMessageIndex = idx
				nodes[len(nodes)-1].PrefixChars = prefixChars
			}
			nodes = append(nodes, &TurnNode{
				BranchID:     branchID,
				Turn:         turn,
				Prompt:       stablePrompt(m.Content),
				MessageIndex: idx,
				filePath:     path,
			})
			turn++
		} else if m.Role == provider.RoleAssistant && len(nodes) > 0 {
			nodes[len(nodes)-1].Response = stablePrompt(m.Content)
		}
		prefixChars += messageContentChars(m)
		idx++
	}
	if len(nodes) > 0 && nodes[len(nodes)-1].EndMessageIndex == 0 {
		nodes[len(nodes)-1].EndMessageIndex = idx
		nodes[len(nodes)-1].PrefixChars = prefixChars
	}
	return nodes, nil
}

func messageContentChars(m provider.Message) int {
	return len([]rune(m.Content))
}

func stablePrompt(content string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
}

// TurnInfo is a picker-friendly summary of a turn, lightweight enough for the
// tree overlay to render without loading full message bodies.
type TurnInfo struct {
	BranchID    string `json:"branch_id"`
	BranchName  string `json:"branch_name,omitempty"`
	Turn        int    `json:"turn"`
	Prompt      string `json:"prompt"`
	Response    string `json:"response"`
	PrefixChars int    `json:"prefix_chars"`
	IsCurrent   bool   `json:"is_current"`
	Depth       int    `json:"depth"`       // tree depth for indentation
	HasFork     bool   `json:"has_fork"`    // true when another branch forks from here
	ChildCount  int    `json:"child_count"` // number of direct fork children
	Key         string `json:"key"`
	ParentKey   string `json:"parent_key"`
	RootKey     string `json:"root_key"`
	RootIndex   int    `json:"root_index"`
}

// Flatten returns a depth-first flattened view of the tree for a picker list.
// The current node is included; its key is set in the tree beforehand.
func (t *TurnTree) Flatten() []TurnInfo {
	var out []TurnInfo
	seen := map[string]bool{}

	var walk func(n *TurnNode, depth int, rootKey string, rootIndex int, parentKey string)
	walk = func(n *TurnNode, depth int, rootKey string, rootIndex int, parentKey string) {
		if n == nil {
			return
		}
		key := NodeKey(n.BranchID, n.Turn)
		if seen[key] {
			return
		}
		seen[key] = true

		out = append(out, TurnInfo{
			BranchID:    n.BranchID,
			Turn:        n.Turn,
			Prompt:      turnSummary(n.Prompt, turnPromptSummaryRunes),
			Response:    turnSummary(n.Response, turnResponseSummaryRunes),
			PrefixChars: n.PrefixChars,
			IsCurrent:   n.IsCurrent,
			Depth:       depth,
			HasFork:     len(n.Children) > 0,
			ChildCount:  len(n.Children),
			Key:         key,
			ParentKey:   parentKey,
			RootKey:     rootKey,
			RootIndex:   rootIndex,
		})

		// Forks stay directly below the node they split from, so the visual tree
		// shows the branch point instead of burying children after later turns.
		for _, child := range n.Children {
			walk(child, depth+1, rootKey, rootIndex, key)
		}

		// Next in same branch
		walk(n.Next, depth, rootKey, rootIndex, key)
	}

	for i, root := range t.Roots {
		walk(root, 0, NodeKey(root.BranchID, root.Turn), i, "")
	}
	return out
}

// FlattenCurrentRoot returns the visible turn nodes for the root that contains
// the current node. UIs use this focused view when opening a tree from a
// specific session, while Flatten remains available for full-session listings.
func (t *TurnTree) FlattenCurrentRoot() []TurnInfo {
	flat := t.Flatten()
	currentRootKey := ""
	for _, n := range flat {
		if n.Key == t.CurrentKey {
			currentRootKey = n.RootKey
			break
		}
	}
	if currentRootKey == "" {
		return nil
	}
	out := make([]TurnInfo, 0, len(flat))
	for _, n := range flat {
		if n.RootKey == currentRootKey {
			out = append(out, n)
		}
	}
	return out
}

func turnSummary(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return strings.TrimSpace(string(runes[:maxRunes-3])) + "..."
}

// SetCurrent marks exactly one node as current when the active conversation is a
// temporary in-memory preview rather than a persisted branch file.
func (t *TurnTree) SetCurrent(branchID string, turn int) {
	key := NodeKey(branchID, turn)
	t.CurrentKey = ""
	for _, n := range t.Nodes {
		n.IsCurrent = NodeKey(n.BranchID, n.Turn) == key
		if n.IsCurrent {
			t.CurrentKey = key
		}
	}
}

// PathTo returns the linear turn sequence from the root to the given node,
// following Next links and crossing into child branches when the node is in one.
func (t *TurnTree) PathTo(target *TurnNode) []*TurnNode {
	if target == nil {
		return nil
	}
	// Walk backwards: find the root, then collect forward.
	// Since we don't store parent pointers, we search from each root.
	for _, root := range t.Roots {
		path := t.pathFrom(root, target, nil)
		if path != nil {
			return path
		}
	}
	return nil
}

func (t *TurnTree) pathFrom(current, target *TurnNode, path []*TurnNode) []*TurnNode {
	if current == nil {
		return nil
	}
	key := NodeKey(current.BranchID, current.Turn)
	for _, p := range path {
		if NodeKey(p.BranchID, p.Turn) == key {
			return nil // cycle guard
		}
	}
	path = append(path, current)
	if key == NodeKey(target.BranchID, target.Turn) {
		return path
	}
	// Try next in branch
	if found := t.pathFrom(current.Next, target, path); found != nil {
		return found
	}
	// Try fork children
	for _, child := range current.Children {
		if found := t.pathFrom(child, target, path); found != nil {
			return found
		}
	}
	return nil
}

// LastInBranch returns the tip node of a branch (the last turn), used to detect
// the current position and append new turns.
func (t *TurnTree) LastInBranch(branchID string) *TurnNode {
	var last *TurnNode
	for _, n := range t.Nodes {
		if n.BranchID == branchID {
			last = n
		}
	}
	return last
}
