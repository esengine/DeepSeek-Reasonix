package sessionstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

// Schema 2 is the event log Reasonix 1.x writes from 1.38.2: an append-only
// DAG where every message names its parent, heads point into the graph, and
// rewinds, forks, patches and redactions are markers appended behind the
// messages they touch. This build reads it and never writes it.
const sessionDAGSchemaVersion = 2

// ErrSessionLogForeign means the session's event log belongs to a line this
// build reads but does not write. Its transcript opens; continuing it has to
// land somewhere else, because anything written beside that log is invisible
// to the line that owns it.
var ErrSessionLogForeign = errors.New("session event log is written by Reasonix 1.x")

// ErrSessionLogUnchanged means a save found the 1.x log already holding the
// transcript, so nothing was written — not the transcript and not its
// sidecars, which also belong to 1.x.
var ErrSessionLogUnchanged = errors.New("session event log written by Reasonix 1.x already holds this transcript")

const sessionDAGMainHead = "main"

type sessionDAGEntry struct {
	SchemaVersion int                        `json:"schema_version"`
	Type          string                     `json:"type"`
	ID            string                     `json:"id,omitempty"`
	Head          string                     `json:"head,omitempty"`
	At            time.Time                  `json:"at"`
	Parent        string                     `json:"parent,omitempty"`
	Msgs          json.RawMessage            `json:"msgs,omitempty"`
	Target        string                     `json:"target,omitempty"`
	NewHead       string                     `json:"new_head,omitempty"`
	From          string                     `json:"from,omitempty"`
	To            string                     `json:"to,omitempty"`
	Targets       map[string]json.RawMessage `json:"targets,omitempty"`
}

type sessionDAGNode struct {
	parent  string
	at      time.Time
	payload sessionDAGPayload
}

type sessionDAGHead struct {
	leaf         string
	system       *sessionDAGPayload
	lastActivity time.Time
	lastOffset   int64
	retired      bool
}

// sessionDAGState retains topology and file offsets; message bodies are loaded
// only for the effective selected transcript.
type sessionDAGState struct {
	path            string
	nodes           map[string]*sessionDAGNode
	heads           map[string]*sessionDAGHead
	headOrder       []string
	patches         map[string]sessionDAGPayload
	redactions      map[string]sessionDAGPayload
	selected        string
	collectionItems int
	metadataBytes   int64
	records         int
	damaged         bool
}

// replaySessionDAG decodes a schema 2 log. It stops at the first entry that
// does not decode and reports the transcript up to there as damaged; an entry
// type this reader does not know is a hard error, since the log's meaning
// would change under it.
func replaySessionDAG(f *os.File, path string, limits sessionReplayLimits) (*sessionDAGState, error) {
	st := &sessionDAGState{
		path:       path,
		nodes:      map[string]*sessionDAGNode{},
		heads:      map[string]*sessionDAGHead{sessionDAGMainHead: {}},
		headOrder:  []string{sessionDAGMainHead},
		patches:    map[string]sessionDAGPayload{},
		redactions: map[string]sessionDAGPayload{},
	}
	info, err := f.Stat()
	if err != nil {
		return st, err
	}
	if info.Size() > limits.maxBytes {
		return st, sessionReplayLimitError(path, "encoded_bytes", info.Size(), limits.maxBytes)
	}
	reader := newSessionRecordReader(limits.reader(f), path, limits.maxBytes)
	for {
		if err := limits.err(); err != nil {
			return st, err
		}
		raw, start, err := reader.next()
		if err != nil {
			if err := limits.err(); err != nil {
				return st, err
			}
			if errors.Is(err, ErrSessionReplayLimitExceeded) {
				return st, err
			}
			if !errors.Is(err, io.EOF) {
				st.damaged = true
			}
			return st, nil
		}
		var e sessionDAGEntry
		if err := json.Unmarshal(raw, &e); err != nil {
			st.damaged = true
			return st, nil
		}
		if e.SchemaVersion != sessionDAGSchemaVersion {
			return st, fmt.Errorf("decode session event log %s: unsupported schema version %d", path, e.SchemaVersion)
		}
		if st.records >= limits.maxRecords {
			return st, sessionReplayLimitError(path, "event_records", int64(st.records+1), int64(limits.maxRecords))
		}
		metadataBytes := len(e.ID) + len(e.Head) + len(e.Parent) + len(e.Target) + len(e.NewHead) + len(e.From) + len(e.To)
		for id := range e.Targets {
			metadataBytes += len(id)
		}
		if err := st.chargeMetadata(int64(metadataBytes)); err != nil {
			return st, err
		}
		locations, err := locateSessionDAGPayloads(raw, start)
		if err != nil {
			st.damaged = true
			return st, nil
		}
		ok, err := st.apply(e, reader.offset, locations, limits)
		if err := limits.err(); err != nil {
			return st, err
		}
		if err != nil {
			return st, err
		}
		if !ok {
			st.damaged = true
			return st, nil
		}
		st.records++
	}
}

// apply folds one entry into the state. ok=false marks an entry that does not
// decode; err is kept for unknown types and replay limits.
func (st *sessionDAGState) apply(e sessionDAGEntry, offset int64, locations sessionDAGLocations, limits sessionReplayLimits) (bool, error) {
	switch e.Type {
	case "message":
		return st.applyMessage(e, offset, locations.message, limits)
	case "patch", "system", "redact":
		return st.applyOverlay(e, locations, limits)
	case "fork":
		if e.NewHead == "" {
			return false, nil
		}
		if _, exists := st.heads[e.NewHead]; !exists {
			parent := st.head(e.Head)
			st.heads[e.NewHead] = &sessionDAGHead{leaf: e.From, system: parent.system, lastActivity: e.At, lastOffset: offset}
			st.headOrder = append(st.headOrder, e.NewHead)
		}
	case "rewind":
		h := st.head(e.Head)
		h.leaf, h.lastActivity, h.lastOffset = e.To, e.At, offset
	case "select":
		st.selected = e.Head
	case "retire":
		st.head(e.Head).retired = true
	case "log", "writer", "rename", "turn_begin", "turn_end", "compaction", "checkpoint":
	default:
		return false, fmt.Errorf("decode session event log %s: unsupported entry type %q", st.path, e.Type)
	}
	return true, nil
}

func (st *sessionDAGState) applyMessage(e sessionDAGEntry, offset int64, ref sessionDAGPayload, limits sessionReplayLimits) (bool, error) {
	if e.ID == "" {
		return false, nil
	}
	payload, ok, err := st.payload(e.Msgs, ref, limits)
	if err != nil || !ok {
		return ok, err
	}
	if _, dup := st.nodes[e.ID]; dup {
		return true, nil
	}
	st.nodes[e.ID] = &sessionDAGNode{parent: e.Parent, at: e.At, payload: payload}
	h := st.head(e.Head)
	h.leaf, h.lastActivity, h.lastOffset = e.ID, e.At, offset
	return true, nil
}

func (st *sessionDAGState) applyOverlay(e sessionDAGEntry, locations sessionDAGLocations, limits sessionReplayLimits) (bool, error) {
	switch e.Type {
	case "patch":
		if _, known := st.nodes[e.Target]; !known {
			return true, nil
		}
		payload, ok, err := st.payload(e.Msgs, locations.message, limits)
		if err != nil || !ok {
			return ok, err
		}
		st.patches[e.Target] = payload
	case "system":
		payload, ok, err := st.payload(e.Msgs, locations.message, limits)
		if err != nil || !ok {
			return ok, err
		}
		st.head(e.Head).system = &payload
	case "redact":
		for id, raw := range e.Targets {
			payload, ok, err := st.payload(raw, locations.targets[id], limits)
			if err != nil || !ok {
				return ok, err
			}
			st.redactions[id] = payload
		}
	}
	return true, nil
}

// decodeOne validates the historical message before its file reference is kept.
func (st *sessionDAGState) decodeOne(raw json.RawMessage, limits sessionReplayLimits) (provider.Message, bool, error) {
	msgs, items, err := decodeSessionEventMessages(st.path, raw, len(st.nodes), st.collectionItems, limits)
	if err != nil {
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

// head resolves a head id, creating one the log never declared rather than
// dropping the entries that name it; an empty id is the main head.
func (st *sessionDAGState) head(id string) *sessionDAGHead {
	if id == "" {
		id = sessionDAGMainHead
	}
	h := st.heads[id]
	if h == nil {
		h = &sessionDAGHead{}
		st.heads[id] = h
		st.headOrder = append(st.headOrder, id)
	}
	return h
}

// selectedHead is the head 1.x opens: the last explicit select while that
// head is alive, otherwise the most recently active live head, the larger
// log offset breaking ties, so both lines land on the same conversation.
func (st *sessionDAGState) selectedHead() string {
	if h := st.heads[st.selected]; h != nil && !h.retired {
		return st.selected
	}
	for _, includeRetired := range []bool{false, true} {
		best := ""
		for _, id := range st.headOrder {
			h := st.heads[id]
			if h.retired && !includeRetired {
				continue
			}
			if b := st.heads[best]; best == "" || h.lastActivity.After(b.lastActivity) ||
				(h.lastActivity.Equal(b.lastActivity) && h.lastOffset > b.lastOffset) {
				best = id
			}
		}
		if best != "" {
			return best
		}
	}
	return sessionDAGMainHead
}

// materialize is the transcript of one head: its parent chain from the root,
// patches and redactions applied, and the head's system message first. A
// chain that reaches a parent the log no longer holds starts there.
func (st *sessionDAGState) materialize(f io.ReaderAt, headID string, limits sessionReplayLimits) ([]provider.Message, error) {
	refs, _, err := st.chain(headID, limits)
	if err != nil {
		return nil, err
	}
	return readDAGPayloads(f, st.path, refs, limits)
}

// chain is the payloads of one head in order, the head's system message first.
// With a tail budget it keeps the newest that fit and counts the rest as dropped.
func (st *sessionDAGState) chain(headID string, limits sessionReplayLimits) (refs []sessionDAGPayload, dropped int, err error) {
	h := st.heads[headID]
	if h == nil {
		return nil, 0, nil
	}
	var ids []string
	seen := map[string]bool{}
	for id := h.leaf; id != "" && !seen[id]; {
		n := st.nodes[id]
		if n == nil {
			break
		}
		seen[id] = true
		ids = append(ids, id)
		id = n.parent
	}
	slices.Reverse(ids)
	refs = make([]sessionDAGPayload, 0, len(ids)+1)
	for _, id := range ids {
		m := st.nodes[id].payload
		if p, ok := st.patches[id]; ok {
			m = p
		}
		if r, ok := st.redactions[id]; ok {
			m = r
		}
		refs = append(refs, m)
	}
	if h.system != nil {
		if len(refs) > 0 && refs[0].role == provider.RoleSystem {
			refs[0] = *h.system
		} else {
			refs = append([]sessionDAGPayload{*h.system}, refs...)
		}
	}
	if limits.tailBudget > 0 {
		refs, dropped = tailOfDAGPayloads(refs, limits.tailBudget)
	}
	var payloadBytes int64
	for _, ref := range refs {
		payloadBytes += ref.end - ref.start
		if err := limits.payloadErr(st.path, payloadBytes); err != nil {
			return nil, 0, err
		}
	}
	return refs, dropped, nil
}

func readDAGPayloads(f io.ReaderAt, path string, refs []sessionDAGPayload, limits sessionReplayLimits) ([]provider.Message, error) {
	msgs := make([]provider.Message, 0, len(refs))
	for _, ref := range refs {
		m, err := ref.read(f, path, limits)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// tailOfDAGPayloads keeps the system prompt and the newest payloads within
// budget, starting the rest at a user message when one follows the cut.
func tailOfDAGPayloads(refs []sessionDAGPayload, budget int64) ([]sessionDAGPayload, int) {
	start := 0
	if len(refs) > 0 && refs[0].role == provider.RoleSystem {
		start = 1
	}
	var total int64
	for _, r := range refs {
		total += r.end - r.start
	}
	cut := start
	for cut < len(refs)-1 && total > budget {
		total -= refs[cut].end - refs[cut].start
		cut++
	}
	if cut == start {
		return refs, 0
	}
	for next := cut; next < len(refs); next++ {
		if refs[next].role == provider.RoleUser {
			cut = next
			break
		}
	}
	return slices.Delete(refs, start, cut), cut - start
}

func loadSessionDAGTail(sessionPath string, limits sessionReplayLimits) (SessionTail, error) {
	path := store.SessionEventLog(sessionPath)
	f, err := os.Open(path)
	if err != nil {
		return SessionTail{}, err
	}
	defer f.Close()
	st, err := replaySessionDAG(f, path, limits)
	if err != nil {
		return SessionTail{}, err
	}
	refs, dropped, err := st.chain(st.selectedHead(), limits)
	if err != nil {
		return SessionTail{}, err
	}
	msgs, err := readDAGPayloads(f, path, refs, limits)
	return SessionTail{Messages: msgs, Dropped: dropped}, err
}

// loadSessionDAGMessages is the transcript a schema 2 log's selected head
// holds.
func loadSessionDAGMessages(sessionPath string, limits sessionReplayLimits) ([]provider.Message, bool, error) {
	path := store.SessionEventLog(sessionPath)
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	st, err := replaySessionDAG(f, path, limits)
	if err != nil {
		return nil, false, err
	}
	msgs, err := st.materialize(f, st.selectedHead(), limits)
	return msgs, st.damaged, err
}

// dagTranscriptMatches reports whether a snapshot holds what the 1.x log
// already does, read the way LoadSession reads it. The leading system prompt is
// the host's contract rather than conversation, and a rebuild swaps it for the
// current build's, so it takes no part: opening and leaving a 1.x conversation
// saves nothing, so it must not move to a new session.
func dagTranscriptMatches(sessionPath string, snapshot []provider.Message) bool {
	msgs, _, err := loadSessionDAGMessages(sessionPath, defaultSessionReplayLimits)
	if err != nil {
		return false
	}
	onDisk, err := DigestSessionMessages(withoutLeadingSystem(migrateLegacyProviderContent(NormalizeSession(msgs))))
	if err != nil {
		return false
	}
	held, err := DigestSessionMessages(withoutLeadingSystem(snapshot))
	return err == nil && onDisk == held
}

func withoutLeadingSystem(msgs []provider.Message) []provider.Message {
	if len(msgs) > 0 && msgs[0].Role == provider.RoleSystem {
		return msgs[1:]
	}
	return msgs
}

// IsForeignSessionLog reports whether the session at sessionPath is kept in a
// 1.x event log, which this build reads and does not write.
func IsForeignSessionLog(sessionPath string) bool {
	probe, err := probeSessionEventLog(sessionPath)
	return err == nil && probe.dag
}

// probeRefusesSave is why a save cannot write the log the probe found: a
// schema newer than this build, or 1.x's log, which either already holds the
// transcript or cannot take the rest of it.
func probeRefusesSave(path string, probe sessionEventLogProbe, msgs []provider.Message) error {
	switch {
	case probe.futureSchema:
		return fmt.Errorf("session event log for %s uses schema %d; this build supports up to %d", path, probe.schemaVersion, sessionEventImageBlobsSchemaVersion)
	case probe.dag && dagTranscriptMatches(path, msgs):
		return fmt.Errorf("%w: %s", ErrSessionLogUnchanged, path)
	case probe.dag:
		return fmt.Errorf("%w: %s", ErrSessionLogForeign, path)
	}
	return nil
}

// loadUnownedEventLog reads a log this build does not write: 1.x's, or one
// from a schema newer than it can read at all.
func loadUnownedEventLog(sessionPath string, probe sessionEventLogProbe, limits sessionReplayLimits) ([]provider.Message, bool, error) {
	if probe.futureSchema {
		return nil, false, fmt.Errorf("session event log for %s uses schema %d; this build supports up to %d", sessionPath, probe.schemaVersion, sessionEventImageBlobsSchemaVersion)
	}
	return loadSessionDAGMessages(sessionPath, limits)
}
