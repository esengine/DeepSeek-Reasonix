package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type Projection struct {
	Submissions          SubmissionIndex
	TranscriptInputs     []transcriptInput
	HiddenTurns          map[string]bool
	RetractedInputs      map[string]string
	CommittedSequence    uint64
	TurnID               string
	TurnStatus           event.TurnStatus
	CurrentTurnStart     uint64
	CurrentTurnStartedAt int64
	// CurrentTurnMessageID is the stable identity of the newest assistant
	// message committed inside the open turn. It becomes the turn's final reply
	// identity when the turn closes.
	CurrentTurnMessageID string
	CurrentAttempts      map[string]bool
	CurrentCalls         map[string]bool
	Turns                []TurnBoundary
	Messages             []provider.Message
	// ModelMessages is the exact provider-visible projection. Canonical Messages
	// remains the complete UI/history transcript; compaction replaces only this
	// view and never deletes the underlying business history.
	ModelMessages   []provider.Message
	ImageIsolations map[string]provider.ImageIsolationDecision `json:"imageIsolations,omitempty"`
	Todos           []event.Todo
	TodoWritten     bool
	Interactions    map[string]string
	ActiveTools     map[string]string
	StartedTools    map[string]bool
	ActiveSteps     map[string]bool
	Recovery        *event.RecoveryStatus
	PlanState       json.RawMessage
	GoalState       json.RawMessage
	Title           string
	// TitleSequence is the sequence of the latest accepted session/title event.
	// It is independent from CommittedSequence so ordinary chat appends do not
	// conflict with a delayed title mutation.
	TitleSequence uint64
	ModelRef      string
	ModelIdentity string
}

type TurnBoundary struct {
	SamplingCount int              `json:"samplingCount,omitempty"`
	ToolCount     int              `json:"toolCount,omitempty"`
	DurationMs    int64            `json:"durationMs,omitempty"`
	TurnID        string           `json:"turnId"`
	StartSequence uint64           `json:"startSequence"`
	EndSequence   uint64           `json:"endSequence"`
	Status        event.TurnStatus `json:"status"`
	// BoundarySequence is the last sequence of the commit that closed this turn.
	// A cut may only land here: a turn end and the state ending with it can share
	// one commit, and a cut inside that commit inherits half an operation.
	BoundarySequence uint64 `json:"boundarySequence"`
	// Availability is fixed from the complete commit that closed the turn. It
	// must not be recomputed from the latest projection: a later commit may
	// resolve authority that the earlier fork prefix would still inherit.
	Availability ForkAvailability `json:"availability"`
	// MessageID is the stable transcript identity of the turn's final reply, empty
	// when the turn committed none. Surfaces match turns to messages through this
	// identity, never through an array position.
	MessageID string `json:"messageId,omitempty"`
}

var ProjectionKinds = map[string]bool{
	"message/complete": true, "message/upsert": true, "message/retract": true, "assistant/attempt": true,
	"tool/call": true, "tool/start": true, "tool/result": true,
	"turn/start": true, "turn/end": true, "step/start": true, "step/end": true,
	"todo/write": true, "interaction/created": true, "interaction/resolved": true,
	"plan/state": true, "goal/state": true, "session/title": true, "session/config": true,
	"model/context-replace": true, "history/replace": true,
	"model/image-isolation": true,
	"compaction":            true, "runtime/recovery": true, "legacy/import": true,
	"diagnostic": true,
}

var PrototypeProjectionKinds = func() map[string]bool {
	kinds := make(map[string]bool, len(ProjectionKinds)+1)
	maps.Copy(kinds, ProjectionKinds)
	kinds["context/replace"] = true
	return kinds
}()

func Project(commits []Commit) (Projection, error) {
	projection := Projection{Todos: []event.Todo{}, Interactions: map[string]string{}, ActiveTools: map[string]string{}}
	for _, commit := range commits {
		if err := applyProjectionCommit(&projection, commit); err != nil {
			return Projection{}, err
		}
	}
	return projection, nil
}

func applyProjectionCommit(projection *Projection, commit Commit) error {
	initializeProjectionMaps(projection)
	return applyProjectionEvents(projection, commit)
}

func initializeProjectionMaps(projection *Projection) {
	if projection.Interactions == nil {
		projection.Interactions = map[string]string{}
	}
	if projection.ActiveTools == nil {
		projection.ActiveTools = map[string]string{}
	}
	if projection.StartedTools == nil {
		projection.StartedTools = map[string]bool{}
	}
	if projection.ActiveSteps == nil {
		projection.ActiveSteps = map[string]bool{}
	}
	if projection.ImageIsolations == nil {
		projection.ImageIsolations = map[string]provider.ImageIsolationDecision{}
	}
}

func applyProjectionEvents(projection *Projection, commit Commit) error {
	closedBefore := len(projection.Turns)
	for _, ev := range commit.Events {
		projection.CommittedSequence = ev.Sequence
		var err error
		switch ev.Kind {
		case "submission/accepted":
			err = projectSubmission(projection, commit, ev)
		case "legacy/import":
			err = projectLegacyImport(projection, commit, ev)
		case "message/complete":
			err = projectMessageComplete(projection, commit, ev)
		case "message/upsert":
			err = projectMessageUpsert(projection, commit, ev)
		case "message/retract":
			err = projectMessageRetract(projection, commit, ev)
		case "assistant/attempt":
			err = projectAssistantAttempt(projection, commit, ev)
		case "history/replace":
			err = projectHistoryReplace(projection, commit, ev)
		case "model/context-replace":
			err = projectModelContextReplace(projection, commit, ev)
		case "model/image-isolation":
			err = projectImageIsolation(projection, commit, ev)
		case "session/title":
			err = projectSessionTitle(projection, commit, ev)
		case "session/config":
			err = projectSessionConfig(projection, commit, ev)
		case "compaction":
			err = projectCompaction(projection, commit, ev)
		case "turn/start":
			err = projectTurnStart(projection, commit, ev)
		case "step/start", "step/end":
			err = projectStepStart(projection, commit, ev)
		case "tool/call":
			err = projectToolCall(projection, commit, ev)
		case "tool/start":
			err = projectToolStart(projection, commit, ev)
		case "tool/result":
			err = projectToolResult(projection, commit, ev)
		case "todo/write":
			err = projectTodoWrite(projection, commit, ev)
		case "interaction/created":
			err = projectInteractionCreated(projection, commit, ev)
		case "interaction/resolved":
			err = projectInteractionResolved(projection, commit, ev)
		case "runtime/recovery":
			err = projectRuntimeRecovery(projection, commit, ev)
		case "plan/state":
			err = projectPlanState(projection, commit, ev)
		case "goal/state":
			err = projectGoalState(projection, commit, ev)
		case "diagnostic":
			err = projectDiagnostic(projection, commit, ev)
		case "turn/end":
			err = projectTurnEnd(projection, commit, ev)
		}
		if err != nil {
			return err
		}
		applyTranscriptMetadata(projection, commit, ev)
	}
	refreshModelImageIsolations(projection)
	// turn/end can be followed by more events in the same atomic commit. Only
	// after the whole commit is projected do we know whether its cut leaves a
	// turn, interaction, or tool authority open.
	for index := closedBefore; index < len(projection.Turns); index++ {
		projection.Turns[index].Availability = forkProjectionAvailability(*projection, projection.Turns[index].BoundarySequence)
	}
	return nil
}

func projectLegacyImport(projection *Projection, commit Commit, ev Event) error {
	var body legacyImportPayload
	if err := strictPayload(ev.Payload, &body); err != nil || body.Messages == nil {
		return damagedPayload(ev, err)
	}
	projection.Messages = append([]provider.Message{}, body.Messages...)
	projection.ModelMessages = append([]provider.Message{}, provider.ModelMessages(body.Messages)...)
	projection.GoalState = cloneRaw(body.Goal)
	projection.ModelRef = strings.TrimSpace(body.ModelRef)
	projection.ModelIdentity = strings.TrimSpace(body.ModelIdentity)
	return nil
}

func projectMessageComplete(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		Message *provider.Message `json:"message"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil || body.Message == nil || body.Message.ID == "" {
		return damagedPayload(ev, err)
	}
	if projectionMessageIndex(projection.Messages, body.Message.ID) >= 0 {
		return damagedPayload(ev, fmt.Errorf("duplicate stable message id %q", body.Message.ID))
	}
	projection.Messages = append(projection.Messages, *body.Message)
	projection.ModelMessages = append(projection.ModelMessages, provider.ModelMessages([]provider.Message{*body.Message})...)
	projection.recordTurnReply(*body.Message)
	return nil
}

// recordTurnReply keeps the open turn's final answer identity. A fork entry
// belongs on the turn's answer, so a trailing tool call, a retried attempt, or a
// host-generated protocol message must not take the anchor away from the text a
// user actually reads.
func (projection *Projection) recordTurnReply(message provider.Message) {
	if projection.TurnID == "" || message.Role != provider.RoleAssistant || message.LocalOnly {
		return
	}
	if strings.TrimSpace(message.RawContent) == "" && strings.TrimSpace(message.Content) == "" {
		return
	}
	projection.CurrentTurnMessageID = message.ID
}

func projectMessageUpsert(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		Message *provider.Message `json:"message"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil || body.Message == nil || body.Message.ID == "" {
		return damagedPayload(ev, err)
	}
	if !replaceProjectionMessage(projection.Messages, *body.Message) {
		projection.Messages = append(projection.Messages, *body.Message)
	}
	visible := provider.ModelMessages([]provider.Message{*body.Message})
	modelIndex := projectionMessageIndex(projection.ModelMessages, body.Message.ID)
	if modelIndex >= 0 {
		if len(visible) == 0 {
			projection.ModelMessages = append(projection.ModelMessages[:modelIndex], projection.ModelMessages[modelIndex+1:]...)
		} else {
			projection.ModelMessages[modelIndex] = visible[0]
		}
	} else if len(visible) > 0 {
		// Upserts are normally metadata changes to an existing message. The
		// append case is retained for explicitly-created records.
		projection.ModelMessages = append(projection.ModelMessages, visible[0])
	}
	if projection.CurrentTurnMessageID == body.Message.ID && (body.Message.LocalOnly || body.Message.Role != provider.RoleAssistant) {
		projection.CurrentTurnMessageID = ""
	}
	projection.recordTurnReply(*body.Message)
	return nil
}

func projectMessageRetract(projection *Projection, commit Commit, ev Event) error {
	ids, err := retractedMessageIDs(ev, ev.Payload)
	if err != nil {
		return err
	}
	removed := make(map[string]bool, len(ids))
	for _, id := range ids {
		removed[id] = true
	}
	filter := func(messages []provider.Message) []provider.Message {
		return slices.DeleteFunc(messages, func(message provider.Message) bool { return removed[message.ID] })
	}
	projection.Messages = filter(projection.Messages)
	projection.ModelMessages = filter(projection.ModelMessages)
	if removed[projection.CurrentTurnMessageID] {
		projection.CurrentTurnMessageID = ""
	}
	return nil
}

func projectAssistantAttempt(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		ID        string `json:"id"`
		MessageID string `json:"messageId,omitempty"`
		Action    string `json:"action"`
		Attempt   int    `json:"attempt,omitempty"`
		Max       int    `json:"max,omitempty"`
		Reason    string `json:"reason,omitempty"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil || body.ID == "" || (body.Action != "begin" && body.Action != "discard" && body.Action != "commit") {
		return damagedPayload(ev, err)
	}
	if projection.TurnID != "" && body.Action == "begin" {
		if projection.CurrentAttempts == nil {
			projection.CurrentAttempts = map[string]bool{}
		}
		projection.CurrentAttempts[body.ID] = true
	}
	return nil
}

func projectHistoryReplace(projection *Projection, commit Commit, ev Event) error {
	var body historyReplacePayload
	if err := strictPayload(ev.Payload, &body); err != nil || body.Messages == nil {
		return damagedPayload(ev, err)
	}
	projection.Messages = append([]provider.Message(nil), body.Messages...)
	projection.ModelMessages = append([]provider.Message(nil), provider.ModelMessages(body.Messages)...)
	return nil
}

func projectModelContextReplace(projection *Projection, commit Commit, ev Event) error {
	var body historyReplacePayload
	if err := strictPayload(ev.Payload, &body); err != nil || body.Messages == nil {
		return damagedPayload(ev, err)
	}
	projection.ModelMessages = append([]provider.Message(nil), body.Messages...)
	return nil
}

func projectSessionTitle(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		Title string `json:"title"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil {
		return damagedPayload(ev, err)
	}
	projection.Title = body.Title
	// Sequence zero is a valid first event, so store the one-based identity;
	// zero remains the durable "no title event yet" revision.
	projection.TitleSequence = ev.Sequence + 1
	return nil
}

func projectSessionConfig(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		ModelRef      string `json:"modelRef"`
		ModelIdentity string `json:"modelIdentity,omitempty"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil || strings.TrimSpace(body.ModelRef) == "" {
		return damagedPayload(ev, err)
	}
	projection.ModelRef = strings.TrimSpace(body.ModelRef)
	projection.ModelIdentity = strings.TrimSpace(body.ModelIdentity)
	return nil
}

func projectCompaction(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		Messages []provider.Message `json:"messages"`
		Trigger  string             `json:"trigger,omitempty"`
		Sources  []uint64           `json:"sourceSequences,omitempty"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil || body.Messages == nil {
		return damagedPayload(ev, err)
	}
	projection.ModelMessages = append([]provider.Message(nil), body.Messages...)
	return nil
}

func projectTurnStart(projection *Projection, commit Commit, ev Event) error {
	projection.TurnID = commit.TurnID
	projection.TurnStatus = event.TurnInProgress
	projection.CurrentTurnStart = ev.Sequence
	projection.CurrentTurnStartedAt = commit.CreatedAt.UnixMilli()
	projection.CurrentTurnMessageID = ""
	projection.CurrentAttempts, projection.CurrentCalls = map[string]bool{}, map[string]bool{}
	projection.Todos, projection.TodoWritten = []event.Todo{}, false
	projection.Recovery = nil
	return nil
}

func projectStepStart(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		ID     string `json:"id"`
		Status string `json:"status,omitempty"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil || body.ID == "" {
		return damagedPayload(ev, err)
	}
	if ev.Kind == "step/start" {
		projection.ActiveSteps[body.ID] = true
	} else {
		delete(projection.ActiveSteps, body.ID)
	}
	return nil
}

func projectToolCall(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		ID                string                   `json:"id"`
		Name              string                   `json:"name"`
		Args              string                   `json:"args,omitempty"`
		RunState          provider.ToolRunState    `json:"runState,omitempty"`
		Diagnostic        json.RawMessage          `json:"diagnostic,omitempty"`
		ResolvedName      string                   `json:"resolvedName,omitempty"`
		CapabilityID      string                   `json:"capabilityId,omitempty"`
		ReadOnly          bool                     `json:"readOnly,omitempty"`
		Truncated         bool                     `json:"truncated,omitempty"`
		DurationMs        int64                    `json:"durationMs,omitempty"`
		StartedAt         int64                    `json:"startedAt,omitempty"`
		EndedAt           int64                    `json:"endedAt,omitempty"`
		Partial           bool                     `json:"partial,omitempty"`
		ArgChars          int                      `json:"argChars,omitempty"`
		Refreshed         bool                     `json:"refreshed,omitempty"`
		ParentID          string                   `json:"parentId,omitempty"`
		AttemptID         string                   `json:"attemptId,omitempty"`
		SubagentRef       string                   `json:"subagentRef,omitempty"`
		SubagentStatus    string                   `json:"subagentStatus,omitempty"`
		SubagentErrorCode string                   `json:"subagentErrorCode,omitempty"`
		SubagentRetryable bool                     `json:"subagentRetryable,omitempty"`
		Diff              string                   `json:"diff,omitempty"`
		Added             int                      `json:"added,omitempty"`
		Removed           int                      `json:"removed,omitempty"`
		Profile           json.RawMessage          `json:"profile,omitempty"`
		Execution         json.RawMessage          `json:"execution,omitempty"`
		PresentedFiles    []provider.PresentedFile `json:"presentedFiles,omitempty"`
		WorkspaceMutation bool                     `json:"workspaceMutation,omitempty"`
		WorkspacePaths    []string                 `json:"workspacePaths,omitempty"`
		WorkspaceAllPaths bool                     `json:"workspaceAllPaths,omitempty"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil || body.ID == "" || body.Name == "" {
		return damagedPayload(ev, err)
	}
	projection.ActiveTools[body.ID] = body.Name
	if projection.TurnID != "" {
		if projection.CurrentCalls == nil {
			projection.CurrentCalls = map[string]bool{}
		}
		projection.CurrentCalls[body.ID] = true
	}
	return nil
}

func projectToolStart(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil || body.ID == "" || body.Name == "" {
		return damagedPayload(ev, err)
	}
	projection.ActiveTools[body.ID] = body.Name
	projection.StartedTools[body.ID] = true
	return nil
}

func projectToolResult(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		ID                string                   `json:"id"`
		Name              string                   `json:"name"`
		Args              string                   `json:"args,omitempty"`
		Error             string                   `json:"error,omitempty"`
		Output            string                   `json:"output,omitempty"`
		State             string                   `json:"state,omitempty"`
		RunState          provider.ToolRunState    `json:"runState,omitempty"`
		Diagnostic        json.RawMessage          `json:"diagnostic,omitempty"`
		ResolvedName      string                   `json:"resolvedName,omitempty"`
		CapabilityID      string                   `json:"capabilityId,omitempty"`
		ReadOnly          bool                     `json:"readOnly,omitempty"`
		Truncated         bool                     `json:"truncated,omitempty"`
		DurationMs        int64                    `json:"durationMs,omitempty"`
		StartedAt         int64                    `json:"startedAt,omitempty"`
		EndedAt           int64                    `json:"endedAt,omitempty"`
		Partial           bool                     `json:"partial,omitempty"`
		ArgChars          int                      `json:"argChars,omitempty"`
		Refreshed         bool                     `json:"refreshed,omitempty"`
		ParentID          string                   `json:"parentId,omitempty"`
		AttemptID         string                   `json:"attemptId,omitempty"`
		SubagentRef       string                   `json:"subagentRef,omitempty"`
		SubagentStatus    string                   `json:"subagentStatus,omitempty"`
		SubagentErrorCode string                   `json:"subagentErrorCode,omitempty"`
		SubagentRetryable bool                     `json:"subagentRetryable,omitempty"`
		Diff              string                   `json:"diff,omitempty"`
		Added             int                      `json:"added,omitempty"`
		Removed           int                      `json:"removed,omitempty"`
		Profile           json.RawMessage          `json:"profile,omitempty"`
		Execution         json.RawMessage          `json:"execution,omitempty"`
		PresentedFiles    []provider.PresentedFile `json:"presentedFiles,omitempty"`
		Todos             []event.Todo             `json:"todos,omitempty"`
		TodoWritten       bool                     `json:"todoWritten,omitempty"`
		WorkspaceMutation bool                     `json:"workspaceMutation,omitempty"`
		WorkspacePaths    []string                 `json:"workspacePaths,omitempty"`
		WorkspaceAllPaths bool                     `json:"workspaceAllPaths,omitempty"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil || body.ID == "" || body.Name == "" {
		return damagedPayload(ev, err)
	}
	delete(projection.ActiveTools, body.ID)
	delete(projection.StartedTools, body.ID)
	return nil
}

func projectTodoWrite(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		Todos []event.Todo `json:"todos"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil || validateTodos(body.Todos) != nil {
		return damagedPayload(ev, err)
	}
	projection.Todos, projection.TodoWritten = append([]event.Todo(nil), body.Todos...), true
	return nil
}

func projectInteractionCreated(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		ID           string `json:"id"`
		ToolCallID   string `json:"toolCallId,omitempty"`
		Kind         string `json:"kind,omitempty"`
		State        string `json:"state,omitempty"`
		SessionID    string `json:"sessionId,omitempty"`
		HeadID       string `json:"headId,omitempty"`
		TurnID       string `json:"turnId,omitempty"`
		RuntimeEpoch string `json:"runtimeEpoch,omitempty"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil || body.ID == "" || (body.State != "" && body.State != "pending") {
		return damagedPayload(ev, err)
	}
	projection.Interactions[body.ID] = "pending"
	return nil
}

func projectInteractionResolved(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil || body.ID == "" || !terminalInteractionState(body.State) {
		return damagedPayload(ev, err)
	}
	delete(projection.Interactions, body.ID)
	return nil
}

func projectRuntimeRecovery(projection *Projection, commit Commit, ev Event) error {
	var body event.RecoveryStatus
	if err := strictPayload(ev.Payload, &body); err != nil {
		return damagedPayload(ev, err)
	}
	projection.Recovery = &body
	return nil
}

func projectPlanState(projection *Projection, commit Commit, ev Event) error {
	if !validJSONObject(ev.Payload) {
		return damagedPayload(ev, nil)
	}
	projection.PlanState = cloneRaw(ev.Payload)
	return nil
}

func projectGoalState(projection *Projection, commit Commit, ev Event) error {
	if !validJSONObject(ev.Payload) {
		return damagedPayload(ev, nil)
	}
	projection.GoalState = cloneRaw(ev.Payload)
	return nil
}

func projectDiagnostic(projection *Projection, commit Commit, ev Event) error {
	if len(ev.Payload) > 0 && !json.Valid(ev.Payload) {
		return damagedPayload(ev, nil)
	}
	return nil
}

func projectTurnEnd(projection *Projection, commit Commit, ev Event) error {
	var body struct {
		Status event.TurnStatus `json:"status"`
	}
	if err := strictPayload(ev.Payload, &body); err != nil || !body.Status.Terminal() {
		return damagedPayload(ev, err)
	}
	if projection.TurnID != "" && projection.CurrentTurnStart != 0 {
		projection.Turns = append(projection.Turns, TurnBoundary{
			SamplingCount: len(projection.CurrentAttempts), ToolCount: len(projection.CurrentCalls),
			DurationMs: max(0, commit.CreatedAt.UnixMilli()-projection.CurrentTurnStartedAt),
			TurnID:     projection.TurnID, StartSequence: projection.CurrentTurnStart,
			EndSequence: ev.Sequence, Status: body.Status,
			BoundarySequence: commit.LastSequence(),
			MessageID:        projection.CurrentTurnMessageID,
		})
	}
	projection.TurnID = ""
	projection.CurrentTurnStart = 0
	projection.CurrentTurnStartedAt = 0
	projection.CurrentTurnMessageID = ""
	projection.TurnStatus = body.Status
	return nil
}

func replaceProjectionMessage(messages []provider.Message, replacement provider.Message) bool {
	if index := projectionMessageIndex(messages, replacement.ID); index >= 0 {
		messages[index] = replacement
		return true
	}
	return false
}

func projectionMessageIndex(messages []provider.Message, id string) int {
	for i := range messages {
		if messages[i].ID == id {
			return i
		}
	}
	return -1
}

func cloneProjection(projection Projection) Projection {
	projection.TranscriptInputs = append([]transcriptInput(nil), projection.TranscriptInputs...)
	projection.HiddenTurns = maps.Clone(projection.HiddenTurns)
	projection.RetractedInputs = maps.Clone(projection.RetractedInputs)
	projection.CurrentAttempts = maps.Clone(projection.CurrentAttempts)
	projection.CurrentCalls = maps.Clone(projection.CurrentCalls)
	projection.Messages = append([]provider.Message(nil), projection.Messages...)
	for i := range projection.Messages {
		projection.Messages[i].ImageIsolations = append([]provider.ImageIsolationDecision(nil), projection.Messages[i].ImageIsolations...)
	}
	projection.ModelMessages = append([]provider.Message(nil), projection.ModelMessages...)
	for i := range projection.ModelMessages {
		projection.ModelMessages[i].ImageIsolations = append([]provider.ImageIsolationDecision(nil), projection.ModelMessages[i].ImageIsolations...)
	}
	projection.ImageIsolations = maps.Clone(projection.ImageIsolations)
	projection.Turns = append([]TurnBoundary(nil), projection.Turns...)
	projection.Todos = append([]event.Todo(nil), projection.Todos...)
	interactions := make(map[string]string, len(projection.Interactions))
	maps.Copy(interactions, projection.Interactions)
	projection.Interactions = interactions
	tools := make(map[string]string, len(projection.ActiveTools))
	maps.Copy(tools, projection.ActiveTools)
	projection.ActiveTools = tools
	projection.StartedTools = maps.Clone(projection.StartedTools)
	projection.ActiveSteps = maps.Clone(projection.ActiveSteps)
	if projection.Recovery != nil {
		recovery := *projection.Recovery
		projection.Recovery = &recovery
	}
	projection.PlanState = cloneRaw(projection.PlanState)
	projection.GoalState = cloneRaw(projection.GoalState)
	return projection
}

func strictPayload(payload json.RawMessage, target any) error {
	if len(payload) == 0 {
		return io.ErrUnexpectedEOF
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateTodos(todos []event.Todo) error {
	if todos == nil {
		return fmt.Errorf("todos must be an array")
	}
	seen := make(map[string]bool, len(todos))
	for i, todo := range todos {
		content := strings.TrimSpace(todo.Content)
		if content == "" || content != todo.Content || seen[content] {
			return fmt.Errorf("todos[%d].content is invalid", i)
		}
		seen[content] = true
		switch todo.Status {
		case "pending", "in_progress", "completed":
		default:
			return fmt.Errorf("todos[%d].status is invalid", i)
		}
	}
	return nil
}

func terminalInteractionState(state string) bool {
	switch state {
	case "answered", "rejected", "cancelled", "unavailable":
		return true
	default:
		return false
	}
}

func validJSONObject(raw json.RawMessage) bool {
	var object map[string]json.RawMessage
	return len(raw) > 0 && json.Unmarshal(raw, &object) == nil && object != nil
}

func damagedPayload(event Event, cause error) error {
	if cause != nil {
		return fmt.Errorf("%w: invalid %s payload at %d: %w", ErrDamagedStore, event.Kind, event.Sequence, cause)
	}
	return fmt.Errorf("%w: invalid %s payload at %d", ErrDamagedStore, event.Kind, event.Sequence)
}

func cloneRaw(raw json.RawMessage) json.RawMessage { return append(json.RawMessage(nil), raw...) }
