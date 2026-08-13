package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// This file implements the windowed history paging API (Phase B1 of the
// history-pipeline refactor). HistoryPageForTab copies and converts the whole
// transcript on every request; HistorySliceForTab pages toward older history
// using the per-session display index sidecar (internal/agent
// SessionDisplayIndex) so only the returned window is read from disk and
// converted. The legacy API stays untouched for one compatibility cycle.
//
// Entry ID scheme: s<sessionFileID>:r<rewriteEpoch>:m<messageIndex>:o<subOrder>.
//   - sessionFileID is the transcript basename minus .jsonl — stable for the
//     life of the session file.
//   - rewriteEpoch is the persisted rewrite version (live) or the index
//     revision (cold, 0 when unknown). Append-only saves keep it, so entry IDs
//     of an unchanged prefix survive appends; rewrites (compaction, rewind)
//     bump it, and cursors go stale on rewrites anyway.
//   - messageIndex is the absolute provider-message index; subOrder is the
//     row number within that message's conversion (one message maps to 0..n
//     history rows: notices, planner turns, …).
//
// Cursor format: base64url(JSON{v, revision, revKnown, digest, before}).
// The cursor binds the page to the session's persisted revision + content
// digest; any save bumps the revision, so continuing with a pre-save cursor
// returns HistorySlice{Stale: true} and the frontend reloads the latest page.
// "before" is the absolute provider-message index the next page ends at
// (exclusive), which carries the intra-turn position for oversized turns:
// pages always cut at message boundaries, so concatenating pages reproduces
// the full conversion exactly — no duplication, no omission.
//
// Visible-turn mapping: the display index's AuthoredTurn is counted with
// IsUserAuthoredTurn semantics, which already excludes synthetic and steer
// user messages — exactly the desktop visible-turn rule — so an entry's
// visible turn is its AuthoredTurn (1-based; 0 = before the first turn). The
// unsaved in-memory tail of a live session is classified with the real
// resolver-based rule (isVisibleHistoryUser), matching today's behavior.

const (
	defaultHistorySliceTurns   = 12
	defaultHistorySliceEntries = 120
	defaultHistorySliceBytes   = 512 << 10
	maxHistorySliceTurns       = 500
	maxHistorySliceEntries     = 1000
	maxHistorySliceBytes       = 8 << 20

	// historyInlineRefThreshold is the field size above which a string field
	// is replaced inline by a preview + HistoryContentRef.
	historyInlineRefThreshold = 64 << 10
	// historyFieldPreviewBytes is the rune-safe inline preview kept for a
	// ref-replaced field. The full value stays retrievable via
	// HistoryContentForTab.
	historyFieldPreviewBytes = 4 << 10
	// historyContentChunkBytes is the HistoryContentForTab chunk size. Chunks
	// split on UTF-8 rune boundaries, never mid-rune.
	historyContentChunkBytes = 256 << 10

	// historySliceColdWindowBytes caps the raw transcript span one cold-path
	// page reads from disk. Inline output is still bounded by the byte budget;
	// this cap only keeps windows dense with multi-megabyte image lines from
	// reading unbounded file spans.
	historySliceColdWindowBytes = 32 << 20
	// historyLookupChunkMessages bounds the number of decoded messages retained
	// while deriving cross-page planner/todo state.
	historyLookupChunkMessages = 128
	historyDerivedCacheEntries = 4
)

// HistorySliceRequest is one page request. Cursor empty = latest page.
type HistorySliceRequest struct {
	Cursor  string `json:"cursor"`
	Turns   int    `json:"turns"`   // default 12
	Entries int    `json:"entries"` // default 120
	Bytes   int    `json:"bytes"`   // inline byte budget, default 512KiB
}

// HistoryContentRef marks a string field that exceeded the inline threshold.
// The field carries a rune-safe preview prefix; the full value is retrievable
// in chunks via HistoryContentForTab.
type HistoryContentRef struct {
	EntryID string `json:"entryId"`
	Field   string `json:"field"` // "content", "reasoning", "submitText", "detail", "code", "summary", "archive", "toolResultError", "toolArguments", "toolSubject", "toolSummary", "toolDiff"
	Size    int    `json:"size"`
	Chunks  int    `json:"chunks"`
	// ToolCallID identifies the tool call for tool* fields.
	ToolCallID string `json:"toolCallId,omitempty"`
	// Revision/RevKnown/Digest bind the ref to the session state it was cut
	// from; a mismatch on fetch resolves to Stale.
	Revision int64  `json:"revision"`
	RevKnown bool   `json:"revKnown,omitempty"`
	Digest   string `json:"digest"`
}

// HistoryEntry is one display row in a history page.
type HistoryEntry struct {
	EntryID string `json:"entryId"`
	// Turn is the absolute visible turn the row belongs to (1-based; 0 =
	// before the first visible turn).
	Turn int `json:"turn"`
	// Order is the absolute provider-message index the row was converted
	// from; combined with the sub-order in EntryID it is strictly increasing
	// in display order.
	Order   int            `json:"order"`
	Message HistoryMessage `json:"message"`
	// Refs lists the message fields replaced by previews. Always initialized
	// so JSON encodes [] rather than null.
	Refs []HistoryContentRef `json:"refs"`
}

// HistorySlice is one page of history toward older messages.
type HistorySlice struct {
	Entries    []HistoryEntry `json:"entries"`
	NextCursor string         `json:"nextCursor"` // toward older; empty when none
	HasOlder   bool           `json:"hasOlder"`
	TotalTurns int            `json:"totalTurns"`
	StartTurn  int            `json:"startTurn"` // oldest visible turn in the page (0 when none)
	EndTurn    int            `json:"endTurn"`   // newest visible turn in the page (0 when none)
	Stale      bool           `json:"stale"`     // cursor bound to an older session revision
	Revision   int64          `json:"revision"`  // session revision the page was cut from (0 when unknown)
	// RevisionKnown and Digest expose the complete canonical identity already
	// carried by cursors. They let same-path resident frontend projections be
	// invalidated after another process advances or rewrites the session.
	RevisionKnown bool   `json:"revisionKnown,omitempty"`
	Digest        string `json:"digest,omitempty"`
	// Source: index|scan|live-index|live-fallback. Error marks a failed read
	// (empty Entries alone means a genuinely empty session).
	Source string `json:"source,omitempty"`
	Error  string `json:"error,omitempty"`
}

// HistoryContentChunk is one chunk of a ref-replaced field's full value.
type HistoryContentChunk struct {
	EntryID string `json:"entryId"`
	Field   string `json:"field"`
	Chunk   int    `json:"chunk"`
	Chunks  int    `json:"chunks"`
	Data    string `json:"data"`
	Done    bool   `json:"done"`
	Stale   bool   `json:"stale"`
}

// MarshalJSON enforces the Wails contract even for zero values: entries is
// always [], never null.
func (s HistorySlice) MarshalJSON() ([]byte, error) {
	type alias HistorySlice
	if s.Entries == nil {
		s.Entries = []HistoryEntry{}
	}
	return json.Marshal(alias(s))
}

// MarshalJSON keeps refs [] on zero values, matching the entries contract.
func (e HistoryEntry) MarshalJSON() ([]byte, error) {
	type alias HistoryEntry
	if e.Refs == nil {
		e.Refs = []HistoryContentRef{}
	}
	return json.Marshal(alias(e))
}

func emptyHistorySlice() HistorySlice { return HistorySlice{Entries: []HistoryEntry{}} }

func failedHistorySlice(message string) HistorySlice {
	return HistorySlice{Entries: []HistoryEntry{}, Error: strings.TrimSpace(message)}
}

func staleHistorySlice(revision int64, revisionKnown bool, digest string) HistorySlice {
	return HistorySlice{
		Entries:       []HistoryEntry{},
		Stale:         true,
		Revision:      revision,
		RevisionKnown: revisionKnown,
		Digest:        digest,
	}
}

func normalizeHistorySliceRequest(req HistorySliceRequest) HistorySliceRequest {
	if req.Turns <= 0 {
		req.Turns = defaultHistorySliceTurns
	}
	if req.Turns > maxHistorySliceTurns {
		req.Turns = maxHistorySliceTurns
	}
	if req.Entries <= 0 {
		req.Entries = defaultHistorySliceEntries
	}
	if req.Entries > maxHistorySliceEntries {
		req.Entries = maxHistorySliceEntries
	}
	if req.Bytes <= 0 {
		req.Bytes = defaultHistorySliceBytes
	}
	if req.Bytes > maxHistorySliceBytes {
		req.Bytes = maxHistorySliceBytes
	}
	return req
}

// historySliceCursor is the opaque page position toward older history.
type historySliceCursor struct {
	V        int    `json:"v"`
	Revision int64  `json:"revision"`
	RevKnown bool   `json:"revKnown"`
	Digest   string `json:"digest"`
	Before   int    `json:"before"` // next page covers messages/rows with index < Before
}

func encodeHistorySliceCursor(c historySliceCursor) string {
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeHistorySliceCursor(s string) (historySliceCursor, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return historySliceCursor{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return historySliceCursor{}, err
	}
	var c historySliceCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return historySliceCursor{}, err
	}
	if c.V != 1 || c.Before < 0 {
		return historySliceCursor{}, fmt.Errorf("unsupported history cursor")
	}
	return c, nil
}

// historySliceSource is the windowed read view over one session used to cut a
// page: per-message visible turns and roles plus bounded message fetches.
type historySliceSource struct {
	sessionID  string // transcript basename minus .jsonl
	total      int    // total provider messages
	turns      []int  // turns[i] = visible turn of message i (1-based; 0 = before first turn)
	roles      []provider.Role
	totalTurns int
	revision   int64
	revKnown   bool
	digest     string
	epoch      int
	// cacheKey is non-empty only when revision+digest describe the complete
	// source (no unsaved live tail). Derived cross-page state may then be reused
	// without risking a stale completion against newly appended messages.
	cacheKey string
	// fetch returns messages [lo, hi). Implementations must copy or freshly
	// decode; callers never mutate but may retain across budget checks. Decode
	// errors are propagated all the way to the cold read instead of being
	// mistaken for an empty window and indexing past its end.
	fetch func(lo, hi int) ([]provider.Message, error)
	// windowBytes estimates the raw transcript span of [lo, hi); 0 means
	// unbounded-but-cheap (in-memory). Used to cap cold-path reads.
	windowBytes func(lo, hi int) int64
}

type historyDerivedCacheEntry struct {
	ready    chan struct{}
	todoArgs map[string]string
	err      error
}

// historyDerivedCache prevents every older-page request from replaying a huge
// transcript twice to derive the same todo state. Entries are identity-bound,
// single-flight, and deliberately few; transcript bodies are never retained.
type historyDerivedCache struct {
	mu      sync.Mutex
	entries map[string]*historyDerivedCacheEntry
	order   []string
}

func (c *historyDerivedCache) todoArgs(key string, compute func() (map[string]string, error)) (map[string]string, error) {
	if key == "" {
		return compute()
	}
	c.mu.Lock()
	if entry := c.entries[key]; entry != nil {
		c.touchLocked(key)
		ready := entry.ready
		c.mu.Unlock()
		<-ready
		return entry.todoArgs, entry.err
	}
	if c.entries == nil {
		c.entries = map[string]*historyDerivedCacheEntry{}
	}
	entry := &historyDerivedCacheEntry{ready: make(chan struct{})}
	c.entries[key] = entry
	c.order = append(c.order, key)
	c.pruneLocked()
	c.mu.Unlock()

	entry.todoArgs, entry.err = compute()
	close(entry.ready)
	c.mu.Lock()
	if entry.err != nil && c.entries[key] == entry {
		// Do not retain transient I/O or decode failures. A later page request
		// should be able to retry after the underlying read model is repaired.
		delete(c.entries, key)
		for i, candidate := range c.order {
			if candidate == key {
				c.order = append(c.order[:i], c.order[i+1:]...)
				break
			}
		}
	}
	c.pruneLocked()
	c.mu.Unlock()
	return entry.todoArgs, entry.err
}

func (c *historyDerivedCache) touchLocked(key string) {
	for i, candidate := range c.order {
		if candidate == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, key)
}

func (c *historyDerivedCache) pruneLocked() {
	for len(c.entries) > historyDerivedCacheEntries {
		removed := false
		for i, key := range c.order {
			entry := c.entries[key]
			if entry == nil {
				c.order = append(c.order[:i], c.order[i+1:]...)
				removed = true
				break
			}
			select {
			case <-entry.ready:
				delete(c.entries, key)
				c.order = append(c.order[:i], c.order[i+1:]...)
				removed = true
			default:
			}
			if removed {
				break
			}
		}
		if !removed {
			return
		}
	}
}

// identityMatches reports whether the cursor/ref identity describes the same
// session state as the source. Revision is normalized to 0 when unknown on
// both sides, so the comparison is exact.
func (src *historySliceSource) identityMatches(revision int64, revKnown bool, digest string) bool {
	if !revKnown {
		revision = 0
	}
	return src.revKnown == revKnown && src.revision == revision && src.digest == digest
}

// historyWindowController is the slice of *control.Controller the windowed
// live path needs. Kept as an interface assertion (like
// sessionTempFromController) so test fakes implementing control.SessionAPI
// keep working via the full-snapshot fallback.
type historyWindowController interface {
	HistoryLen() int
	HistoryWindow(start, end int) []provider.Message
	SessionPersistedState() (agent.PersistedState, bool)
}

// HistorySliceForTab returns one page of the tab's history toward older
// messages, converting only the returned window.
func (a *App) HistorySliceForTab(tabID string, req HistorySliceRequest) HistorySlice {
	req = normalizeHistorySliceRequest(req)
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	var ctrl control.SessionAPI
	var sessionDir, sessionPath string
	if tab != nil {
		ctrl = tab.Ctrl
		sessionDir = tabSessionDir(tab)
		sessionPath = tab.currentSessionPath()
	}
	a.mu.RUnlock()

	if ctrl == nil {
		if strings.TrimSpace(sessionPath) == "" {
			return failedHistorySlice("session path unavailable before controller ready")
		}
		slice, err := a.coldHistorySlice(sessionDir, sessionPath, req)
		if err != nil {
			slog.Debug("desktop: cold history slice failed", "path", sessionPath, "err", err)
			return failedHistorySlice(err.Error())
		}
		return slice
	}
	if p := ctrl.SessionPath(); strings.TrimSpace(p) != "" {
		sessionPath = p
		sessionDir = controllerSessionDir(ctrl)
	}
	return a.liveHistorySlice(ctrl, sessionDir, sessionPath, req)
}

// liveHistorySlice pages a tab with a running controller. The display index
// supplies turn boundaries when it validates against the session's persisted
// state; otherwise a single in-memory snapshot walk classifies turns (still
// converting only the window) and a background rebuild is kicked.
func (a *App) liveHistorySlice(ctrl control.SessionAPI, sessionDir, sessionPath string, req HistorySliceRequest) HistorySlice {
	resolver := sessionDisplayResolver(sessionDir, sessionPath)
	src, indexUsed := a.liveHistorySliceSource(ctrl, sessionPath, resolver)
	if src == nil {
		return emptyHistorySlice()
	}
	if !indexUsed {
		a.kickHistoryIndexRebuild(sessionPath)
	}
	slice, err := a.pageHistorySliceSource(src, req, resolver, sessionPlannerDisplayTurns(sessionDir, sessionPath), ctrl.CheckpointTurnsByMessageIndex(), sessionPath)
	if err != nil {
		slog.Debug("desktop: live history slice failed", "path", sessionPath, "err", err)
		return failedHistorySlice(err.Error())
	}
	if indexUsed {
		slice.Source = "live-index"
	} else {
		slice.Source = "live-fallback"
	}
	return slice
}

func (a *App) liveHistorySliceSource(ctrl control.SessionAPI, sessionPath string, resolver func(string) string) (*historySliceSource, bool) {
	sessionID := strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl")
	wc, ok := ctrl.(historyWindowController)
	if !ok {
		// Compat for fakes: full snapshot, windowed conversion.
		msgs := ctrl.History()
		src := newInMemoryHistorySliceSource(sessionID, msgs, resolver, agent.PersistedState{}, false)
		return src, false
	}
	n := wc.HistoryLen()
	ps, psOK := wc.SessionPersistedState()
	if psOK && ps.AppendOnlyTail && n > 0 {
		if idx, err := agent.LoadSessionDisplayIndex(store.SessionDisplayIndex(sessionPath)); err == nil &&
			idx.RevisionKnown == ps.RevisionKnown &&
			(!ps.RevisionKnown || idx.Revision == ps.Revision) &&
			idx.ContentDigest == ps.DigestHex &&
			idx.MessageCount <= n {
			turns := make([]int, n)
			roles := make([]provider.Role, n)
			for i, e := range idx.Entries {
				turns[i] = e.AuthoredTurn
				roles[i] = e.Role
			}
			turn := idx.AuthoredTurns
			if idx.MessageCount < n {
				tail := wc.HistoryWindow(idx.MessageCount, n)
				for j, m := range tail {
					if isVisibleHistoryUser(m, resolver) {
						turn++
					}
					turns[idx.MessageCount+j] = turn
					roles[idx.MessageCount+j] = m.Role
				}
			}
			src := &historySliceSource{
				sessionID:  sessionID,
				total:      n,
				turns:      turns,
				roles:      roles,
				totalTurns: turn,
				revision:   ps.Revision,
				revKnown:   ps.RevisionKnown,
				digest:     ps.DigestHex,
				epoch:      ps.RewriteEpoch,
				fetch: func(lo, hi int) ([]provider.Message, error) {
					return wc.HistoryWindow(lo, hi), nil
				},
			}
			if ps.UnchangedSincePersisted && idx.MessageCount == n {
				src.cacheKey = historyDerivedSourceKey(sessionPath, src)
			}
			return src, true
		}
	}
	// Fallback: one full snapshot for classification; conversion stays
	// windowed. The background rebuild republishes the index when the
	// in-memory log is exactly the persisted transcript.
	msgs := ctrl.History()
	var state agent.PersistedState
	if psOK {
		state = ps
	}
	src := newInMemoryHistorySliceSource(sessionID, msgs, resolver, state, psOK)
	if psOK && ps.UnchangedSincePersisted {
		src.cacheKey = historyDerivedSourceKey(sessionPath, src)
	}
	return src, false
}

// newInMemoryHistorySliceSource builds a source by classifying a full
// in-memory snapshot with the resolver-based visible-turn rule — the same
// semantics the legacy history path uses.
func newInMemoryHistorySliceSource(sessionID string, msgs []provider.Message, resolver func(string) string, ps agent.PersistedState, psOK bool) *historySliceSource {
	turns := make([]int, len(msgs))
	roles := make([]provider.Role, len(msgs))
	turn := 0
	for i, m := range msgs {
		if isVisibleHistoryUser(m, resolver) {
			turn++
		}
		turns[i] = turn
		roles[i] = m.Role
	}
	src := &historySliceSource{
		sessionID:  sessionID,
		total:      len(msgs),
		turns:      turns,
		roles:      roles,
		totalTurns: turn,
		fetch: func(lo, hi int) ([]provider.Message, error) {
			if lo < 0 {
				lo = 0
			}
			if hi > len(msgs) {
				hi = len(msgs)
			}
			if lo >= hi {
				return []provider.Message{}, nil
			}
			return msgs[lo:hi], nil
		},
	}
	if psOK {
		src.revision = ps.Revision
		src.revKnown = ps.RevisionKnown
		src.digest = ps.DigestHex
		src.epoch = ps.RewriteEpoch
	}
	return src
}

func historyDerivedSourceKey(sessionPath string, src *historySliceSource) string {
	if src == nil || strings.TrimSpace(sessionPath) == "" || strings.TrimSpace(src.digest) == "" {
		return ""
	}
	return fmt.Sprintf("%s|%t|%d|%s|%d", agent.CanonicalSessionPath(sessionPath), src.revKnown, src.revision, src.digest, src.total)
}

// coldHistorySlice pages a session file with no running controller. It never
// loads the whole session: a valid on-disk display index + byte-offset reads
// serve the window; a missing/stale/corrupt index is rebuilt by streaming
// scan (constant memory) and the first page is served from the scan result.
func (a *App) coldHistorySlice(sessionDir, path string, req HistorySliceRequest) (HistorySlice, error) {
	sessionPath, _, err := validateSessionPath(sessionDir, path)
	if err != nil {
		return emptyHistorySlice(), err
	}
	info, err := os.Stat(sessionPath)
	if err != nil {
		return emptyHistorySlice(), err
	}
	if info.IsDir() {
		return emptyHistorySlice(), fmt.Errorf("not a session file: %s", sessionPath)
	}
	if historySessionLooksEventFormat(sessionPath) {
		// Legacy event-record format: stream-decode (constant memory) and page
		// the decoded rows. Only ancient sessions take this path.
		slice, err := coldEventHistorySlice(sessionPath, info, req)
		slice.Source = "scan"
		return slice, err
	}
	resolver := sessionDisplayResolver(sessionDir, sessionPath)
	indexPath := store.SessionDisplayIndex(sessionPath)
	idx, err := agent.LoadSessionDisplayIndex(indexPath)
	identity, identityKnown, identityErr := agent.SessionContentIdentity(sessionPath)
	if identityErr != nil {
		return emptyHistorySlice(), identityErr
	}
	indexIdentityValid := false
	if idx != nil && err == nil {
		if identityKnown {
			indexIdentityValid = agent.ValidateSessionDisplayIndex(idx, identity.Revision, identity.RevisionKnown, identity.Digest, info.Size())
		} else {
			// Legacy sessions have no ledger digest. Atomic index publication
			// after the transcript plus exact structural/size validation is their
			// generation stamp; a later rewrite advances the transcript mtime.
			indexIdentityValid = !idx.RevisionKnown
		}
	}
	if idx != nil && err == nil && idx.TranscriptSize == info.Size() && indexIdentityValid && historyIndexTimestampValid(indexPath, sessionPath, info, idx, true) {
		slice, pageErr := a.pageHistorySliceSource(coldHistorySliceSource(sessionPath, idx), req, resolver, sessionPlannerDisplayTurns(sessionDir, sessionPath), nil, sessionPath)
		if pageErr != nil {
			return emptyHistorySlice(), pageErr
		}
		slice.Source = "index"
		return slice, nil
	}

	// A missing/corrupt sidecar is cheap to repair when the checkpoint itself is
	// still the authoritative transcript. Scan once and compare its digest to
	// the ledger before falling back to a full event-log replay. This preserves
	// the bounded cold path for ordinary legacy/index-migration reads while
	// still rejecting same-size anchor rewrites.
	scanned, scanErr := agent.ScanSessionDisplayIndex(sessionPath)
	if scanErr == nil {
		if !identityKnown || scanned.ContentDigest == identity.DigestHex {
			if identityKnown {
				scanned.Revision = identity.Revision
				scanned.RevisionKnown = identity.RevisionKnown
			}
			if writeErr := agent.WriteSessionDisplayIndex(store.SessionDisplayIndex(sessionPath), scanned); writeErr != nil {
				slog.Debug("desktop: history display index republish failed", "path", sessionPath, "err", writeErr)
			}
			slice, pageErr := a.pageHistorySliceSource(coldHistorySliceSource(sessionPath, scanned), req, resolver, sessionPlannerDisplayTurns(sessionDir, sessionPath), nil, sessionPath)
			if pageErr != nil {
				return emptyHistorySlice(), pageErr
			}
			slice.Source = "scan"
			return slice, nil
		}
	}

	// The event log is authoritative. During append-only saves its transcript
	// is newer than the compatibility .jsonl anchor, so scanning the anchor
	// would silently omit the tail even when a display index covers it.
	if eventInfo, statErr := os.Stat(store.SessionEventLog(sessionPath)); statErr == nil && !eventInfo.IsDir() && eventInfo.Size() > 0 {
		messages, state, repairable, loadErr := agent.LoadSessionDisplayMessages(sessionPath)
		if loadErr != nil {
			return emptyHistorySlice(), loadErr
		}
		src := newInMemoryHistorySliceSource(strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl"), messages, resolver, state, true)
		src.cacheKey = historyDerivedSourceKey(sessionPath, src)
		slice, pageErr := a.pageHistorySliceSource(src, req, resolver, sessionPlannerDisplayTurns(sessionDir, sessionPath), nil, sessionPath)
		if pageErr != nil {
			return emptyHistorySlice(), pageErr
		}
		slice.Source = "event-log"
		if repairable {
			a.kickHistoryReadModelRepair(sessionPath)
		}
		return slice, nil
	}

	// Legacy checkpoints have no authoritative ledger identity. Scan their
	// bytes to obtain the digest before trusting (or republishing) offsets; this
	// detects same-size external rewrites that a size-only comparison misses.
	if scanErr != nil {
		// The bounded scanner rejects malformed or exceptionally large single
		// records before allocating without limit. A legacy transcript still
		// remains readable through the ordinary authoritative loader, then gets
		// a file-exact index in the background for subsequent opens.
		messages, state, repairable, loadErr := agent.LoadSessionDisplayMessages(sessionPath)
		if loadErr != nil {
			return emptyHistorySlice(), errors.Join(scanErr, loadErr)
		}
		src := newInMemoryHistorySliceSource(strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl"), messages, resolver, state, true)
		src.cacheKey = historyDerivedSourceKey(sessionPath, src)
		slice, pageErr := a.pageHistorySliceSource(src, req, resolver, sessionPlannerDisplayTurns(sessionDir, sessionPath), nil, sessionPath)
		if pageErr != nil {
			return emptyHistorySlice(), pageErr
		}
		slice.Source = "scan"
		if repairable {
			a.kickHistoryReadModelRepair(sessionPath)
		}
		return slice, nil
	}
	if identityKnown {
		if scanned.ContentDigest == identity.DigestHex {
			scanned.Revision = identity.Revision
			scanned.RevisionKnown = identity.RevisionKnown
		}
	}
	if writeErr := agent.WriteSessionDisplayIndex(store.SessionDisplayIndex(sessionPath), scanned); writeErr != nil {
		slog.Debug("desktop: history display index republish failed", "path", sessionPath, "err", writeErr)
	}
	slice, pageErr := a.pageHistorySliceSource(coldHistorySliceSource(sessionPath, scanned), req, resolver, sessionPlannerDisplayTurns(sessionDir, sessionPath), nil, sessionPath)
	if pageErr != nil {
		return emptyHistorySlice(), pageErr
	}
	slice.Source = "scan"
	return slice, nil
}

// historyIndexTimestampValid is the cheap file-generation guard for
// cold offset reads. Save/scan publish the index atomically after the transcript
// is complete. Equal timestamps are ambiguous on coarse filesystems, so cold
// readers verify the streamed digest before trusting offsets. The migration
// probe may accept equality because it never reads indexed content.
func historyIndexTimestampValid(indexPath, sessionPath string, transcriptInfo os.FileInfo, idx *agent.SessionDisplayIndex, verifyEqual bool) bool {
	indexInfo, err := os.Stat(indexPath)
	if err != nil || indexInfo.IsDir() || idx == nil {
		return false
	}
	if indexInfo.ModTime().After(transcriptInfo.ModTime()) {
		return true
	}
	if !indexInfo.ModTime().Equal(transcriptInfo.ModTime()) {
		return false
	}
	if !verifyEqual {
		return true
	}
	scanned, err := agent.ScanSessionDisplayIndex(sessionPath)
	matches := err == nil && scanned.TranscriptSize == idx.TranscriptSize && scanned.MessageCount == idx.MessageCount && scanned.ContentDigest == idx.ContentDigest
	if matches {
		if err := agent.WriteSessionDisplayIndex(indexPath, idx); err != nil {
			slog.Debug("desktop: history display index tie republish failed", "path", sessionPath, "err", err)
		}
	}
	return matches
}

func coldHistorySliceSource(sessionPath string, idx *agent.SessionDisplayIndex) *historySliceSource {
	n := idx.MessageCount
	turns := make([]int, n)
	roles := make([]provider.Role, n)
	for i, e := range idx.Entries {
		turns[i] = e.AuthoredTurn
		roles[i] = e.Role
	}
	revision := idx.Revision
	if !idx.RevisionKnown {
		revision = 0
	}
	epoch := 0
	if idx.RevisionKnown {
		epoch = int(idx.Revision)
	}
	src := &historySliceSource{
		sessionID:  strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl"),
		total:      n,
		turns:      turns,
		roles:      roles,
		totalTurns: idx.AuthoredTurns,
		revision:   revision,
		revKnown:   idx.RevisionKnown,
		digest:     idx.ContentDigest,
		epoch:      epoch,
		fetch: func(lo, hi int) ([]provider.Message, error) {
			if lo < 0 || hi < lo || hi > len(idx.Entries) {
				return nil, fmt.Errorf("history display index window [%d,%d) is out of range", lo, hi)
			}
			return readSessionMessagesAtOffsets(sessionPath, idx.Entries[lo:hi])
		},
		windowBytes: func(lo, hi int) int64 {
			if lo >= hi || hi > len(idx.Entries) {
				return 0
			}
			last := idx.Entries[hi-1]
			return last.Offset + last.Length - idx.Entries[lo].Offset
		},
	}
	src.cacheKey = historyDerivedSourceKey(sessionPath, src)
	return src
}

// readSessionMessagesAtOffsets decodes the message lines for entries, whose
// byte ranges are contiguous in the transcript, with one read.
func readSessionMessagesAtOffsets(sessionPath string, entries []agent.DisplayIndexEntry) ([]provider.Message, error) {
	out := make([]provider.Message, 0, len(entries))
	if len(entries) == 0 {
		return out, nil
	}
	f, err := os.Open(sessionPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	spanStart := entries[0].Offset
	spanEnd := entries[len(entries)-1].Offset + entries[len(entries)-1].Length
	spanLength := spanEnd - spanStart
	if spanLength < 0 {
		return nil, fmt.Errorf("invalid history display index span")
	}
	if spanLength <= historySliceColdWindowBytes {
		buf := make([]byte, int(spanLength))
		if _, err := f.ReadAt(buf, spanStart); err != nil {
			return nil, err
		}
		for _, e := range entries {
			start := e.Offset - spanStart
			end := start + e.Length
			if start < 0 || end < start || end > int64(len(buf)) {
				return nil, fmt.Errorf("history display index line %d escapes fetched span", e.Index)
			}
			var m provider.Message
			if err := json.Unmarshal(buf[int(start):int(end)], &m); err != nil {
				return nil, fmt.Errorf("decode session transcript line %d: %w", e.Index, err)
			}
			out = append(out, m)
		}
		return out, nil
	}
	// A legitimate historical record may exceed the normal 32MiB page span.
	// Decode it directly from a bounded section so the read path does not first
	// allocate and copy a second full record-sized byte slice.
	for _, e := range entries {
		var m provider.Message
		dec := json.NewDecoder(io.NewSectionReader(f, e.Offset, e.Length))
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("decode oversized session transcript line %d: %w", e.Index, err)
		}
		out = append(out, m)
	}
	return out, nil
}

// historySessionLooksEventFormat reports whether the transcript is a legacy
// event-record log rather than a provider-message transcript: event records
// carry kind/type and no role.
func historySessionLooksEventFormat(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(f, 1<<20).ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		// Legacy event headers are tiny. A megabyte first record is a provider
		// message or malformed input, neither of which needs event probing.
		return false
	}
	if len(line) == 0 || err != nil && len(line) == 0 {
		return false
	}
	var probe struct {
		Role provider.Role `json:"role"`
		Kind string        `json:"kind"`
		Type string        `json:"type"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return false
	}
	return probe.Role == "" && (probe.Kind != "" || probe.Type != "")
}

// pageHistorySliceSource cuts one page from src. Pages are suffixes of the
// candidate window: the turn budget picks the oldest message that may be
// included, conversion runs forward (its cross-message state flows forward),
// and the entry/byte budgets drop the oldest whole-message groups — so cuts
// always land on message boundaries.
func (a *App) pageHistorySliceSource(src *historySliceSource, req HistorySliceRequest, resolver func(string) string, plannerTurns []plannerDisplayTurn, checkpointTurns map[int]int, sessionPath string) (HistorySlice, error) {
	cursor, err := decodeHistorySliceCursor(req.Cursor)
	// An undecodable cursor is treated like a request for the latest page.
	hasCursor := req.Cursor != "" && err == nil
	if hasCursor && !src.identityMatches(cursor.Revision, cursor.RevKnown, cursor.Digest) {
		return staleHistorySlice(src.revision, src.revKnown, src.digest), nil
	}
	hi := src.total
	if hasCursor && cursor.Before < hi {
		hi = cursor.Before
	}
	page := HistorySlice{
		Entries:       []HistoryEntry{},
		TotalTurns:    src.totalTurns,
		Revision:      src.revision,
		RevisionKnown: src.revKnown,
		Digest:        src.digest,
	}
	if hi <= 0 || src.total == 0 {
		return page, nil
	}

	// Turn budget: the oldest visible turn this page may reach.
	newestTurn := src.turns[hi-1]
	oldestTurn := 0
	if newestTurn > 0 {
		oldestTurn = max(newestTurn-req.Turns+1, 1)
	}
	// turns is non-decreasing: binary-search the first message in the page.
	candidateLo := sort.Search(hi, func(i int) bool { return src.turns[i] >= oldestTurn })
	if oldestTurn <= 1 {
		// A page reaching the first turn also includes the pre-turn messages
		// (system prompt), mirroring providerMessagesForVisibleTurnRange.
		candidateLo = 0
	}
	// Cold-path raw-span cap: shrink the window forward while the byte span
	// is excessive (image-dense windows).
	if src.windowBytes != nil {
		for candidateLo < hi-1 && src.windowBytes(candidateLo, hi) > historySliceColdWindowBytes {
			candidateLo++
		}
	}

	window, fetchErr := src.fetch(candidateLo, hi)
	if fetchErr != nil {
		return emptyHistorySlice(), fetchErr
	}
	if len(window) != hi-candidateLo {
		return emptyHistorySlice(), fmt.Errorf("history window length %d, want %d", len(window), hi-candidateLo)
	}
	window = historyWindowWithPersistedTimes(window, sessionPath, countRoleBefore(src.roles, candidateLo, provider.RoleUser))
	todoArgs := map[string]string{}
	if historyWindowContainsTodoWrite(window) {
		var todoErr error
		todoArgs, todoErr = a.historyDerived.todoArgs(src.cacheKey, func() (map[string]string, error) {
			return historyTodoArgsForSource(src)
		})
		if todoErr != nil {
			return emptyHistorySlice(), todoErr
		}
	}
	toolResults := historyToolResultsByID(window)
	if err := extendHistoryToolResults(src, window, hi, toolResults); err != nil {
		return emptyHistorySlice(), err
	}

	type entryGroup struct {
		msgIndex int
		entries  []HistoryEntry
		bytes    int
	}
	groups := []entryGroup{}
	entryCount, byteCount := 0, 0
	state := newHistoryMessageConvertState(plannerTurns)
	if err := primeHistoryPlannerState(src, state, candidateLo, resolver); err != nil {
		return emptyHistorySlice(), err
	}
	for i := candidateLo; i < hi; i++ {
		m := window[i-candidateLo]
		rows := state.convertHistoryMessage(i, m, resolver, checkpointTurns, todoArgs, toolResults)
		if len(rows) == 0 {
			continue
		}
		g := entryGroup{msgIndex: i, entries: make([]HistoryEntry, 0, len(rows))}
		for sub, row := range rows {
			entry := newHistoryEntry(src, fmt.Sprintf("s%s:r%d:m%d:o%d", src.sessionID, src.epoch, i, sub), i, sub, row)
			g.bytes += entry.inlineBytes()
			g.entries = append(g.entries, entry)
		}
		groups = append(groups, g)
		entryCount += len(g.entries)
		byteCount += g.bytes
		// Keep the newest suffix within budget; always keep the newest group
		// so a single oversized message still makes progress.
		for len(groups) > 1 && (entryCount > req.Entries || byteCount > req.Bytes) {
			entryCount -= len(groups[0].entries)
			byteCount -= groups[0].bytes
			groups = groups[1:]
		}
	}

	pageStart := candidateLo
	if len(groups) > 0 {
		pageStart = groups[0].msgIndex
	}
	for _, g := range groups {
		page.Entries = append(page.Entries, g.entries...)
	}
	for _, e := range page.Entries {
		if e.Turn <= 0 {
			continue
		}
		if page.StartTurn == 0 || e.Turn < page.StartTurn {
			page.StartTurn = e.Turn
		}
		if e.Turn > page.EndTurn {
			page.EndTurn = e.Turn
		}
	}
	page.HasOlder = pageStart > 0
	if page.HasOlder {
		page.NextCursor = encodeHistorySliceCursor(historySliceCursor{
			V:        1,
			Revision: src.revision,
			RevKnown: src.revKnown,
			Digest:   src.digest,
			Before:   pageStart,
		})
	}
	return page, nil
}

func historyWindowContainsTodoWrite(msgs []provider.Message) bool {
	for _, msg := range msgs {
		for _, call := range msg.ToolCalls {
			if call.Name == "todo_write" {
				return true
			}
		}
	}
	return false
}

// countRoleBefore counts messages with role in [0, lo).
func countRoleBefore(roles []provider.Role, lo int, role provider.Role) int {
	if lo > len(roles) {
		lo = len(roles)
	}
	count := 0
	for i := range lo {
		if roles[i] == role {
			count++
		}
	}
	return count
}

// extendHistoryToolResults fills in tool results for window tool calls whose
// result message lies past the window's newer edge (an intra-turn page cut),
// so tool-call summaries match the full-conversion output. It keeps no result
// body except one whose call is actually visible, but deliberately scans past
// arbitrary non-tool traffic: correctness cannot depend on a result arriving
// within a guessed distance.
func extendHistoryToolResults(src *historySliceSource, window []provider.Message, hi int, toolResults map[string]provider.Message) error {
	var want map[string]bool
	for _, m := range window {
		for _, tc := range m.ToolCalls {
			if tc.ID == "" {
				continue
			}
			if _, ok := toolResults[tc.ID]; ok {
				continue
			}
			if want == nil {
				want = map[string]bool{}
			}
			want[tc.ID] = true
		}
	}
	if len(want) == 0 {
		return nil
	}
	for i := hi; i < src.total && len(want) > 0; i++ {
		if src.roles[i] != provider.RoleTool {
			continue
		}
		msgs, err := src.fetch(i, i+1)
		if err != nil {
			return err
		}
		if len(msgs) != 1 {
			return fmt.Errorf("tool result window length %d, want 1", len(msgs))
		}
		m := msgs[0]
		if m.ToolCallID != "" && want[m.ToolCallID] {
			toolResults[m.ToolCallID] = m
			delete(want, m.ToolCallID)
		}
	}
	return nil
}

// forEachHistorySourceChunk decodes a bounded contiguous message window at a
// time. It is the common primitive for the cross-page lookups below; callers
// retain only their derived state, never the full transcript.
func forEachHistorySourceChunk(src *historySliceSource, end int, visit func([]provider.Message) error) error {
	if end > src.total {
		end = src.total
	}
	for lo := 0; lo < end; lo += historyLookupChunkMessages {
		hi := min(lo+historyLookupChunkMessages, end)
		msgs, err := src.fetch(lo, hi)
		if err != nil {
			return err
		}
		if len(msgs) != hi-lo {
			return fmt.Errorf("history lookup window length %d, want %d", len(msgs), hi-lo)
		}
		if err := visit(msgs); err != nil {
			return err
		}
	}
	return nil
}

// historyTodoArgsForSource derives completed todo state in two bounded passes:
// first discover successful calls anywhere in the transcript, then replay the
// todo stream. The legacy converter does the same work over an in-memory
// slice; doing it here prevents a page cut from displaying stale todo items.
func historyTodoArgsForSource(src *historySliceSource) (map[string]string, error) {
	successful := map[string]bool{}
	if err := forEachHistorySourceChunk(src, src.total, func(msgs []provider.Message) error {
		for _, msg := range msgs {
			if msg.Role == provider.RoleTool && msg.ToolCallID != "" && !historyToolResultFailed(msg.Content) {
				successful[msg.ToolCallID] = true
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	state := newHistoryTodoArgsState(successful)
	if err := forEachHistorySourceChunk(src, src.total, func(msgs []provider.Message) error {
		for _, msg := range msgs {
			state.consume(msg)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return state.out, nil
}

// primeHistoryPlannerState consumes the non-rendered prefix so planner
// displays remain FIFO per duplicated user text and an interrupt's canonical
// suppression crosses page boundaries exactly as in a full conversion.
func primeHistoryPlannerState(src *historySliceSource, state *historyMessageConvertState, end int, resolver func(string) string) error {
	if end <= 0 || len(state.plannerByUserHash) == 0 {
		return nil
	}
	return forEachHistorySourceChunk(src, end, func(msgs []provider.Message) error {
		for _, msg := range msgs {
			state.consumeHistoryPlannerState(msg, resolver)
		}
		return nil
	})
}

// newHistoryEntry builds one entry, replacing oversized string fields with
// preview + ref. entryID is the fully-built entry ID (message- or row-form).
func newHistoryEntry(src *historySliceSource, entryID string, msgIndex, sub int, row HistoryMessage) HistoryEntry {
	entry := HistoryEntry{
		EntryID: entryID,
		Turn:    src.turns[msgIndex],
		Order:   msgIndex,
		Message: row,
		Refs:    []HistoryContentRef{},
	}
	addRef := func(field, toolCallID string, size, chunks int) {
		entry.Refs = append(entry.Refs, HistoryContentRef{
			EntryID:    entryID,
			Field:      field,
			Size:       size,
			Chunks:     chunks,
			ToolCallID: toolCallID,
			Revision:   src.revision,
			RevKnown:   src.revKnown,
			Digest:     src.digest,
		})
	}
	m := &entry.Message
	m.Content = truncateHistoryField(m.Content, "content", "", addRef)
	m.Reasoning = truncateHistoryField(m.Reasoning, "reasoning", "", addRef)
	m.SubmitText = truncateHistoryField(m.SubmitText, "submitText", "", addRef)
	m.Detail = truncateHistoryField(m.Detail, "detail", "", addRef)
	m.Code = truncateHistoryField(m.Code, "code", "", addRef)
	m.Summary = truncateHistoryField(m.Summary, "summary", "", addRef)
	m.Archive = truncateHistoryField(m.Archive, "archive", "", addRef)
	m.ToolResultError = truncateHistoryField(m.ToolResultError, "toolResultError", "", addRef)
	for i := range m.ToolCalls {
		tc := &m.ToolCalls[i]
		tc.Arguments = truncateHistoryField(tc.Arguments, "toolArguments", tc.ID, addRef)
		tc.Subject = truncateHistoryField(tc.Subject, "toolSubject", tc.ID, addRef)
		tc.Summary = truncateHistoryField(tc.Summary, "toolSummary", tc.ID, addRef)
		tc.Diff = truncateHistoryField(tc.Diff, "toolDiff", tc.ID, addRef)
	}
	return entry
}

// truncateHistoryField replaces value with a rune-safe preview and registers
// a content ref when it exceeds the inline threshold.
func truncateHistoryField(value, field, toolCallID string, addRef func(field, toolCallID string, size, chunks int)) string {
	if len(value) <= historyInlineRefThreshold {
		return value
	}
	addRef(field, toolCallID, len(value), historyContentChunkCount(value))
	return clipStringBytes(value, historyFieldPreviewBytes)
}

// inlineBytes approximates the JSON payload contributed by the entry's inline
// string fields (post-truncation), for the byte budget.
func (e HistoryEntry) inlineBytes() int {
	m := e.Message
	n := len(m.Content) + len(m.Detail) + len(m.Code) + len(m.SubmitText) +
		len(m.Reasoning) + len(m.Summary) + len(m.Archive) + len(m.ToolResultError) +
		len(m.ToolCallID) + len(m.ToolName) + len(m.Role)
	for _, tc := range m.ToolCalls {
		n += len(tc.Arguments) + len(tc.Subject) + len(tc.Summary) + len(tc.Diff) + len(tc.ID) + len(tc.Name)
	}
	return n
}

// historyWindowWithPersistedTimes is the window-scoped form of
// historyProviderMessagesWithPersistedTimes: userOffset is the number of
// user-role messages before the window, keeping the ordinal alignment with
// the persisted user-message records.
func historyWindowWithPersistedTimes(msgs []provider.Message, sessionPath string, userOffset int) []provider.Message {
	if len(msgs) == 0 || strings.TrimSpace(sessionPath) == "" {
		return msgs
	}
	needsPersistedTime := false
	for _, msg := range msgs {
		if msg.Role == provider.RoleUser && msg.CreatedAt <= 0 && agent.IsUserAuthoredTurn(agent.UserMessageText(msg)) {
			needsPersistedTime = true
			break
		}
	}
	if !needsPersistedTime {
		return msgs
	}
	users, err := agent.LoadSessionUserMessages(sessionPath)
	if err != nil || len(users) <= userOffset {
		return msgs
	}
	out := append([]provider.Message(nil), msgs...)
	userIndex := userOffset
	for i := range out {
		if out[i].Role != provider.RoleUser {
			continue
		}
		if userIndex >= len(users) {
			break
		}
		user := users[userIndex]
		userIndex++
		if out[i].CreatedAt <= 0 && !user.At.IsZero() {
			out[i].CreatedAt = user.At.UnixMilli()
		}
	}
	return out
}

// historyContentChunkCount returns the number of rune-aligned ≤256KiB chunks
// for s. The empty string is one empty chunk.
func historyContentChunkCount(s string) int {
	if len(s) == 0 {
		return 1
	}
	chunks := 0
	for off := 0; off < len(s); chunks++ {
		off = historyContentChunkEnd(s, off)
	}
	return chunks
}

// historyContentChunkEnd returns the end offset of the chunk starting at off:
// off+256KiB backed off to a rune boundary.
func historyContentChunkEnd(s string, off int) int {
	end := off + historyContentChunkBytes
	if end >= len(s) {
		return len(s)
	}
	for end > off && !utf8.RuneStart(s[end]) {
		end--
	}
	return end
}

// historyContentChunkAt returns chunk index (0-based) of s and the total
// chunk count, splitting on rune boundaries.
func historyContentChunkAt(s string, index int) (string, int) {
	chunks := historyContentChunkCount(s)
	if index < 0 {
		index = 0
	}
	off := 0
	for i := 0; i < index && off < len(s); i++ {
		off = historyContentChunkEnd(s, off)
	}
	if off >= len(s) {
		return "", chunks
	}
	return s[off:historyContentChunkEnd(s, off)], chunks
}

// HistoryContentForTab returns one chunk of a ref-replaced field's full
// value. The entry is re-resolved through the same window machinery; when the
// session's revision/digest moved past the ref, Stale is set so the frontend
// reloads.
func (a *App) HistoryContentForTab(tabID string, ref HistoryContentRef, chunkIndex int) HistoryContentChunk {
	out := HistoryContentChunk{EntryID: ref.EntryID, Field: ref.Field, Chunk: max(chunkIndex, 0)}
	msgIndex, sub, legacyRow, ok := parseHistoryEntryID(ref.EntryID)
	if !ok {
		out.Done = true
		return out
	}
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	var ctrl control.SessionAPI
	var sessionDir, sessionPath string
	if tab != nil {
		ctrl = tab.Ctrl
		sessionDir = tabSessionDir(tab)
		sessionPath = tab.currentSessionPath()
	}
	a.mu.RUnlock()
	if ctrl != nil {
		if p := ctrl.SessionPath(); strings.TrimSpace(p) != "" {
			sessionPath = p
			sessionDir = controllerSessionDir(ctrl)
		}
	}
	if strings.TrimSpace(sessionPath) == "" {
		out.Done = true
		return out
	}
	sessionID := strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl")
	if entryIDSession(ref.EntryID) != sessionID {
		out.Stale = true
		return out
	}

	var value string
	var found bool
	if legacyRow >= 0 {
		value, found = a.legacyHistoryFieldValue(sessionPath, sessionDir, legacyRow, ref)
	} else if ctrl != nil {
		var stale bool
		value, found, stale = a.liveHistoryFieldValue(ctrl, sessionDir, sessionPath, msgIndex, sub, ref)
		if stale {
			out.Stale = true
			return out
		}
	} else {
		var stale bool
		value, found, stale = a.coldHistoryFieldValue(sessionDir, sessionPath, msgIndex, sub, ref)
		if stale {
			out.Stale = true
			return out
		}
	}
	if !found {
		// The entry or field no longer resolves — content changed underneath.
		out.Stale = true
		return out
	}
	if len(value) != ref.Size {
		out.Stale = true
		return out
	}
	data, chunks := historyContentChunkAt(value, chunkIndex)
	out.Chunks = chunks
	out.Data = data
	out.Done = chunkIndex >= chunks-1
	return out
}

// parseHistoryEntryID parses s<id>:r<epoch>:m<msgIndex>:o<sub> and the legacy
// event-format s<id>:r<epoch>:e<row>:o0 form.
func parseHistoryEntryID(entryID string) (msgIndex, sub, legacyRow int, ok bool) {
	legacyRow = -1
	parts := strings.Split(entryID, ":")
	if len(parts) != 4 {
		return 0, 0, -1, false
	}
	if _, err := fmt.Sscanf(parts[2], "m%d", &msgIndex); err == nil {
		if _, err := fmt.Sscanf(parts[3], "o%d", &sub); err != nil {
			return 0, 0, -1, false
		}
		return msgIndex, sub, -1, true
	}
	if _, err := fmt.Sscanf(parts[2], "e%d", &legacyRow); err == nil {
		return 0, 0, legacyRow, true
	}
	return 0, 0, -1, false
}

func entryIDSession(entryID string) string {
	rest := strings.SplitN(entryID, ":", 2)
	if len(rest) != 2 {
		return ""
	}
	return strings.TrimPrefix(rest[0], "s")
}

// liveHistoryFieldValue re-resolves one entry's field from the live session.
func (a *App) liveHistoryFieldValue(ctrl control.SessionAPI, sessionDir, sessionPath string, msgIndex, sub int, ref HistoryContentRef) (string, bool, bool) {
	wc, ok := ctrl.(historyWindowController)
	if !ok {
		return "", false, true
	}
	ps, psOK := wc.SessionPersistedState()
	revKnown := psOK && ps.RevisionKnown
	revision := int64(0)
	digest := ""
	if psOK {
		revision = ps.Revision
		digest = ps.DigestHex
	}
	if !psOK || revKnown != ref.RevKnown || revision != ref.Revision || digest != ref.Digest {
		return "", false, true
	}
	resolver := sessionDisplayResolver(sessionDir, sessionPath)
	src, _ := a.liveHistorySliceSource(ctrl, sessionPath, resolver)
	if src == nil {
		return "", false, true
	}
	return a.historyFieldValueForSource(src, msgIndex, sub, ref, resolver, sessionPlannerDisplayTurns(sessionDir, sessionPath), ctrl.CheckpointTurnsByMessageIndex())
}

// coldHistoryFieldValue re-resolves one entry's field through the same
// authoritative source selection as HistorySliceForTab. It never trusts a
// stale checkpoint merely because the requested message's old offset exists.
func (a *App) coldHistoryFieldValue(sessionDir, sessionPath string, msgIndex, sub int, ref HistoryContentRef) (string, bool, bool) {
	absPath, _, err := validateSessionPath(sessionDir, sessionPath)
	if err != nil {
		return "", false, true
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", false, true
	}
	resolver := sessionDisplayResolver(sessionDir, absPath)
	idx, idxErr := agent.LoadSessionDisplayIndex(store.SessionDisplayIndex(absPath))
	identity, identityKnown, identityErr := agent.SessionContentIdentity(absPath)
	if identityErr != nil {
		return "", false, true
	}
	valid := idxErr == nil && idx != nil && idx.TranscriptSize == info.Size() && historyIndexTimestampValid(store.SessionDisplayIndex(absPath), absPath, info, idx, true)
	if valid && identityKnown {
		valid = agent.ValidateSessionDisplayIndex(idx, identity.Revision, identity.RevisionKnown, identity.Digest, info.Size())
	} else if valid {
		valid = !idx.RevisionKnown
	}
	var src *historySliceSource
	if valid {
		src = coldHistorySliceSource(absPath, idx)
	} else if scanned, scanErr := agent.ScanSessionDisplayIndex(absPath); scanErr == nil && (!identityKnown || scanned.ContentDigest == identity.DigestHex) {
		if identityKnown {
			scanned.Revision = identity.Revision
			scanned.RevisionKnown = identity.RevisionKnown
		}
		_ = agent.WriteSessionDisplayIndex(store.SessionDisplayIndex(absPath), scanned)
		src = coldHistorySliceSource(absPath, scanned)
	} else {
		messages, state, repairable, loadErr := agent.LoadSessionDisplayMessages(absPath)
		if loadErr != nil {
			return "", false, true
		}
		src = newInMemoryHistorySliceSource(strings.TrimSuffix(filepath.Base(absPath), ".jsonl"), messages, resolver, state, true)
		src.cacheKey = historyDerivedSourceKey(absPath, src)
		if repairable {
			a.kickHistoryReadModelRepair(absPath)
		}
	}
	return a.historyFieldValueForSource(src, msgIndex, sub, ref, resolver, sessionPlannerDisplayTurns(sessionDir, absPath), nil)
}

func (a *App) historyFieldValueForSource(src *historySliceSource, msgIndex, sub int, ref HistoryContentRef, resolver func(string) string, plannerTurns []plannerDisplayTurn, checkpointTurns map[int]int) (string, bool, bool) {
	if src == nil || !src.identityMatches(ref.Revision, ref.RevKnown, ref.Digest) || msgIndex < 0 || msgIndex >= src.total {
		return "", false, true
	}
	msgs, err := src.fetch(msgIndex, msgIndex+1)
	if err != nil || len(msgs) != 1 {
		return "", false, true
	}
	toolResults := historyToolResultsByID(msgs)
	if err := extendHistoryToolResults(src, msgs, msgIndex+1, toolResults); err != nil {
		return "", false, true
	}
	todoArgs := map[string]string{}
	if historyWindowContainsTodoWrite(msgs) {
		todoArgs, err = a.historyDerived.todoArgs(src.cacheKey, func() (map[string]string, error) {
			return historyTodoArgsForSource(src)
		})
		if err != nil {
			return "", false, true
		}
	}
	state := newHistoryMessageConvertState(plannerTurns)
	if err := primeHistoryPlannerState(src, state, msgIndex, resolver); err != nil {
		return "", false, true
	}
	rows := state.convertHistoryMessage(msgIndex, msgs[0], resolver, checkpointTurns, todoArgs, toolResults)
	if sub < 0 || sub >= len(rows) {
		return "", false, true
	}
	value, found := historyEntryFieldValue(&rows[sub], ref.Field, ref.ToolCallID)
	return value, found, false
}

// historyEntryFieldValue reads one field of a converted row by ref field name.
func historyEntryFieldValue(m *HistoryMessage, field, toolCallID string) (string, bool) {
	switch field {
	case "content":
		return m.Content, true
	case "reasoning":
		return m.Reasoning, true
	case "submitText":
		return m.SubmitText, true
	case "detail":
		return m.Detail, true
	case "code":
		return m.Code, true
	case "summary":
		return m.Summary, true
	case "archive":
		return m.Archive, true
	case "toolResultError":
		return m.ToolResultError, true
	case "toolArguments", "toolSubject", "toolSummary", "toolDiff":
		for i := range m.ToolCalls {
			if m.ToolCalls[i].ID != toolCallID {
				continue
			}
			switch field {
			case "toolArguments":
				return m.ToolCalls[i].Arguments, true
			case "toolSubject":
				return m.ToolCalls[i].Subject, true
			case "toolSummary":
				return m.ToolCalls[i].Summary, true
			case "toolDiff":
				return m.ToolCalls[i].Diff, true
			}
		}
		return "", false
	}
	return "", false
}

// --- Legacy event-format paging -------------------------------------------

// coldEventHistorySlice pages a legacy event-record session. The decode
// streams the file once per request (constant memory); only ancient sessions
// take this path.
func coldEventHistorySlice(sessionPath string, info os.FileInfo, req HistorySliceRequest) (HistorySlice, error) {
	messages, ok, err := previewEventSessionMessages(sessionPath)
	if err != nil || !ok {
		return emptyHistorySlice(), err
	}
	digest := fmt.Sprintf("event:%d:%d", info.Size(), info.ModTime().UnixNano())
	src := &historySliceSource{
		sessionID: strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl"),
		digest:    digest,
	}
	return pageHistoryEventRows(src, messages, req), nil
}

// pageHistoryEventRows cuts a page from already-converted rows (legacy event
// format). Row indexes play the role of message indexes; every row is its own
// group. Turns count user rows, 1-based.
func pageHistoryEventRows(src *historySliceSource, rows []HistoryMessage, req HistorySliceRequest) HistorySlice {
	cursor, err := decodeHistorySliceCursor(req.Cursor)
	hasCursor := req.Cursor != "" && err == nil
	if hasCursor && src.digest != cursor.Digest {
		return staleHistorySlice(0, false, src.digest)
	}
	hi := len(rows)
	if hasCursor && cursor.Before < hi {
		hi = cursor.Before
	}
	// Visible turn per row.
	turns := make([]int, len(rows))
	turn := 0
	for i, r := range rows {
		if r.Role == "user" {
			turn++
		}
		turns[i] = turn
	}
	src.turns = turns
	src.totalTurns = turn
	src.total = len(rows)
	page := HistorySlice{Entries: []HistoryEntry{}, TotalTurns: turn, Digest: src.digest}
	if hi <= 0 {
		return page
	}
	newestTurn := turns[hi-1]
	oldestTurn := 0
	if newestTurn > 0 {
		oldestTurn = max(newestTurn-req.Turns+1, 1)
	}
	candidateLo := sort.Search(hi, func(i int) bool { return turns[i] >= oldestTurn })
	if oldestTurn <= 1 {
		candidateLo = 0
	}
	// Suffix cut: walk backward from the newest row, keeping whole rows until
	// a budget is reached; always keep the newest row so a single oversized
	// row still makes progress.
	kept := make([]HistoryEntry, 0, req.Entries)
	entryCount, byteCount := 0, 0
	lo := hi
	for i := hi - 1; i >= candidateLo; i-- {
		entry := newHistoryEntry(src, fmt.Sprintf("s%s:r0:e%d:o0", src.sessionID, i), i, 0, rows[i])
		b := entry.inlineBytes()
		if len(kept) > 0 && (entryCount+1 > req.Entries || byteCount+b > req.Bytes) {
			break
		}
		kept = append(kept, entry)
		entryCount++
		byteCount += b
		lo = i
	}
	for _, e := range slices.Backward(kept) {
		page.Entries = append(page.Entries, e)
	}
	for _, e := range page.Entries {
		if e.Turn <= 0 {
			continue
		}
		if page.StartTurn == 0 || e.Turn < page.StartTurn {
			page.StartTurn = e.Turn
		}
		if e.Turn > page.EndTurn {
			page.EndTurn = e.Turn
		}
	}
	page.HasOlder = lo > 0
	if page.HasOlder {
		page.NextCursor = encodeHistorySliceCursor(historySliceCursor{V: 1, Digest: src.digest, Before: lo})
	}
	return page
}

// legacyHistoryFieldValue re-resolves a field of a legacy event-format row.
func (a *App) legacyHistoryFieldValue(sessionPath, sessionDir string, row int, ref HistoryContentRef) (string, bool) {
	absPath, _, err := validateSessionPath(sessionDir, sessionPath)
	if err != nil {
		return "", false
	}
	messages, ok, err := previewEventSessionMessages(absPath)
	if err != nil || !ok || row < 0 || row >= len(messages) {
		return "", false
	}
	return historyEntryFieldValue(&messages[row], ref.Field, ref.ToolCallID)
}

// --- Background index maintenance ------------------------------------------

// kickHistoryIndexRebuild single-flight schedules a background display-index
// rebuild for a live session whose on-disk index did not validate. It never
// blocks the request path.
func (a *App) kickHistoryIndexRebuild(sessionPath string) {
	if strings.TrimSpace(sessionPath) == "" {
		return
	}
	a.historySliceMu.Lock()
	if a.historyIndexRebuilds == nil {
		a.historyIndexRebuilds = map[string]struct{}{}
	}
	if _, ok := a.historyIndexRebuilds[sessionPath]; ok {
		a.historySliceMu.Unlock()
		return
	}
	a.historyIndexRebuilds[sessionPath] = struct{}{}
	a.historySliceMu.Unlock()
	a.goSafe("historyIndexRebuild", func() {
		defer func() {
			a.historySliceMu.Lock()
			delete(a.historyIndexRebuilds, sessionPath)
			a.historySliceMu.Unlock()
		}()
		a.rebuildHistoryIndexForLiveSession(sessionPath)
	})
}

// kickHistoryReadModelRepair single-flights the stronger cold-session repair:
// replay the authoritative event log under the save lock, atomically refresh
// the JSONL random-read model, then publish matching offsets. The cold request
// already returned from its in-memory recovery source before this work starts.
func (a *App) kickHistoryReadModelRepair(sessionPath string) {
	if strings.TrimSpace(sessionPath) == "" {
		return
	}
	key := "read-model:" + agent.CanonicalSessionPath(sessionPath)
	a.historySliceMu.Lock()
	if a.historyIndexRebuilds == nil {
		a.historyIndexRebuilds = map[string]struct{}{}
	}
	if _, ok := a.historyIndexRebuilds[key]; ok {
		a.historySliceMu.Unlock()
		return
	}
	a.historyIndexRebuilds[key] = struct{}{}
	a.historySliceMu.Unlock()
	a.goSafe("historyReadModelRepair", func() {
		defer func() {
			a.historySliceMu.Lock()
			delete(a.historyIndexRebuilds, key)
			a.historySliceMu.Unlock()
		}()
		if err := agent.RepairSessionDisplayReadModel(sessionPath); err != nil {
			slog.Debug("desktop: history read-model repair failed", "path", sessionPath, "err", err)
		}
	})
}

// rebuildHistoryIndexForLiveSession republishes the display index for a live
// session, but only when the in-memory log is exactly the persisted
// transcript — an append-only tail means the next save will publish a
// covering index anyway, and a scanned .jsonl anchor cannot describe the
// event-log tail.
func (a *App) rebuildHistoryIndexForLiveSession(sessionPath string) {
	a.mu.RLock()
	ctrls := make([]control.SessionAPI, 0, len(a.tabs))
	for _, tab := range a.tabs {
		if tab != nil && tab.Ctrl != nil {
			ctrls = append(ctrls, tab.Ctrl)
		}
	}
	a.mu.RUnlock()
	var ctrl control.SessionAPI
	for _, c := range ctrls {
		if c.SessionPath() == sessionPath {
			ctrl = c
			break
		}
	}
	wc, ok := ctrl.(historyWindowController)
	if !ok {
		return
	}
	ps, ok := wc.SessionPersistedState()
	if !ok || !ps.UnchangedSincePersisted {
		return
	}
	if err := agent.RepairSessionDisplayReadModel(sessionPath); err != nil {
		slog.Debug("desktop: live history read-model rebuild failed", "path", sessionPath, "err", err)
	}
}

// startHistoryIndexMigration arms the startup background worker that builds
// display indexes for session files that predate the sidecar. Like
// enableDeferredRebuildRetry it is only called from the Wails startup hook, so
// test-constructed Apps never spawn the worker.
func (a *App) startHistoryIndexMigration() {
	if a.ctx == nil {
		return
	}
	a.historySliceMu.Lock()
	if a.historyIndexMigrationCancel != nil {
		a.historySliceMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.historyIndexMigrationCancel = cancel
	a.historySliceMu.Unlock()
	a.goSafe("historyIndexMigration", func() { a.historyIndexMigrationLoop(ctx) })
}

// stopHistoryIndexMigration stops the startup migration worker; called from
// shutdown. The worker also stops with the Wails context.
func (a *App) stopHistoryIndexMigration() {
	a.historySliceMu.Lock()
	cancel := a.historyIndexMigrationCancel
	a.historySliceMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// historyIndexMigrationLoop walks every known session dir once, building
// missing or stale display indexes. It is single-concurrency, yields between
// sessions, and is idempotent: a valid index (loadable + transcript size
// match) is left untouched.
func (a *App) historyIndexMigrationLoop(ctx context.Context) {
	for _, dir := range a.knownSessionDirs() {
		if ctx.Err() != nil {
			return
		}
		// ListSessionOrder is the lightweight listing: it never decodes
		// transcript content, which keeps this worker cheap on dirs full of
		// legacy sessions.
		infos, err := agent.ListSessionOrder(dir)
		if err != nil {
			continue
		}
		for _, info := range infos {
			if ctx.Err() != nil {
				return
			}
			path := info.Path
			if !store.IsSessionTranscriptName(filepath.Base(path)) {
				continue
			}
			if historySessionIndexOnDiskValid(path) || historySessionLooksEventFormat(path) {
				continue
			}
			if err := agent.RepairSessionDisplayReadModel(path); err != nil {
				slog.Debug("desktop: history read-model migration failed", "path", path, "err", err)
			}
			timer := time.NewTimer(25 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

// historySessionIndexOnDiskValid reports whether the on-disk display index
// loads and describes the current transcript file size. The size guard is the
// Phase A stale-anchor rule: append-only saves leave the .jsonl anchor behind
// the canonical transcript, and the reverse (a rewritten anchor with an old
// index) must not be sliced by stale offsets either.
func historySessionIndexOnDiskValid(sessionPath string) bool {
	indexPath := store.SessionDisplayIndex(sessionPath)
	idx, err := agent.LoadSessionDisplayIndex(indexPath)
	if err != nil {
		return false
	}
	info, err := os.Stat(sessionPath)
	if err != nil {
		return false
	}
	if idx.TranscriptSize != info.Size() || !historyIndexTimestampValid(indexPath, sessionPath, info, idx, false) {
		return false
	}
	identity, known, err := agent.SessionContentIdentity(sessionPath)
	if err != nil {
		return false
	}
	if !known {
		return !idx.RevisionKnown
	}
	return agent.ValidateSessionDisplayIndex(idx, identity.Revision, identity.RevisionKnown, identity.Digest, info.Size())
}
