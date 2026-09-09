package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"time"

	"reasonix/internal/provider"
)

const (
	// A half-written last line is usually another writer mid-append: re-stat a
	// few times before calling the tail torn.
	sessionDAGTornRetries    = 3
	sessionDAGTornRetryDelay = 50 * time.Millisecond
)

type sessionDAGNode struct {
	id, parent, head, writer, turn string
	offset                         int64
	at                             time.Time
	msg                            provider.Message
	digest                         string
}

type sessionDAGTurn struct {
	turn, leaf, writer string
	preserveUser       bool
	at                 time.Time
}

type sessionDAGCompaction struct {
	coveredLeaf  string
	coveredCount int
	prefixHash   string
	at           time.Time
}

type sessionDAGHead struct {
	id, kind, name, parentHead, forkFrom, writer, leaf string
	system                                             *provider.Message
	createdAt, lastActivity                            time.Time
	lastOffset                                         int64
	retired                                            bool
	compaction                                         *sessionDAGCompaction
	openTurn                                           *sessionDAGTurn
}

type sessionDAGWriter struct {
	id, hostname    string
	pid             int
	leaseGeneration uint64
	lastActivity    time.Time
}

// sessionDAGState is the replayed graph: every node, every head, and the
// overlays (patches, redactions) that materialize applies on read.
type sessionDAGState struct {
	path            string
	generation      int64
	upgradedFrom    int
	nodes           map[string]*sessionDAGNode
	heads           map[string]*sessionDAGHead
	headOrder       []string
	patches         map[string]provider.Message
	redactions      map[string]provider.Message
	writers         map[string]*sessionDAGWriter
	selected        string
	orphans         []string
	records         int
	collectionItems int
	size            int64
	lastGoodEnd     int64
	damaged         bool
	holes           int // unreadable lines skipped between good entries
}

func newSessionDAGState(path string) *sessionDAGState {
	st := &sessionDAGState{
		path:       path,
		nodes:      map[string]*sessionDAGNode{},
		heads:      map[string]*sessionDAGHead{},
		patches:    map[string]provider.Message{},
		redactions: map[string]provider.Message{},
		writers:    map[string]*sessionDAGWriter{},
	}
	st.heads[SessionMainHead] = &sessionDAGHead{id: SessionMainHead, kind: HeadKindMain}
	st.headOrder = []string{SessionMainHead}
	return st
}

// replaySessionDAG decodes a schema-2 log. Decoding stops at the first entry
// that fails to parse (damaged=true, lastGoodEnd set); an unsupported schema
// or entry type is a hard error because a newer writer owns the log.
func replaySessionDAG(ctx context.Context, path string, limits sessionReplayLimits) (*sessionDAGState, error) {
	st := newSessionDAGState(path)
	if err := st.replayFrom(ctx, 0, limits); err != nil {
		return st, err
	}
	for attempt := 0; st.damaged && attempt < sessionDAGTornRetries; attempt++ {
		time.Sleep(sessionDAGTornRetryDelay)
		info, err := os.Stat(path)
		if err != nil || info.Size() <= st.size {
			break
		}
		st.damaged = false
		if err := st.replayFrom(ctx, st.lastGoodEnd, limits); err != nil {
			return st, err
		}
	}
	return st, nil
}

// replayFrom applies every entry from byte offset from to the end of the log.
// Callers use it for the initial pass and for incremental tail reads.
func (st *sessionDAGState) replayFrom(ctx context.Context, from int64, limits sessionReplayLimits) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.Open(st.path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	st.size = info.Size()
	if st.size > limits.maxBytes {
		return sessionReplayLimitError(st.path, "encoded_bytes", st.size, limits.maxBytes)
	}
	if from > 0 {
		if _, err := f.Seek(from, io.SeekStart); err != nil {
			return err
		}
	}
	limited := &io.LimitedReader{R: &contextReader{ctx: ctx, reader: f}, N: limits.maxBytes + 1 - from}
	dec := json.NewDecoder(limited)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var e sessionDAGEntry
		if err := dec.Decode(&e); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if limited.N == 0 {
				return sessionReplayLimitError(st.path, "encoded_bytes", limits.maxBytes+1, limits.maxBytes)
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return st.resumePastTornLine(ctx, limits)
		}
		if e.SchemaVersion != sessionDAGSchemaVersion {
			return fmt.Errorf("decode session event log %s: unsupported schema version %d", st.path, e.SchemaVersion)
		}
		if st.records >= limits.maxRecords {
			return sessionReplayLimitError(st.path, "event_records", int64(st.records+1), int64(limits.maxRecords))
		}
		offset := from + dec.InputOffset()
		ok, err := st.apply(ctx, e, offset, limits)
		if err != nil {
			return err
		}
		if !ok {
			st.damaged = true
			return nil
		}
		st.records++
		st.lastGoodEnd = offset
	}
}

// apply folds one entry into the state. ok=false marks an undecodable entry
// (the replay stops there); err is reserved for hard errors and budget limits.
func (st *sessionDAGState) apply(ctx context.Context, e sessionDAGEntry, offset int64, limits sessionReplayLimits) (bool, error) {
	switch e.Type {
	case sessionDAGTypeLog:
		st.generation = e.Generation
		st.upgradedFrom = e.UpgradedFrom
	case sessionDAGTypeMessage:
		return st.applyMessage(ctx, e, offset, limits)
	case sessionDAGTypePatch, sessionDAGTypeSystem, sessionDAGTypeRedact, sessionDAGTypeWriter:
		return st.applyOverlay(ctx, e, limits)
	case sessionDAGTypeFork, sessionDAGTypeRewind, sessionDAGTypeSelect, sessionDAGTypeRename, sessionDAGTypeRetire,
		sessionDAGTypeTurnBegin, sessionDAGTypeTurnEnd, sessionDAGTypeCompaction:
		return st.applyHeadMarker(e, offset), nil
	case sessionDAGTypeCheckpoint:
	default:
		return false, fmt.Errorf("decode session event log %s: unsupported entry type %q", st.path, e.Type)
	}
	return true, nil
}

func (st *sessionDAGState) applyMessage(ctx context.Context, e sessionDAGEntry, offset int64, limits sessionReplayLimits) (bool, error) {
	if e.ID == "" {
		return false, nil
	}
	m, ok, err := st.decodeOne(ctx, e.Msgs, limits)
	if err != nil || !ok {
		return ok, err
	}
	m.ID = e.ID
	if _, dup := st.nodes[e.ID]; dup {
		slog.Warn("session: ignoring duplicate message entry", "path", st.path, "id", e.ID, "offset", offset)
		return true, nil
	}
	if e.Parent != "" {
		if _, known := st.nodes[e.Parent]; !known {
			st.orphans = append(st.orphans, e.ID)
		}
	}
	st.nodes[e.ID] = &sessionDAGNode{
		id: e.ID, parent: e.Parent, head: e.Head, writer: e.Writer, turn: e.Turn,
		offset: offset, at: e.At, msg: m, digest: e.Digest,
	}
	h := st.headFor(e.Head, e.At)
	h.leaf = e.ID
	h.lastActivity, h.lastOffset = e.At, offset
	if h.writer == "" {
		h.writer = e.Writer
	}
	st.touchWriter(e.Writer, e.At)
	return true, nil
}

// applyOverlay handles the entries that change how messages read without
// moving any head: patch replacements, system overrides, redactions, writer
// identity.
func (st *sessionDAGState) applyOverlay(ctx context.Context, e sessionDAGEntry, limits sessionReplayLimits) (bool, error) {
	switch e.Type {
	case sessionDAGTypePatch:
		if _, known := st.nodes[e.Target]; !known {
			return true, nil
		}
		m, ok, err := st.decodeOne(ctx, e.Msgs, limits)
		if err != nil || !ok {
			return ok, err
		}
		m.ID = e.Target
		st.patches[e.Target] = m
	case sessionDAGTypeSystem:
		m, ok, err := st.decodeOne(ctx, e.Msgs, limits)
		if err != nil || !ok {
			return ok, err
		}
		st.headFor(e.Head, e.At).system = &m
	case sessionDAGTypeRedact:
		for id, raw := range e.Targets {
			m, ok, err := st.decodeOne(ctx, raw, limits)
			if err != nil || !ok {
				return ok, err
			}
			m.ID = id
			st.redactions[id] = m
		}
	case sessionDAGTypeWriter:
		w := st.touchWriter(e.Writer, e.At)
		w.pid, w.hostname, w.leaseGeneration = e.PID, e.Hostname, e.LeaseGeneration
	}
	return true, nil
}

// applyHeadMarker handles the entries that create or move heads. Only a fork
// without a new head id is undecodable.
func (st *sessionDAGState) applyHeadMarker(e sessionDAGEntry, offset int64) bool {
	switch e.Type {
	case sessionDAGTypeFork:
		if e.NewHead == "" {
			return false
		}
		if _, exists := st.heads[e.NewHead]; exists {
			return true
		}
		parent := st.headFor(e.Head, e.At)
		kind := e.Kind
		if kind == "" {
			kind = HeadKindFork
		}
		st.heads[e.NewHead] = &sessionDAGHead{
			id: e.NewHead, kind: kind, name: e.Name, parentHead: parent.id, forkFrom: e.From,
			writer: e.Writer, leaf: e.From, system: parent.system,
			createdAt: e.At, lastActivity: e.At, lastOffset: offset,
		}
		st.headOrder = append(st.headOrder, e.NewHead)
		st.touchWriter(e.Writer, e.At)
	case sessionDAGTypeRewind:
		h := st.headFor(e.Head, e.At)
		h.leaf = e.To
		h.lastActivity, h.lastOffset = e.At, offset
		st.touchWriter(e.Writer, e.At)
	case sessionDAGTypeSelect:
		st.selected = e.Head
	case sessionDAGTypeRename:
		st.headFor(e.Head, e.At).name = e.Name
	case sessionDAGTypeRetire:
		st.headFor(e.Head, e.At).retired = true
	case sessionDAGTypeTurnBegin:
		st.headFor(e.Head, e.At).openTurn = &sessionDAGTurn{turn: e.Turn, leaf: e.Leaf, writer: e.Writer, preserveUser: e.PreserveUser, at: e.At}
	case sessionDAGTypeTurnEnd:
		h := st.headFor(e.Head, e.At)
		if h.openTurn != nil && (e.Turn == "" || h.openTurn.turn == e.Turn) {
			h.openTurn = nil
		}
	case sessionDAGTypeCompaction:
		st.headFor(e.Head, e.At).compaction = &sessionDAGCompaction{
			coveredLeaf: e.CoveredLeaf, coveredCount: e.CoveredCount, prefixHash: e.PrefixHash, at: e.At,
		}
	}
	return true
}

// decodeOne decodes the single-message array carried by message, system, and
// redact entries through the same bounded decoder as schema-1 records.
func (st *sessionDAGState) decodeOne(ctx context.Context, raw json.RawMessage, limits sessionReplayLimits) (provider.Message, bool, error) {
	msgs, items, err := decodeSessionEventMessages(ctx, st.path, raw, len(st.nodes), st.collectionItems, limits)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return provider.Message{}, false, ctxErr
		}
		if errors.Is(err, ErrSessionReplayLimitExceeded) {
			return provider.Message{}, false, err
		}
		return provider.Message{}, false, nil
	}
	if len(msgs) != 1 {
		return provider.Message{}, false, nil
	}
	st.collectionItems = items
	return msgs[0], true, nil
}

// headFor resolves a head id, creating an undeclared head rather than dropping
// the entries that reference it; an empty id means the main head.
func (st *sessionDAGState) headFor(id string, at time.Time) *sessionDAGHead {
	if id == "" {
		id = SessionMainHead
	}
	h := st.heads[id]
	if h == nil {
		h = &sessionDAGHead{id: id, createdAt: at}
		st.heads[id] = h
		st.headOrder = append(st.headOrder, id)
	}
	if h.createdAt.IsZero() {
		h.createdAt = at
	}
	return h
}

func (st *sessionDAGState) touchWriter(id string, at time.Time) *sessionDAGWriter {
	w := st.writers[id]
	if w == nil {
		w = &sessionDAGWriter{id: id}
		st.writers[id] = w
	}
	if at.After(w.lastActivity) {
		w.lastActivity = at
	}
	return w
}

// chainIDs returns the message ids of a head from root to leaf. It stops at an
// orphan root (parent never seen) so a rotated-away prefix degrades to a
// shorter transcript instead of a failed load.
func (st *sessionDAGState) chainIDs(headID string) []string {
	h := st.heads[headID]
	if h == nil {
		return nil
	}
	var ids []string
	seen := map[string]struct{}{}
	for id := h.leaf; id != ""; {
		n := st.nodes[id]
		if n == nil {
			break
		}
		if _, cyc := seen[id]; cyc {
			break
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
		id = n.parent
	}
	slices.Reverse(ids)
	return ids
}

// appliedMessage is one node with its latest patch and any redaction
// substituted; a patch is provider-equivalent to the original by contract, so
// the swap never touches provider-visible bytes.
func (st *sessionDAGState) appliedMessage(n *sessionDAGNode) provider.Message {
	m := n.msg
	if p, ok := st.patches[n.id]; ok {
		m = p
	}
	if r, ok := st.redactions[n.id]; ok {
		m = r
	}
	m.ID = n.id
	return m
}

// materialize builds the transcript of one head: the parent chain with
// patches merged, redactions substituted, and the head's system override at
// position 0. times mirror msgs with each entry's append time.
func (st *sessionDAGState) materialize(headID string) ([]provider.Message, []time.Time) {
	ids := st.chainIDs(headID)
	msgs := make([]provider.Message, 0, len(ids)+1)
	times := make([]time.Time, 0, len(ids)+1)
	for _, id := range ids {
		n := st.nodes[id]
		msgs = append(msgs, st.appliedMessage(n))
		times = append(times, n.at)
	}
	if h := st.heads[headID]; h != nil && h.system != nil {
		sys := *h.system
		if len(msgs) > 0 && msgs[0].Role == provider.RoleSystem {
			sys.ID = msgs[0].ID
			msgs[0] = sys
		} else {
			// The writer stamps the prepended system message's id on the marker
			// so every reader materializes the same id.
			msgs = append([]provider.Message{sys}, msgs...)
			times = append([]time.Time{h.createdAt}, times...)
		}
	}
	return msgs, times
}
