// Package snapshotcapture converts one already-frozen Controller getter view
// into the transport-neutral pieces of a Remote Session snapshot. It performs
// no Controller calls, filesystem I/O, identity allocation, retention, or wire
// externalization; the Session actor and daemon own those concerns.
package snapshotcapture

import (
	"errors"
	"fmt"
	"math"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/evidence"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/remote/history"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/runtimeservice"
	"reasonix/internal/sessiondisplay"
	"reasonix/internal/sessiontelemetry"
)

const checkpointFilePreviewLimit = 60

// ErrInvalidGetterSnapshot reports an internally inconsistent Controller
// getter or shared telemetry capture. Such a capture must not be published as
// a wire snapshot.
var ErrInvalidGetterSnapshot = errors.New("invalid Controller getter snapshot")

// GetterSnapshot is the explicit, point-in-time value of the Controller getters
// needed by Remote V1 snapshot projection. Callers must collect these values at
// the Session actor barrier; Project never reaches back into the Controller.
type GetterSnapshot struct {
	History []provider.Message
	Todos   []evidence.TodoItem

	UsedTokens   int
	WindowTokens int
	LastUsage    *provider.Usage

	Jobs []jobs.View

	Checkpoints                    []checkpoint.Meta
	CheckpointTurnsByMessageIndex  map[int]int
	CheckpointConversationBoundary map[int]bool
}

// AcceptedTurn describes a Turn that the Session actor has admitted but whose
// Controller goroutine may not yet have appended its canonical user message.
//
// HistoryPrefix is the Controller History snapshot taken at the admission
// barrier. While the Turn is running it is the only canonical history
// authority: Controller may already have appended the composed user message,
// assistant/tool output, or later synthetic user messages, but those live
// events are ordered separately by boundarySeq and must not leak across this
// snapshot boundary. Project publishes this prefix plus one provisional raw
// user message. After TurnDone the caller omits AcceptedTurn and full canonical
// Controller History becomes authoritative again.
//
// UserMessagesBeforeAdmission counts provider.RoleUser messages in that frozen
// prefix, rather than visible history turns. TurnID is validated only as an
// opaque ownership guard; history never exposes it as a message or checkpoint
// identity.
type AcceptedTurn struct {
	TurnID                      protocol.TurnID
	Input                       string
	DisplayText                 string
	HistoryMessageCount         int
	UserMessagesBeforeAdmission int
	HistoryPrefix               []provider.Message
}

// Input combines the actor-frozen getter and telemetry values with immutable
// identity and display inputs. CheckpointIDs must have been allocated and
// reconciled by the Session actor; this package never derives protocol identity
// from turn numbers.
type Input struct {
	Binding       history.Binding
	SessionPath   string
	Displays      sessiondisplay.Map
	Getters       GetterSnapshot
	Telemetry     sessiontelemetry.Snapshot
	CheckpointIDs map[int]protocol.CheckpointID
	AcceptedTurn  *AcceptedTurn
}

// Output contains the complete history capture and the non-runtime views that
// the daemon inserts into a SessionSnapshot before final owner budgeting.
type Output struct {
	History     history.Capture
	Todos       []protocol.TodoItem
	Context     protocol.ContextView
	Jobs        []protocol.JobView
	Checkpoints []protocol.CheckpointView
}

// Project converts an explicit getter snapshot without mutating or retaining
// caller-owned maps and slices.
func Project(input Input) (Output, error) {
	contextView, err := projectContext(input.Getters, input.Telemetry)
	if err != nil {
		return Output{}, err
	}
	todos, err := projectTodos(input.Getters.Todos)
	if err != nil {
		return Output{}, err
	}
	jobViews, err := projectJobs(input.Getters.Jobs)
	if err != nil {
		return Output{}, err
	}
	checkpointViews, currentCheckpointIDs, err := projectCheckpoints(
		input.Getters.Checkpoints,
		input.Getters.CheckpointConversationBoundary,
		input.CheckpointIDs,
	)
	if err != nil {
		return Output{}, err
	}

	messages, acceptedMessageIndex, acceptedDisplay, err := reconcileAcceptedTurn(input.Getters.History, input.AcceptedTurn)
	if err != nil {
		return Output{}, err
	}
	resolveDisplay := sessiondisplay.ResolverFromMap(input.Displays, input.SessionPath)
	metadata := make([]history.MessageMetadata, 0)
	for index, message := range messages {
		if message.Role != provider.RoleUser {
			continue
		}
		display := resolveDisplay(message.Content)
		if index == acceptedMessageIndex {
			display = acceptedDisplay
		}
		metadata = append(metadata, history.MessageMetadata{
			MessageIndex:   index,
			DisplayContent: stringPointer(display),
		})
	}

	messageIndexes := make([]int, 0, len(input.Getters.CheckpointTurnsByMessageIndex))
	for messageIndex := range input.Getters.CheckpointTurnsByMessageIndex {
		messageIndexes = append(messageIndexes, messageIndex)
	}
	sort.Ints(messageIndexes)
	checkpointBindings := make([]history.CheckpointBinding, 0, len(messageIndexes))
	for _, messageIndex := range messageIndexes {
		if messageIndex < 0 || messageIndex >= len(messages) {
			return Output{}, invalid("checkpoint boundary message index %d is out of range", messageIndex)
		}
		if messages[messageIndex].Role != provider.RoleUser {
			return Output{}, invalid("checkpoint boundary message index %d is not a user message", messageIndex)
		}
		turn := input.Getters.CheckpointTurnsByMessageIndex[messageIndex]
		checkpointID, ok := currentCheckpointIDs[turn]
		if !ok {
			return Output{}, invalid("checkpoint boundary turn %d has no current opaque checkpointId", turn)
		}
		checkpointBindings = append(checkpointBindings, history.CheckpointBinding{
			MessageIndex: messageIndex,
			CheckpointID: checkpointID,
		})
	}

	return Output{
		History: history.Capture{
			Binding:     input.Binding,
			Messages:    messages,
			Metadata:    metadata,
			Checkpoints: checkpointBindings,
			// An admitted user Turn is reconciled into canonical Messages above.
			// Supplemental user messages remain forbidden by history.Capture.
			Supplemental: []history.SupplementalMessage{},
		},
		Todos:       todos,
		Context:     contextView,
		Jobs:        jobViews,
		Checkpoints: checkpointViews,
	}, nil
}

// reconcileAcceptedTurn freezes the history authority for Project. A running
// Turn always projects its admission prefix plus one provisional raw user
// message; concurrently produced canonical current-Turn messages are restored
// by live state and are deliberately ignored here.
func reconcileAcceptedTurn(current []provider.Message, accepted *AcceptedTurn) ([]provider.Message, int, string, error) {
	if accepted == nil {
		return cloneProviderMessages(current), -1, "", nil
	}
	if strings.TrimSpace(string(accepted.TurnID)) == "" {
		return nil, -1, "", invalid("accepted turnId must be a non-empty opaque string")
	}
	if accepted.Input == "" {
		return nil, -1, "", invalid("accepted turn input must be non-empty")
	}
	if accepted.HistoryMessageCount < 0 || accepted.UserMessagesBeforeAdmission < 0 {
		return nil, -1, "", invalid("accepted turn admission history and user-message counts must be non-negative")
	}
	if len(accepted.HistoryPrefix) != accepted.HistoryMessageCount {
		return nil, -1, "", invalid(
			"accepted turn admission history count %d does not match frozen prefix length %d",
			accepted.HistoryMessageCount,
			len(accepted.HistoryPrefix),
		)
	}
	userMessages := 0
	for _, message := range accepted.HistoryPrefix {
		if message.Role == provider.RoleUser {
			userMessages++
		}
	}
	if userMessages != accepted.UserMessagesBeforeAdmission {
		return nil, -1, "", invalid(
			"accepted turn admission user-message count %d does not match frozen prefix count %d",
			accepted.UserMessagesBeforeAdmission,
			userMessages,
		)
	}

	display := accepted.DisplayText
	// Match Controller.recordDisplay/sessiondisplay.Record: whitespace-only UI
	// text is absence, not an intentional blank history message.
	if strings.TrimSpace(display) == "" {
		display = control.StripComposePrefixes(accepted.Input)
	}
	messages := cloneProviderMessages(accepted.HistoryPrefix)
	acceptedIndex := len(messages)
	messages = append(messages, provider.Message{Role: provider.RoleUser, Content: accepted.Input})
	return messages, acceptedIndex, display, nil
}

func projectContext(getters GetterSnapshot, telemetry sessiontelemetry.Snapshot) (protocol.ContextView, error) {
	shared, err := runtimeservice.ProjectContext(runtimeservice.ContextSource{
		UsedTokens: getters.UsedTokens, WindowTokens: getters.WindowTokens,
		LastUsage: getters.LastUsage, Telemetry: telemetry,
	})
	if err != nil {
		detail := strings.TrimPrefix(err.Error(), runtimeservice.ErrInvalidStatusProjection.Error()+": ")
		return protocol.ContextView{}, invalid("%s", detail)
	}
	out := protocol.ContextView{
		UsedTokens: shared.UsedTokens, WindowTokens: shared.WindowTokens,
		PromptTokens: shared.PromptTokens, CompletionTokens: shared.CompletionTokens,
		TotalTokens: shared.TotalTokens, ReasoningTokens: shared.ReasoningTokens,
		CacheHitTokens: shared.CacheHitTokens, CacheMissTokens: shared.CacheMissTokens,
		SessionCacheHitTokens:   shared.SessionCacheHitTokens,
		SessionCacheMissTokens:  shared.SessionCacheMissTokens,
		SessionCompletionTokens: shared.SessionCompletionTokens,
		RequestCount:            shared.RequestCount, ElapsedMs: shared.ElapsedMillis,
		SessionCost: shared.SessionCost, SessionCurrency: shared.SessionCurrency,
		Sources: []protocol.UsageSourceView{}, ReadFiles: []protocol.ReadFileRecord{},
	}
	for _, source := range shared.Sources {
		out.Sources = append(out.Sources, protocol.UsageSourceView{
			Source: source.Source, PromptTokens: source.PromptTokens,
			CompletionTokens: source.CompletionTokens, TotalTokens: source.TotalTokens,
			ReasoningTokens: source.ReasoningTokens, CacheHitTokens: source.CacheHitTokens,
			CacheMissTokens: source.CacheMissTokens, RequestCount: source.RequestCount,
			SessionCost: source.SessionCost, SessionCurrency: source.SessionCurrency,
		})
	}
	for _, record := range shared.ReadFiles {
		out.ReadFiles = append(out.ReadFiles, protocol.ReadFileRecord{
			Path: record.Path, Turn: record.Turn, TimeMs: record.TimeMs,
			Offset: record.Offset, Limit: record.Limit, Truncated: record.Truncated,
		})
	}
	return out, nil
}

func projectContextLegacy(getters GetterSnapshot, telemetry sessiontelemetry.Snapshot) (protocol.ContextView, error) {
	if getters.UsedTokens < 0 || getters.WindowTokens < 0 {
		return protocol.ContextView{}, invalid("current context counters must be non-negative")
	}
	view := protocol.ContextView{
		UsedTokens:   getters.UsedTokens,
		WindowTokens: getters.WindowTokens,
		Sources:      []protocol.UsageSourceView{},
		ReadFiles:    []protocol.ReadFileRecord{},
	}
	if usage := getters.LastUsage; usage != nil {
		if usage.PromptTokens < 0 || usage.CompletionTokens < 0 || usage.TotalTokens < 0 ||
			usage.ReasoningTokens < 0 || usage.CacheHitTokens < 0 || usage.CacheMissTokens < 0 {
			return protocol.ContextView{}, invalid("last usage counters must be non-negative")
		}
		view.PromptTokens = usage.PromptTokens
		view.CompletionTokens = usage.CompletionTokens
		view.ReasoningTokens = usage.ReasoningTokens
		view.CacheHitTokens = usage.CacheHitTokens
		view.CacheMissTokens = usage.CacheMissTokens
	}

	usage := telemetry.Usage
	if err := validateUsageStats("Session telemetry", usage); err != nil {
		return protocol.ContextView{}, err
	}
	view.TotalTokens = usage.TotalTokens
	view.SessionCacheHitTokens = usage.CacheHitTokens
	view.SessionCacheMissTokens = usage.CacheMissTokens
	view.SessionCompletionTokens = usage.CompletionTokens
	view.RequestCount = usage.RequestCount
	view.ElapsedMs = usage.ElapsedMs
	view.SessionCost = canonicalCost(usage.SessionCost, usage.SessionCostUsd)
	view.SessionCurrency = usage.SessionCurrency

	sourceNames := make([]string, 0, len(usage.Sources))
	for source := range usage.Sources {
		sourceNames = append(sourceNames, source)
	}
	sort.Strings(sourceNames)
	view.Sources = make([]protocol.UsageSourceView, 0, len(sourceNames))
	for _, source := range sourceNames {
		if err := validateIdentityText("usage source", source, 128); err != nil {
			return protocol.ContextView{}, err
		}
		stats := usage.Sources[source]
		if err := validateUsageSource(source, stats); err != nil {
			return protocol.ContextView{}, err
		}
		view.Sources = append(view.Sources, protocol.UsageSourceView{
			Source:           source,
			PromptTokens:     stats.PromptTokens,
			CompletionTokens: stats.CompletionTokens,
			TotalTokens:      stats.TotalTokens,
			ReasoningTokens:  stats.ReasoningTokens,
			CacheHitTokens:   stats.CacheHitTokens,
			CacheMissTokens:  stats.CacheMissTokens,
			RequestCount:     stats.RequestCount,
			SessionCost:      canonicalCost(stats.SessionCost, stats.SessionCostUsd),
			SessionCurrency:  stats.SessionCurrency,
		})
	}

	view.ReadFiles = make([]protocol.ReadFileRecord, 0, len(telemetry.ReadFiles))
	for index, record := range telemetry.ReadFiles {
		projected, err := projectReadFile(index, record)
		if err != nil {
			return protocol.ContextView{}, err
		}
		view.ReadFiles = append(view.ReadFiles, projected)
	}
	// Prompt/Completion/Reasoning/CacheHit/CacheMiss remain the current-context
	// compatibility fields backed by LastUsage, matching the existing Local
	// workbench contract. All Session-cumulative fields above come exclusively
	// from the shared telemetry snapshot and are never overwritten by LastUsage.
	return view, nil
}

func validateUsageStats(label string, usage sessiontelemetry.UsageStats) error {
	if usage.PromptTokens < 0 || usage.CompletionTokens < 0 || usage.TotalTokens < 0 ||
		usage.ReasoningTokens < 0 || usage.CacheHitTokens < 0 || usage.CacheMissTokens < 0 ||
		usage.RequestCount < 0 || usage.ElapsedMs < 0 {
		return invalid("%s counters must be non-negative", label)
	}
	if err := validateCostsAndCurrency(label, usage.SessionCost, usage.SessionCostUsd, usage.SessionCurrency); err != nil {
		return err
	}
	if usage.ActiveTurnStartedAt != 0 || len(usage.SourceSessionCache) != 0 {
		return invalid("%s contains mutable runtime-only accounting", label)
	}
	return nil
}

func validateUsageSource(source string, stats sessiontelemetry.UsageSourceStats) error {
	if stats.PromptTokens < 0 || stats.CompletionTokens < 0 || stats.TotalTokens < 0 ||
		stats.ReasoningTokens < 0 || stats.CacheHitTokens < 0 || stats.CacheMissTokens < 0 ||
		stats.RequestCount < 0 {
		return invalid("usage source %q counters must be non-negative", source)
	}
	return validateCostsAndCurrency("usage source "+source, stats.SessionCost, stats.SessionCostUsd, stats.SessionCurrency)
}

func validateCostsAndCurrency(label string, cost, compatibilityCost float64, currency string) error {
	if !finiteNonNegative(cost) || !finiteNonNegative(compatibilityCost) {
		return invalid("%s cost must be finite and non-negative", label)
	}
	canonical := canonicalCost(cost, compatibilityCost)
	if currency == "" {
		if canonical != 0 {
			return invalid("%s has cost without a currency", label)
		}
		return nil
	}
	if err := validateIdentityText("currency", currency, 16); err != nil {
		return invalid("%s has invalid currency %q", label, currency)
	}
	return nil
}

func canonicalCost(cost, compatibilityCost float64) float64 {
	if cost == 0 && compatibilityCost > 0 {
		return compatibilityCost
	}
	return cost
}

func finiteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validateIdentityText(label, value string, maxRunes int) error {
	if value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRunes {
		return invalid("%s must be trimmed, non-empty valid UTF-8 within %d characters", label, maxRunes)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return invalid("%s contains a control character", label)
		}
	}
	return nil
}

func projectReadFile(index int, record sessiontelemetry.ReadFileRecord) (protocol.ReadFileRecord, error) {
	if err := validateReadPath(record.Path); err != nil {
		return protocol.ReadFileRecord{}, invalid("read file %d path %q is unsafe: %v", index, record.Path, err)
	}
	if record.Turn < 0 || record.Time < 0 || record.Offset < 0 || record.Limit < 0 {
		return protocol.ReadFileRecord{}, invalid("read file %d turn, time, offset, and limit must be non-negative", index)
	}
	return protocol.ReadFileRecord{
		Path:      record.Path,
		Turn:      record.Turn,
		TimeMs:    record.Time,
		Offset:    positiveInt64(record.Offset),
		Limit:     positiveInt64(record.Limit),
		Truncated: record.Truncated,
	}, nil
}

func validateReadPath(value string) error {
	if err := validateIdentityText("read path", value, 4096); err != nil {
		return err
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "\\") || strings.Contains(value, "\\") || strings.Contains(value, ":") {
		return errors.New("must be a primary-relative POSIX path")
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("must not escape the primary workspace")
	}
	if cleaned != value {
		return errors.New("must already be canonical")
	}
	return nil
}

func positiveInt64(value int) *int64 {
	if value <= 0 {
		return nil
	}
	converted := int64(value)
	return &converted
}

func projectTodos(items []evidence.TodoItem) ([]protocol.TodoItem, error) {
	out := make([]protocol.TodoItem, 0, len(items))
	for index, item := range items {
		var status protocol.TodoStatus
		switch item.Status {
		case string(protocol.TodoPending):
			status = protocol.TodoPending
		case string(protocol.TodoInProgress):
			status = protocol.TodoInProgress
		case string(protocol.TodoCompleted):
			status = protocol.TodoCompleted
		default:
			return nil, invalid("todo %d has unsupported status %q", index, item.Status)
		}
		if item.Level < 0 {
			return nil, invalid("todo %d has negative level", index)
		}
		out = append(out, protocol.TodoItem{
			Content:    stringPointer(item.Content),
			Status:     status,
			ActiveForm: item.ActiveForm,
			Level:      item.Level,
		})
	}
	return out, nil
}

func projectJobs(items []jobs.View) ([]protocol.JobView, error) {
	shared, err := runtimeservice.ProjectJobs(items)
	if err != nil {
		detail := strings.TrimPrefix(err.Error(), runtimeservice.ErrInvalidStatusProjection.Error()+": ")
		return nil, invalid("%s", detail)
	}
	out := make([]protocol.JobView, 0, len(shared))
	for _, item := range shared {
		out = append(out, protocol.JobView{
			ID: protocol.JobID(item.ID), Kind: protocol.JobKind(item.Kind), Label: item.Label,
			Status: protocol.JobStatus(item.Status), StartedAt: item.StartedAtMillis,
		})
	}
	return out, nil
}

func projectJobsLegacy(items []jobs.View) ([]protocol.JobView, error) {
	out := make([]protocol.JobView, 0, len(items))
	for index, item := range items {
		var kind protocol.JobKind
		switch item.Kind {
		case string(protocol.JobBash):
			kind = protocol.JobBash
		case string(protocol.JobTask):
			kind = protocol.JobTask
		default:
			return nil, invalid("job %d has unsupported kind %q", index, item.Kind)
		}
		if item.Status != string(protocol.JobRunning) {
			return nil, invalid("job %d has unsupported status %q", index, item.Status)
		}
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Label) == "" || item.StartedAt < 0 {
			return nil, invalid("job %d has invalid identity, label, or start time", index)
		}
		out = append(out, protocol.JobView{
			ID:        protocol.JobID(item.ID),
			Kind:      kind,
			Label:     item.Label,
			Status:    protocol.JobRunning,
			StartedAt: item.StartedAt,
		})
	}
	return out, nil
}

func projectCheckpoints(
	metas []checkpoint.Meta,
	conversationBoundaries map[int]bool,
	checkpointIDs map[int]protocol.CheckpointID,
) ([]protocol.CheckpointView, map[int]protocol.CheckpointID, error) {
	out := make([]protocol.CheckpointView, 0, len(metas))
	currentIDs := make(map[int]protocol.CheckpointID, len(metas))
	seenIDs := make(map[protocol.CheckpointID]int, len(metas))
	previousTurn := -1
	for index, meta := range metas {
		if meta.Turn < 0 || (index > 0 && meta.Turn <= previousTurn) {
			return nil, nil, invalid("checkpoints are not in strict oldest-first turn order at index %d", index)
		}
		previousTurn = meta.Turn
		checkpointID := checkpointIDs[meta.Turn]
		if strings.TrimSpace(string(checkpointID)) == "" {
			return nil, nil, invalid("checkpoint turn %d has no opaque checkpointId", meta.Turn)
		}
		if otherTurn, duplicate := seenIDs[checkpointID]; duplicate {
			return nil, nil, invalid("checkpoint turns %d and %d share one opaque checkpointId", otherTurn, meta.Turn)
		}
		seenIDs[checkpointID] = meta.Turn
		currentIDs[meta.Turn] = checkpointID

		createdAt := meta.Time.UnixMilli()
		if meta.Time.IsZero() || createdAt < 0 {
			return nil, nil, invalid("checkpoint turn %d has invalid creation time", meta.Turn)
		}
		paths := append([]string{}, meta.Paths...)
		for _, path := range paths {
			if strings.TrimSpace(path) == "" {
				return nil, nil, invalid("checkpoint turn %d has an empty file path", meta.Turn)
			}
		}
		prompt := meta.Prompt
		out = append(out, protocol.CheckpointView{
			CheckpointID:    checkpointID,
			DisplayTurn:     meta.Turn,
			Prompt:          stringPointer(prompt),
			Files:           paths,
			CreatedAtMs:     createdAt,
			CanCode:         len(paths) > 0,
			CanConversation: conversationBoundaries[meta.Turn],
		})
	}

	hasCodeAfter := false
	fileSet := make(map[string]struct{}, len(metas)*2)
	preview := make([]string, 0, checkpointFilePreviewLimit)
	for index := len(out) - 1; index >= 0; index-- {
		if len(out[index].Files) > 0 {
			hasCodeAfter = true
		}
		for _, path := range out[index].Files {
			if _, exists := fileSet[path]; exists {
				continue
			}
			fileSet[path] = struct{}{}
			preview = insertSortedPreview(preview, path, checkpointFilePreviewLimit)
		}
		out[index].CanCode = hasCodeAfter
		out[index].FileCount = len(fileSet)
		out[index].Files = append([]string{}, preview...)
		out[index].FilesTruncated = out[index].FileCount > len(out[index].Files)
	}
	return out, currentIDs, nil
}

func insertSortedPreview(preview []string, path string, limit int) []string {
	index := sort.SearchStrings(preview, path)
	if index < len(preview) && preview[index] == path {
		return preview
	}
	if len(preview) < limit {
		preview = append(preview, "")
		copy(preview[index+1:], preview[index:])
		preview[index] = path
		return preview
	}
	if index >= limit {
		return preview
	}
	copy(preview[index+1:], preview[index:limit-1])
	preview[index] = path
	return preview
}

func cloneProviderMessages(messages []provider.Message) []provider.Message {
	out := append([]provider.Message(nil), messages...)
	for index := range out {
		out[index].Images = append([]string(nil), messages[index].Images...)
		out[index].ToolCalls = append([]provider.ToolCall(nil), messages[index].ToolCalls...)
		out[index].MemoryCitations = append([]provider.MemoryCitation(nil), messages[index].MemoryCitations...)
	}
	return out
}

func stringPointer(value string) *string {
	copyValue := value
	return &copyValue
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidGetterSnapshot, fmt.Sprintf(format, args...))
}
