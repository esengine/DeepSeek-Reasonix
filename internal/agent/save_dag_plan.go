package agent

import (
	"encoding/json"
	"time"

	"reasonix/internal/provider"
)

// dagWritePlan is the batch a save appends for one head: at most one fork or
// rewind, the overlays for messages that changed in place, and the new tail.
type dagWritePlan struct {
	head        string
	entries     []sessionDAGEntry
	appendFrom  int
	pureAppend  bool
	forked      bool
	rewound     bool
	otherWriter string
	renames     map[string]string
}

// dagDiff describes how the in-memory transcript departs from the persisted
// chain: the common prefix and what changed inside it. adopted maps the ids
// of in-memory messages that matched a persisted message by content only, so
// two sessions that produced the same transcript independently converge.
type dagDiff struct {
	k            int
	patches      []int
	redacts      []int
	systemChange bool
	rewriteAt    int
	adopted      map[string]string
}

// dagHeadView is one head's persisted chain as the planner sees it.
type dagHeadView struct {
	id        string
	head      *sessionDAGHead
	persisted []provider.Message
	chain     []string
	prepended bool
}

// nodeID maps a persisted index to its node id; "" is the prepended system
// override, which is not a node.
func (v dagHeadView) nodeID(i int) string {
	if v.prepended {
		if i == 0 {
			return ""
		}
		return v.chain[i-1]
	}
	return v.chain[i]
}

func (v dagHeadView) parentFor(k int) string {
	if k <= 0 {
		return ""
	}
	return v.nodeID(k - 1)
}

// planDAGWrite diffs msgs against the head's persisted chain by id. Messages
// past the common prefix are appended; a changed message inside it becomes a
// patch (local fields), a system override, a redaction (compact mode), or,
// for any other provider-visible edit, a rewind followed by re-appends. A
// chain another writer extended underneath this session forks a concurrent
// head; a session that merely fell behind reports a stale-prefix conflict.
func (s *Session) planDAGWrite(path string, st *sessionDAGState, msgs []provider.Message, mode sessionSaveMode, now time.Time) (*dagWritePlan, error) {
	s.mu.RLock()
	ref := s.head.ref
	// truncatedLocally: the transcript is a strict prefix of what this session
	// last persisted or loaded, so a shorter transcript is its own truncation
	// (cancel strip, rewind) rather than a sign that it fell behind disk.
	truncatedLocally := len(msgs) < len(s.persistedMessages) && messagesHavePrefix(s.persistedMessages, msgs)
	s.mu.RUnlock()
	head := ref.HeadID
	if head == "" || st.heads[head] == nil {
		head = st.selectedHead()
	}
	view := dagHeadView{id: head, head: st.heads[head]}
	view.persisted, _ = st.materialize(head)
	view.chain = st.chainIDs(head)
	view.prepended = len(view.persisted) == len(view.chain)+1
	// owned: this session's baseline is exactly the head's leaf, so anything
	// shorter or different in memory is this session's own rewrite.
	owned := ref.HeadID == head && ref.LeafID == view.head.leaf

	diff := diffDAGTranscript(view.persisted, msgs, mode)
	plan := &dagWritePlan{head: head, appendFrom: -1, renames: diff.adopted}
	parent, err := plan.moveHead(path, st, view, diff, msgs, owned, truncatedLocally, mode, now)
	if err != nil {
		return nil, err
	}
	if err := plan.addOverlays(view, diff, msgs, now); err != nil {
		return nil, err
	}
	if err := plan.addAppends(st, msgs, diff.k, parent, now); err != nil {
		return nil, err
	}
	plan.pureAppend = !diff.systemChange && len(diff.patches) == 0 && len(diff.redacts) == 0 &&
		!plan.forked && !plan.rewound && diff.k == len(view.persisted) && diff.k < len(msgs)
	if plan.pureAppend {
		plan.appendFrom = diff.k
	}
	return plan, nil
}

func diffDAGTranscript(persisted, msgs []provider.Message, mode sessionSaveMode) dagDiff {
	d := dagDiff{rewriteAt: -1, adopted: map[string]string{}}
	for d.k < len(persisted) && d.k < len(msgs) {
		if persisted[d.k].ID != msgs[d.k].ID {
			if !messagesEqualForStorage(persisted[d.k], msgs[d.k]) {
				break
			}
			d.adopted[msgs[d.k].ID] = persisted[d.k].ID
			msgs[d.k].ID = persisted[d.k].ID
		}
		d.k++
	}
	for i := 0; i < d.k && d.rewriteAt < 0; i++ {
		if messagesEqualForStorage(msgs[i], persisted[i]) {
			continue
		}
		switch {
		case messagesWireEqual(msgs[i], persisted[i]):
			d.patches = append(d.patches, i)
		case i == 0 && msgs[0].Role == provider.RoleSystem && persisted[0].Role == provider.RoleSystem:
			d.systemChange = true
		case mode == sessionSaveRewriteCompact:
			d.redacts = append(d.redacts, i)
		default:
			d.rewriteAt = i
		}
	}
	if d.rewriteAt >= 0 {
		d.k = d.rewriteAt
	}
	return d
}

// moveHead decides whether the save continues the head in place, rewinds it
// (the session owns the leaf, so a shorter or edited transcript is its own
// rewrite), forks a concurrent head (someone else's messages sit past the
// common prefix), or must report that the session merely fell behind. It
// returns the parent id the appended tail hangs from.
func (p *dagWritePlan) moveHead(path string, st *sessionDAGState, view dagHeadView, diff dagDiff, msgs []provider.Message, owned, truncatedLocally bool, mode sessionSaveMode, now time.Time) (string, error) {
	behind := diff.k < len(view.persisted) && diff.k == len(msgs) && diff.rewriteAt < 0
	diverged := diff.k < len(view.persisted) && (diff.k < len(msgs) || diff.rewriteAt >= 0)
	switch {
	case behind && !truncatedLocally:
		return "", &SessionSnapshotConflictError{
			Path: path, Kind: SessionSnapshotConflictStalePrefix,
			ExistingMessages: len(view.persisted), SnapshotMessages: len(msgs),
		}
	case (behind || diverged) && owned:
		parent := view.parentFor(diff.k)
		p.rewound = true
		p.entries = append(p.entries, sessionDAGEntry{Type: sessionDAGTypeRewind, Head: view.id, To: parent, Cause: rewindCause(mode, diff.rewriteAt >= 0), At: now})
		return parent, nil
	case behind || diverged:
		p.head = NewHeadID()
		p.forked = true
		if leafNode := st.nodes[view.head.leaf]; leafNode != nil {
			p.otherWriter = leafNode.writer
		}
		parent := view.parentFor(diff.k)
		p.entries = append(p.entries, sessionDAGEntry{Type: sessionDAGTypeFork, Head: view.id, NewHead: p.head, From: parent, Kind: HeadKindConcurrent, At: now})
		return parent, nil
	}
	return view.head.leaf, nil
}

// addOverlays emits the system override, patches, and redactions for
// messages that changed inside the persisted prefix.
func (p *dagWritePlan) addOverlays(view dagHeadView, diff dagDiff, msgs []provider.Message, now time.Time) error {
	if diff.systemChange {
		raw, err := encodeSessionDAGMessage(msgs[0])
		if err != nil {
			return err
		}
		p.entries = append(p.entries, sessionDAGEntry{Type: sessionDAGTypeSystem, Head: p.head, Msgs: raw, At: now})
	}
	for _, i := range diff.patches {
		raw, err := encodeSessionDAGMessage(msgs[i])
		if err != nil {
			return err
		}
		if id := view.nodeID(i); id != "" {
			p.entries = append(p.entries, sessionDAGEntry{Type: sessionDAGTypePatch, Head: p.head, Target: id, Msgs: raw, At: now})
		} else {
			p.entries = append(p.entries, sessionDAGEntry{Type: sessionDAGTypeSystem, Head: p.head, Msgs: raw, At: now})
		}
	}
	if len(diff.redacts) == 0 {
		return nil
	}
	targets := make(map[string]json.RawMessage, len(diff.redacts))
	for _, i := range diff.redacts {
		raw, err := encodeSessionDAGMessage(msgs[i])
		if err != nil {
			return err
		}
		if id := view.nodeID(i); id != "" {
			targets[id] = raw
		}
	}
	if len(targets) > 0 {
		p.entries = append(p.entries, sessionDAGEntry{Type: sessionDAGTypeRedact, Head: p.head, Targets: targets, Reason: "redaction", At: now})
	}
	return nil
}

// addAppends chains msgs[from:] behind parent. A message whose id already
// names a node (a re-append after a rewind) gets a fresh id, recorded in
// renames so the live session learns it.
func (p *dagWritePlan) addAppends(st *sessionDAGState, msgs []provider.Message, from int, parent string, now time.Time) error {
	parentDigest := ""
	if n := st.nodes[parent]; n != nil {
		parentDigest = n.digest
	}
	for j := from; j < len(msgs); j++ {
		m := msgs[j]
		if _, exists := st.nodes[m.ID]; exists || m.ID == "" {
			fresh := NewMessageID()
			if m.ID != "" {
				p.renames[m.ID] = fresh
			}
			m.ID = fresh
			msgs[j].ID = fresh
		}
		e, err := newSessionDAGMessageEntry(p.head, parent, parentDigest, "", m, now)
		if err != nil {
			return err
		}
		p.entries = append(p.entries, e)
		parent, parentDigest = m.ID, e.Digest
	}
	return nil
}

func rewindCause(mode sessionSaveMode, contentEdit bool) string {
	switch {
	case contentEdit:
		return "content_edit"
	case mode == sessionSaveRewrite, mode == sessionSaveRewriteCompact:
		return "rewrite"
	default:
		return "truncate"
	}
}

// messagesWireEqual reports whether two versions of a message would reach a
// provider identically, so the difference is safe to store as a patch.
func messagesWireEqual(a, b provider.Message) bool {
	if a.Role != b.Role || a.LocalOnly != b.LocalOnly {
		return false
	}
	return providerVisibleFingerprint([]provider.Message{a}) == providerVisibleFingerprint([]provider.Message{b})
}

// applyIDRenames writes the fresh ids of re-appended messages back into the
// live session so the next save recognizes them as persisted.
func (p *dagWritePlan) applyIDRenames(s *Session) {
	if len(p.renames) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Messages {
		if fresh, ok := p.renames[s.Messages[i].ID]; ok {
			s.Messages[i].ID = fresh
		}
	}
}
