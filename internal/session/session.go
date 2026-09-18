package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/sessioncontent"
	"reasonix/internal/transcript"
)

// Session owns the live business state for one session identity: sequence
// allocation, the bounded accepted tail, compact operation identities, and the
// current projection. Durable history bodies and searchable operation details
// live behind Query; the physical handle owns bytes and the writer lease.
//
// A commit becomes observable here before it is durable. That is deliberate and
// mirrors DSH: accepting an event updates the projection and the UI, while the
// binding batches the write-behind and Flush marks a semantic checkpoint.
type Session struct {
	transcript  *transcript.Projection
	mu          sync.Mutex
	id          string
	manifest    Manifest
	next        uint64
	commits     []Commit
	operations  map[string]operationRecord
	projection  Projection
	binding     *PersistenceBinding
	sealed      bool
	sealedError error
	readOnly    bool
	// externalHistory means durable UI messages live in HistoryQuery rather
	// than this runtime projection. Messages then contains only the accepted,
	// not-yet-durable tail; ModelMessages remains the exact provider workset.
	externalHistory   bool
	catalogPreview    string
	recentMessages    []provider.Message
	durableRecent     []provider.Message
	storageGeneration string
	recovery          *recoveryStore
	// coldHandle backs a read-only session, which has no binding because it
	// never enqueues or drains anything.
	coldHandle SessionHandle
}

// PreparedBatch is a validated, self-contained commit payload. Every expensive
// or fallible step — payload copying, schema validation, turn identity
// derivation, and the operation hash — happens here, before the caller takes
// the activity commit gate. CommitPrepared then only assigns identity and
// extends the log.
type PreparedBatch struct {
	sessionID        string
	writerGeneration uint64
	operationID      string
	turnID           string
	events           []Event
	storedEvents     []Event
	hash             string
	reservation      *queueReservation
	prior            *operationRecord
}

// OperationID reports the stable idempotency key of the prepared batch.
func (p PreparedBatch) OperationID() string { return p.operationID }

// Empty reports whether the batch carries no committable event.
func (p PreparedBatch) Empty() bool { return len(p.events) == 0 }

// Release returns queue capacity when a prepared batch loses its activity or
// CAS race before acceptance. It is safe after CommitPrepared consumes it.
func (p PreparedBatch) Release() { p.reservation.release() }

// newSession builds the live in-memory session over an already-open binding.
// commits is the durable prefix replayed by the handle; the projection is
// rebuilt from it so no business state is inherited from the physical layer.
func newSession(id string, manifest Manifest, commits []Commit, projection Projection, binding *PersistenceBinding) *Session {
	operations := make(map[string]operationRecord, len(commits))
	next := uint64(1)
	for _, commit := range commits {
		operations[commit.OperationID] = compactOperationRecord(commit)
		next = commit.LastSequence() + 1
	}
	return &Session{
		id: id, manifest: manifest, next: next,
		operations: operations, projection: projection, binding: binding,
	}
}

// ID returns the immutable session identity.
func (s *Session) ID() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

// Manifest returns the persistence manifest that describes this session.
func (s *Session) Manifest() Manifest {
	if s == nil {
		return Manifest{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest := s.manifest
	if manifest.Source != nil {
		source := *manifest.Source
		manifest.Source = &source
	}
	return manifest
}

// EventSequence reports the last accepted sequence. Accepted events may still
// be waiting in the binding's write-behind queue.
func (s *Session) EventSequence() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next - 1
}

// PrepareBatch validates the logical batch and computes its idempotency digest
// without touching the commit lock. Callers must treat the result as immutable.
func (s *Session) PrepareBatch(operationID string, batch Batch) (PreparedBatch, error) {
	return s.PrepareBatchContext(context.Background(), operationID, batch)
}

// PrepareBatchContext publishes large immutable payloads before the batch can
// enter the accepted sequence. It performs all disk I/O outside the session
// commit lock; CommitPrepared only revalidates identity and generation.
func (s *Session) PrepareBatchContext(ctx context.Context, operationID string, batch Batch) (PreparedBatch, error) {
	if s == nil {
		return PreparedBatch{}, fmt.Errorf("session: nil session")
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		operationID = strings.TrimSpace(batch.OperationID)
	}
	if operationID == "" || len(batch.Events) == 0 {
		return PreparedBatch{}, fmt.Errorf("session: operation id and events are required")
	}
	turnID := strings.TrimSpace(batch.TurnID)
	if turnID == "" && batchContains(batch.Events, "turn/start") {
		turnID = deterministicID("turn\x00" + s.id + "\x00" + operationID)
	}
	events := cloneEvents(batch.Events)
	for i := range events {
		event := &events[i]
		event.Kind = strings.TrimSpace(event.Kind)
		if event.Kind == "message/retract" {
			if event.Optional {
				return PreparedBatch{}, fmt.Errorf("message/retract must be required")
			}
			event.Required = true
		}
		if event.Kind == "" {
			return PreparedBatch{}, fmt.Errorf("session: events[%d].kind is required", i)
		}
		if !event.Optional && !ProjectionKinds[event.Kind] {
			return PreparedBatch{}, fmt.Errorf("%w: unknown required event %q", ErrUnsupportedVersion, event.Kind)
		}
		// The durable sequence is assigned at commit time so a rejected batch
		// never consumes one.
		event.Sequence = 0
	}
	// The digest covers exactly what the caller supplied. Generated event ids
	// are deliberately excluded so retrying the same logical batch is
	// idempotent instead of reporting a spurious operation conflict.
	hash, err := hashOperation(s.id, turnID, events)
	if err != nil {
		return PreparedBatch{}, err
	}
	prior, found, err := s.lookupOperation(operationID)
	if err != nil {
		return PreparedBatch{}, fmt.Errorf("session: lookup operation %q: %w", operationID, err)
	}
	if found && prior.hash != hash {
		return PreparedBatch{}, fmt.Errorf("%w: %q", ErrOperationConflict, operationID)
	}
	for i := range events {
		if events[i].ID == "" {
			events[i].ID = randomID()
		}
	}
	storedEvents := cloneEvents(events)
	content := s.contentStore()
	for i := range storedEvents {
		if len(storedEvents[i].Payload) <= v4InlinePayloadBytes {
			continue
		}
		if content == nil {
			return PreparedBatch{}, errors.New("session: content store unavailable for large event payload")
		}
		ref, err := content.Put(ctx, bytes.NewReader(storedEvents[i].Payload), sessioncontent.Metadata{MediaType: "application/json"})
		if err != nil {
			return PreparedBatch{}, fmt.Errorf("prepare event %s content: %w", storedEvents[i].ID, err)
		}
		storedEvents[i].Payload = nil
		storedEvents[i].PayloadRef = &ref
	}
	s.mu.Lock()
	sessionID, writerGeneration, binding := s.id, s.manifest.WriterGeneration, s.binding
	s.mu.Unlock()
	if binding == nil {
		return PreparedBatch{}, ErrReadOnly
	}
	reservation, err := binding.reserve(ctx, commitHotBytes(Commit{Events: storedEvents}))
	if err != nil {
		return PreparedBatch{}, err
	}
	prepared := PreparedBatch{sessionID: sessionID, writerGeneration: writerGeneration, operationID: operationID, turnID: turnID, events: events, storedEvents: storedEvents, hash: hash, reservation: reservation}
	if found {
		copy := prior
		prepared.prior = &copy
	}
	return prepared, nil
}

func (s *Session) lookupOperation(operationID string) (operationRecord, bool, error) {
	if s == nil {
		return operationRecord{}, false, nil
	}
	s.mu.Lock()
	if record, ok := s.operations[operationID]; ok {
		s.mu.Unlock()
		return record, true, nil
	}
	recovery := s.recovery
	s.mu.Unlock()
	return recovery.lookupOperation(operationID)
}

func (s *Session) contentStore() *sessioncontent.Store {
	if s == nil || s.binding == nil {
		return nil
	}
	store, _ := s.binding.handle.(*Store)
	if store == nil {
		return nil
	}
	return store.content
}

func (s *Session) ContentStore() *sessioncontent.Store { return s.contentStore() }

func (s *Session) StorageGeneration() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.storageGeneration
}

// CommitPrepared appends an already validated batch under one short memory
// lock. The persistence binding only receives an immutable batch into its
// write-behind queue, so this never performs file I/O and never blocks on a
// subscriber.
func (s *Session) CommitPrepared(prepared PreparedBatch) (Commit, error) {
	return s.commitPrepared(prepared, nil)
}

func (s *Session) commitPrepared(prepared PreparedBatch, expectedTitleSequence *uint64) (Commit, error) {
	defer prepared.Release()
	if s == nil {
		return Commit{}, fmt.Errorf("session: nil session")
	}
	if prepared.Empty() {
		return Commit{}, fmt.Errorf("session: operation id and events are required")
	}
	s.mu.Lock()
	if expectedTitleSequence != nil && s.projection.TitleSequence != *expectedTitleSequence {
		s.mu.Unlock()
		return Commit{}, ErrSessionTitleChanged
	}
	if prepared.sessionID != s.id || prepared.writerGeneration != s.manifest.WriterGeneration {
		s.mu.Unlock()
		return Commit{}, ErrStaleGeneration
	}
	if s.readOnly {
		s.mu.Unlock()
		return Commit{}, ErrReadOnly
	}
	if s.sealed {
		err := s.sealedError
		s.mu.Unlock()
		if err == nil {
			err = osClosedError()
		}
		return Commit{}, err
	}
	if prior, ok := s.operations[prepared.operationID]; ok {
		if prior.hash != prepared.hash {
			s.mu.Unlock()
			return Commit{}, fmt.Errorf("%w: %q", ErrOperationConflict, prepared.operationID)
		}
		commit := prior.commit
		commit.Events = cloneEvents(prepared.events)
		for i := range commit.Events {
			commit.Events[i].Sequence = commit.FirstSequence + uint64(i)
		}
		s.mu.Unlock()
		return commit, nil
	}
	if prepared.prior != nil {
		commit := prepared.prior.commit
		commit.Events = cloneEvents(prepared.events)
		for i := range commit.Events {
			commit.Events[i].Sequence = commit.FirstSequence + uint64(i)
		}
		s.mu.Unlock()
		return commit, nil
	}
	commit := Commit{
		SchemaVersion: SchemaVersion, Codec: Codec, RecordType: "commit", ID: randomID(),
		OperationID: prepared.operationID, OperationHash: prepared.hash, FirstSequence: s.next,
		EventCount: len(prepared.events), TurnID: prepared.turnID,
		WriterGeneration: s.manifest.WriterGeneration, CreatedAt: time.Now().UTC(),
		// PreparedBatch already owns a private clone. Transfer it into the
		// immutable accepted commit instead of copying every payload again.
		Events: prepared.events,
	}
	for i := range commit.Events {
		commit.Events[i].Sequence = commit.FirstSequence + uint64(i)
	}
	storedCommit := commit
	storedCommit.Events = prepared.storedEvents
	for i := range storedCommit.Events {
		storedCommit.Events[i].Sequence = storedCommit.FirstSequence + uint64(i)
	}
	projection := cloneProjection(s.projection)
	if err := applyProjectionCommit(&projection, commit); err != nil {
		s.mu.Unlock()
		return Commit{}, err
	}
	binding := s.binding
	if binding == nil {
		s.mu.Unlock()
		return Commit{}, ErrReadOnly
	}
	// Session and the persistence queue form one in-memory acceptance boundary.
	// accept performs no I/O or callbacks; after projection validation there are
	// no remaining fallible state changes in the closure.
	err := binding.accept(storedCommit, prepared.reservation, func() {
		s.commits = append(s.commits, commit)
		s.projection = projection
		_ = applyRecentCommit(&s.recentMessages, commit)
		s.next = commit.LastSequence() + 1
		s.operations[prepared.operationID] = compactOperationRecord(commit)
		s.acceptTranscriptCommit(commit)
	})
	s.mu.Unlock()
	if err != nil {
		return Commit{}, err
	}
	return cloneCommit(commit), nil
}

// AppendBatch is the convenience form used by callers that do not need to
// separate validation from the commit.
func (s *Session) AppendBatch(ctx context.Context, operationID string, events []Event) (Commit, error) {
	return s.Append(ctx, Batch{OperationID: operationID, Events: events})
}

// Append validates and commits a logical batch in one step.
func (s *Session) Append(ctx context.Context, batch Batch) (Commit, error) {
	if err := ctx.Err(); err != nil {
		return Commit{}, err
	}
	prepared, err := s.PrepareBatchContext(ctx, batch.OperationID, batch)
	if err != nil {
		return Commit{}, err
	}
	return s.CommitPrepared(prepared)
}

// Snapshot returns the full live view, including message and turn history.
func (s *Session) Snapshot() Snapshot { return s.snapshot(true, true) }

// StateSnapshot omits history so progress notifications do not copy every
// message and completed turn on each activity update.
func (s *Session) StateSnapshot() Snapshot { return s.snapshot(false, false) }

// ExecutionSnapshot exposes the current provider projection and business
// state without materializing durable UI history. Controllers use it for
// turn/model decisions; UI history is obtained from Query.
func (s *Session) ExecutionSnapshot() Snapshot { return s.snapshot(false, true) }

func (s *Session) snapshot(includeHistory, includeModel bool) Snapshot {
	if s == nil {
		return Snapshot{PersistenceStatus: PersistenceFailed, PersistenceError: "nil session"}
	}
	s.mu.Lock()
	projection := s.projection
	sequence := s.next - 1
	externalHistory := s.externalHistory
	accepted := cloneCommits(s.commits)
	var history eventPageReader
	if s.binding != nil {
		history = s.binding.handle
	} else if s.coldHandle != nil {
		history = s.coldHandle
	}
	s.mu.Unlock()
	if includeHistory && externalHistory {
		// Snapshot is the explicit full-history compatibility boundary. Service
		// progress and Goal paths use StateSnapshot; paged clients use Query.
		// Reconstructing here preserves existing callers without keeping a second
		// durable UI transcript resident in every runtime.
		if messages, err := materializeSnapshotMessages(history, accepted, sequence); err == nil {
			projection.Messages = messages
		}
	}
	if !includeHistory {
		projection.Messages = nil
	}
	if !includeModel {
		projection.ModelMessages, projection.Turns = nil, nil
	}
	snapshot := Snapshot{EventSequence: sequence, Projection: cloneProjection(projection)}
	if s.binding != nil {
		durable, status, detail := s.binding.progress()
		snapshot.DurableSequence, snapshot.PersistenceStatus, snapshot.PersistenceError = durable, status, detail
	} else {
		snapshot.PersistenceStatus = PersistenceReady
	}
	// Nested provider metadata is immutable internally but Go cannot freeze
	// returned slices. Detach it outside the commit lock before exposing it.
	snapshot.Projection.Messages = detachMessages(snapshot.Projection.Messages)
	snapshot.Projection.ModelMessages = detachMessages(snapshot.Projection.ModelMessages)
	return snapshot
}

// CatalogMetadata returns the rebuildable list projection of this session.
func (s *Session) CatalogMetadata() catalogMetadata {
	if s == nil {
		return catalogMetadata{Version: catalogMetadataVersion, Codec: Codec}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	metadata := metadataFromProjection(s.manifest, s.next-1, s.projection)
	return metadata
}

// DeriveMessages returns the model history projection.
func (s *Session) DeriveMessages() []provider.Message {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	messages := detachMessages(s.projection.ModelMessages)
	s.mu.Unlock()
	return messages
}

func (s *Session) cacheWeight() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	weight := int64(64 << 10)
	for _, message := range s.projection.ModelMessages {
		weight += int64(len(message.ID) + len(message.Content) + len(message.RawContent) + len(message.ProviderContent) + len(message.ReasoningContent) + len(message.ReasoningSignature) + len(message.Original))
		for _, image := range message.Images {
			weight += int64(len(image))
		}
		for _, call := range message.ToolCalls {
			weight += int64(len(call.ID) + len(call.Name) + len(call.Arguments) + len(call.Diff))
		}
		for _, item := range message.ResponsesItems {
			weight += int64(len(item))
		}
		for _, block := range message.ThinkingBlocks {
			encoded, _ := json.Marshal(block)
			weight += int64(len(encoded))
		}
	}
	weight += int64(len(s.projection.PlanState) + len(s.projection.GoalState))
	return weight
}

// RecentSnapshot returns the bounded chat baseline without consulting the
// history locator or search index.
func (s *Session) RecentSnapshot() RecentSnapshot {
	if s == nil {
		return RecentSnapshot{}
	}
	durable := uint64(0)
	if s.binding != nil {
		durable = s.binding.durableSequence()
	}
	s.mu.Lock()
	messages := detachMessages(s.durableRecent)
	sessionDir := ""
	if s.binding != nil {
		sessionDir = s.binding.dir
	}
	snapshot := RecentSnapshot{
		Version: recoveryFormatVersion, SessionID: s.id, StorageGeneration: s.storageGeneration,
		DurableSequence: durable,
		Title:           s.projection.Title, ModelRef: s.projection.ModelRef, ModelIdentity: s.projection.ModelIdentity,
		TotalTurns: visibleBoundaryCount(s.projection, false),
	}
	s.mu.Unlock()
	if sessionDir != "" {
		snapshot.Entries, _ = buildRecentEntries(context.Background(), sessionDir, messages, durable, snapshot.TotalTurns)
	}
	return snapshot
}

func materializeSnapshotMessages(history eventPageReader, accepted []Commit, acceptedSequence uint64) ([]provider.Message, error) {
	projection, _ := Project(nil)
	var cursor uint64
	if history != nil {
		for {
			startCursor := cursor
			page, err := history.Read(context.Background(), cursor, 1000)
			if err != nil {
				return nil, err
			}
			for _, commit := range page.Commits {
				if commit.LastSequence() > acceptedSequence {
					break
				}
				if err := applyProjectionCommit(&projection, commit); err != nil {
					return nil, err
				}
				// Only UI messages are requested at this compatibility boundary.
				// Clearing the provider projection after each commit prevents a
				// second cumulative model-history allocation during reconstruction.
				projection.ModelMessages = nil
				cursor = commit.LastSequence()
			}
			if !page.Truncated || cursor >= acceptedSequence {
				break
			}
			if cursor <= startCursor {
				return nil, fmt.Errorf("%w: full snapshot cursor did not advance", ErrDamagedStore)
			}
		}
	}
	for _, commit := range accepted {
		if commit.LastSequence() <= cursor || commit.FirstSequence > acceptedSequence {
			continue
		}
		if err := applyProjectionCommit(&projection, commit); err != nil {
			return nil, err
		}
		projection.ModelMessages = nil
		cursor = commit.LastSequence()
	}
	return projection.Messages, nil
}

// externalizeDurableHistory switches a Service-owned runtime to the bounded
// history model. It is intentionally not used by the low-level Store API,
// whose compatibility callers still request a complete projection.
func (s *Session) externalizeDurableHistory() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.externalHistory {
		return
	}
	for _, message := range s.projection.Messages {
		if s.catalogPreview == "" {
			s.catalogPreview = catalogMessagePreview(message)
		}
	}
	s.projection.Messages = nil
	s.externalHistory = true
}

// AcceptedPage returns the live accepted prefix, including events that have not
// crossed a durability checkpoint yet. SessionHandle.Read on the physical layer
// continues to expose only durable records.
func (s *Session) AcceptedPage(ctx context.Context, offset uint64, limit int) (EventPage, error) {
	if err := ctx.Err(); err != nil {
		return EventPage{}, err
	}
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 1000 {
		return EventPage{}, fmt.Errorf("session: read limit must be 1..1000 commits")
	}
	s.mu.Lock()
	tail := cloneCommits(s.commits)
	var handle SessionHandle
	if s.binding != nil {
		handle = s.binding.handle
	} else {
		handle = s.coldHandle
	}
	s.mu.Unlock()
	page := EventPage{Commits: []Commit{}}
	if handle != nil {
		var err error
		page, err = handle.Read(ctx, offset, limit)
		if err != nil {
			return EventPage{}, err
		}
		if page.Truncated || len(page.Commits) == limit {
			return page, nil
		}
	}
	for _, commit := range tail {
		if commit.LastSequence() <= offset {
			continue
		}
		if len(page.Commits) > 0 && commit.LastSequence() <= page.Commits[len(page.Commits)-1].LastSequence() {
			continue
		}
		if len(page.Commits) == limit {
			page.Truncated = true
			break
		}
		page.Commits = append(page.Commits, cloneCommit(commit))
		page.Next = commit.LastSequence()
	}
	return page, nil
}

// Flush drains the write-behind queue and returns the durable sequence.
func (s *Session) Flush(ctx context.Context) (DurableReceipt, error) {
	if s == nil || s.binding == nil {
		return DurableReceipt{}, ErrReadOnly
	}
	return s.binding.Flush(ctx)
}

// FlushThrough waits only for the captured accepted prefix.
func (s *Session) FlushThrough(ctx context.Context, through uint64) (DurableReceipt, error) {
	if s == nil || s.binding == nil {
		return DurableReceipt{}, ErrReadOnly
	}
	if through > s.EventSequence() {
		return DurableReceipt{}, fmt.Errorf("session: watermark exceeds accepted sequence")
	}
	return s.binding.FlushThrough(ctx, through)
}

// Read exposes the durable prefix through a paged read. Events accepted but not
// yet checkpointed are visible through AcceptedPage instead.
func (s *Session) Read(ctx context.Context, offset uint64, limit int) (EventPage, error) {
	handle := s.Handle()
	if handle == nil {
		return EventPage{}, os.ErrClosed
	}
	return handle.Read(ctx, offset, limit)
}

// Close releases persistence ownership for this session. It is idempotent and
// uncancellable in effect: every caller observes the same close result.
func (s *Session) Close(ctx context.Context) error { return s.close(ctx) }

// Sync forces the physical log to stable storage without draining new work.
func (s *Session) Sync(ctx context.Context) (DurableReceipt, error) {
	handle := s.Handle()
	if handle == nil {
		return DurableReceipt{}, ErrReadOnly
	}
	return handle.Sync(ctx)
}

// newReadSession builds a cold session over a read-only handle. It replays
// nothing: cold callers consume the durable prefix through paged Read, which is
// what keeps catalog and history queries independent of log length.
func newReadSession(handle SessionHandle) *Session {
	manifest := handle.Manifest()
	return &Session{
		id: manifest.SessionID, manifest: manifest, next: 1,
		operations: map[string]operationRecord{}, readOnly: true,
		coldHandle: handle,
	}
}

// Handle exposes the physical persistence handle. Callers must not treat it as
// a business-state owner: reads see only the durable prefix.
func (s *Session) Handle() SessionHandle {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding == nil {
		return s.coldHandle
	}
	return s.binding.handle
}

// WritableHandle exposes the leased physical handle for fork, export, and
// recovery operations that are defined in terms of durable bytes.
func (s *Session) WritableHandle() WritableSessionHandle {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding == nil {
		return nil
	}
	handle, _ := s.binding.handle.(WritableSessionHandle)
	return handle
}

// seal stops accepting new commits. It is the admission boundary that Runtime
// close establishes before the binding is drained.
func (s *Session) seal(err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sealed = true
	s.sealedError = err
	binding := s.binding
	s.mu.Unlock()
	if binding != nil {
		binding.stopAccepting()
	}
}

// close seals the session, drains the binding, and closes the physical handle.
// It is uncancellable and idempotent: every caller observes the same result.
func (s *Session) close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.seal(osClosedError())
	s.mu.Lock()
	binding, cold := s.binding, s.coldHandle
	s.mu.Unlock()
	if binding != nil {
		return binding.Close(ctx)
	}
	if cold != nil {
		// A cold session holds no binding, so its handle is released directly.
		// Leaving it open would leak the read handle for every catalog scan.
		return cold.Close(ctx)
	}
	return nil
}
