package control

import (
	"context"
	"errors"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/base/diff"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/store"
)

// checkpointManager owns the snapshot-based rewind bookkeeping: the per-session
// checkpoint store, the monotonic turn counter, and the conversation-rewind
// boundary map. Like approvalManager it holds only the bookkeeping behind its own
// lock, off the controller's c.mu — the Controller keeps the rewind/fork
// orchestration (truncating the session, restoring code, emitting events) that
// needs its other collaborators.
//
// turn is decoupled from the store so it remains monotonic across session work;
// bound[turn] records len(Session.Messages) at that turn's start — the truncation
// boundary for a conversation rewind/fork. Boundaries are persisted in each
// checkpoint and rebuilt from the store on resume (so a reopened session can still
// rewind conversation / fork). Context compression never changes the transcript,
// so it leaves these boundaries intact. Every store call does its disk I/O off mu —
// mu is taken only to read/swap the store pointer and mutate turn/bound.
type checkpointManager struct {
	// mu guards store, turn, and bound; every critical section under it is short
	// and non-blocking (no disk I/O).
	mu    sync.Mutex
	store *checkpoint.Store
	turn  int
	bound map[int]int
}

// rebind points the store at the (possibly new) session, loading any checkpoints
// already on disk, and resets the turn counter and boundaries from them. root is
// the workspace root used to guard restore writes. Called on construction and
// whenever the session path changes (NewSession/Resume/SetSessionPath/fork).
func (m *checkpointManager) rebind(dir, root string) {
	store := checkpoint.New(dir, root)
	next := store.NextTurn() // continue numbering past any checkpoints on disk
	bound := store.Bounds()  // rebuilt from persisted checkpoints so a resumed
	if bound == nil {        // session can still rewind conversation / fork
		bound = map[int]int{}
	}
	m.mu.Lock()
	m.store = store
	m.turn = next
	m.bound = bound
	m.mu.Unlock()
}

// enabled reports whether a checkpoint store is bound.
func (m *checkpointManager) enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store != nil
}

// beginWithObserver opens a checkpoint and updates the mutation observer's
// ownership turn for subsequent captures.
func (m *checkpointManager) beginWithObserver(input string, msgIndex int, obs *checkpoint.MutationObserver) (int, *checkpoint.Store, bool) {
	m.mu.Lock()
	store := m.store
	if store == nil {
		m.mu.Unlock()
		return 0, nil, false
	}
	turn := m.turn
	m.turn++
	m.bound[turn] = msgIndex
	m.mu.Unlock()
	if obs != nil {
		obs.NoteCrossTurnBackgroundWriter(turn)
		obs.SetOwnershipTurn(turn)
	}
	store.Begin(turn, input, msgIndex)
	return turn, store, true
}

type guardedTurnCheckpoint struct {
	session      *sessionstore.Session
	store        *checkpoint.Store
	turn         int
	messageIndex int
	openedAt     int64
}

type guardedTurnCompletion struct {
	checkpoint *guardedTurnCheckpoint
}

type guardedTurnCompletionKey struct{}

func withGuardedTurnCompletion(ctx context.Context) (context.Context, *guardedTurnCompletion) {
	completion := &guardedTurnCompletion{}
	return context.WithValue(ctx, guardedTurnCompletionKey{}, completion), completion
}

// beginCheckpoint opens a rewind checkpoint before the visible user message is
// appended. Guarded turns retain the exact boundary so TurnDone can identify
// the corresponding optimistic frontend item without positional guessing.
func (c *Controller) beginCheckpoint(ctx context.Context, input string) {
	if c.executor == nil || c.executor.Session() == nil {
		return
	}
	session := c.executor.Session()
	messageIndex := session.Len()
	openedAt := time.Now().UnixMilli()
	atomic.AddInt64(&c.sessionRevision, 1)
	turn, store, ok := c.checkpoints.beginWithObserver(input, messageIndex, c.mutationObserver)
	if ok {
		if completion, _ := ctx.Value(guardedTurnCompletionKey{}).(*guardedTurnCompletion); completion != nil {
			completion.checkpoint = &guardedTurnCheckpoint{
				session: session, store: store, turn: turn, messageIndex: messageIndex, openedAt: openedAt,
			}
		}
	}
	// User-visible turn start records an irreversible message-send receipt so
	// recovery never claims a clean rollback of an already-committed prompt.
	// Keep this owner bookkeeping even when checkpoints are disabled.
	gen := c.RuntimeGeneration()
	if gen == 0 {
		gen = c.RuntimeOwner().Gate.Published()
	}
	msgID := fmt.Sprintf("turn-%d-%d", gen, atomic.LoadInt64(&c.sessionRevision))
	// Dedup: a retried turn with the same revision must not double-record.
	owner := c.RuntimeOwner()
	owner.RecordMessageSentOnce(gen, msgID, "control")
	d := owner.DecideResume(gen)
	c.mu.Lock()
	c.lastResumeDecision = d
	c.mu.Unlock()
}

// validatedCheckpointTurn returns the checkpoint only while its original
// boundary still names the real user message committed by this guarded turn.
// Stale or synthetic candidates fail closed rather than being relocated.
func (c *Controller) validatedCheckpointTurn(completion *guardedTurnCompletion) *int {
	if completion == nil || completion.checkpoint == nil || c.executor == nil {
		return nil
	}
	candidate := completion.checkpoint
	if c.executor.Session() != candidate.session {
		return nil
	}
	if !c.checkpoints.matchesBoundary(candidate.store, candidate.turn, candidate.messageIndex) {
		return nil
	}
	messages := candidate.session.Snapshot()
	if candidate.messageIndex < 0 || candidate.messageIndex >= len(messages) {
		return nil
	}
	message := messages[candidate.messageIndex]
	if message.Role != provider.RoleUser || message.LocalOnly ||
		!sessionstore.IsUserAuthoredTurn(sessionstore.UserMessageText(message)) ||
		(message.CreatedAt > 0 && candidate.openedAt > 0 && message.CreatedAt < candidate.openedAt) {
		return nil
	}
	turn := candidate.turn
	return &turn
}

func (m *checkpointManager) matchesBoundary(store *checkpoint.Store, turn, messageIndex int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	boundary, ok := m.bound[turn]
	return ok && m.store == store && boundary == messageIndex
}

// turnsByMessageIndex returns message-log index -> checkpoint turn over live
// boundaries. The desktop transcript uses this authoritative map instead of
// recounting visible user bubbles, which can diverge when synthetic user-role
// messages are hidden from the UI.
func (m *checkpointManager) turnsByMessageIndex() map[int]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int]int, len(m.bound))
	for turn, index := range m.bound {
		if existing, ok := out[index]; ok && existing < turn {
			continue
		}
		out[index] = turn
	}
	return out
}

// boundary returns the recorded turn-start message index, if any.
func (m *checkpointManager) boundary(turn int) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.bound[turn]
	return b, ok
}

// list returns the checkpoint metadata (nil when disabled).
func (m *checkpointManager) list() []checkpoint.Meta {
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	if store == nil {
		return nil
	}
	return store.List()
}

func (m *checkpointManager) fileState(path string) (checkpoint.FileState, bool) {
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	if store == nil {
		return checkpoint.FileState{}, false
	}
	return store.FileState(path)
}

// snapshot records a pre-edit file change into the open checkpoint — the
// executor's pre-edit hook. No-op when disabled.
func (m *checkpointManager) snapshot(ch diff.Change) {
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	if store != nil {
		store.Snapshot(ch)
	}
}

// truncateFrom renumbers future turns from `turn` and drops every boundary at or
// after it — the conversation-rewind renumber after the message log is cut back.
func (m *checkpointManager) truncateFrom(turn int) error {
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	if store != nil {
		if err := store.TruncateFrom(turn); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.turn = turn
	for k := range m.bound {
		if k >= turn {
			delete(m.bound, k)
		}
	}
	m.mu.Unlock()
	return nil
}

// storeRef returns the live store pointer without holding mu across caller work.
func (m *checkpointManager) storeRef() *checkpoint.Store {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store
}

// ckptDir derives a session's checkpoint directory from its file path
// (…/<id>.jsonl → …/<id>.ckpt). Empty path → empty (in-memory checkpoints).
func ckptDir(sessionPath string) string {
	return store.SessionCheckpointDir(sessionPath)
}

// rebindCheckpoints points the store at the (possibly new) session, loading any
// checkpoints already on disk, and resets the turn boundaries. Called on
// construction and whenever the session path changes (NewSession/Resume/SetSessionPath).
// Also re-wires the mutation observer so capture targets the new store.
func (c *Controller) rebindCheckpoints(sessionPath string) {
	c.goals.setStatePath(goalStatePath(sessionPath))
	c.checkpoints.rebind(ckptDir(sessionPath), c.workspaceRoot)
	if c.executor != nil {
		c.wireMutationObserver()
	}
}

// Checkpoints lists the session's rewind points (one per user turn), oldest first.
//
// Each Meta.Prompt is reduced to what the user typed. A checkpoint opens with
// the composed turn, so the stored prompt can carry the plan-mode marker and
// transient blocks; every consumer of this list is a label (the rewind picker,
// the desktop change list, the workbench projection) and the picker also
// restores the prompt into the composer, so composed text must not reach them.
// Stripping on read rather than only on write keeps checkpoints already on disk
// readable — they were recorded composed.
func (c *Controller) Checkpoints() []checkpoint.Meta {
	metas := c.checkpoints.list()
	for i := range metas {
		metas[i].Prompt = StripComposePrefixes(metas[i].Prompt)
	}
	return metas
}

func (c *Controller) CheckpointFileState(path string) (checkpoint.FileState, bool) {
	return c.checkpoints.fileState(path)
}

func (c *Controller) CheckpointTurnsByMessageIndex() map[int]int {
	return c.checkpoints.turnsByMessageIndex()
}

// rewindFail emits the error as a Warn notice (so a frontend that swallows the
// returned error — e.g. the desktop bridge's .catch — still shows the user why
// the rewind did nothing) and returns it.
func (c *Controller) rewindFail(err error) error {
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: err.Error()})
	return err
}

func (c *Controller) CheckpointHasBoundary(turn int) bool {
	boundary, ok := c.checkpoints.boundary(turn)
	if !ok {
		return false
	}
	// After compaction the key may still exist but the boundary value is
	// stale (it points past the truncated message log).  Treat those
	// turns the same as "no boundary" so the UI can disable the button.
	// Len is lock-guarded: this runs on frontend goroutines while a turn appends.
	return boundary <= c.executor.Session().Len()
}

// SummarizeFrom and SummarizeUpTo preserve the historical turn-index API while
// changing only the model-visible context projection. The canonical transcript
// and checkpoint boundaries remain available for rewind and undo.
func (c *Controller) SummarizeFrom(ctx context.Context, turn int) error {
	return c.summarizeAt(ctx, turn, true)
}

func (c *Controller) SummarizeUpTo(ctx context.Context, turn int) error {
	return c.summarizeAt(ctx, turn, false)
}

func (c *Controller) summarizeAt(ctx context.Context, turn int, from bool) error {
	if c.executor == nil {
		return c.rewindFail(fmt.Errorf("checkpoints unavailable"))
	}
	// Hold the rotation gate from the checkpoint-boundary lookup through
	// projection installation so a turn cannot start against an intermediate
	// context view.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return c.rewindFail(fmt.Errorf("cannot summarize while a turn is running"))
		}
		return c.rewindFail(err)
	}
	defer c.endRotation()
	boundary, hasBound := c.checkpoints.boundary(turn)
	if !hasBound {
		return c.rewindFail(fmt.Errorf("summarize unavailable for turn %d (resumed session)", turn))
	}
	var err error
	if from {
		err = c.executor.SummarizeFrom(ctx, boundary)
	} else {
		err = c.executor.SummarizeUpTo(ctx, boundary)
	}
	if err != nil {
		return c.rewindFail(err)
	}
	return nil
}

// parseRewind parses the arguments after "/rewind". The user may provide:
//
//	/rewind              → latest checkpoint, both
//	/rewind <turn>       → that turn, both
//	/rewind <turn> <scope> → that turn, code|conversation|both
//
// If no turn is given, the latest checkpoint is used. If no scope is given, Both is assumed.
func parseRewind(args string, cps []checkpoint.Meta) (int, RewindScope, error) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		if len(cps) == 0 {
			return 0, RewindBoth, fmt.Errorf("no checkpoints available")
		}
		return cps[len(cps)-1].Turn, RewindBoth, nil
	}
	turn, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, RewindBoth, fmt.Errorf("invalid turn: %w", err)
	}
	scope := RewindBoth
	if len(fields) >= 2 {
		switch strings.ToLower(fields[1]) {
		case "code":
			scope = RewindCode
		case "conversation":
			scope = RewindConversation
		case "both":
			scope = RewindBoth
		default:
			return 0, RewindBoth, fmt.Errorf("unknown scope %q", fields[1])
		}
	}
	return turn, scope, nil
}
