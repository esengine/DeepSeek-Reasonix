package snapshotcapture

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/evidence"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/remote/history"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/sessiondisplay"
	"reasonix/internal/sessiontelemetry"
)

func captureBinding() history.Binding {
	return history.Binding{
		SnapshotID:   "snapshot-capture",
		HostEpoch:    "host-capture",
		Target:       protocol.RuntimeTarget{WorkspaceID: "workspace-capture", SessionID: "session-capture"},
		RuntimeEpoch: "runtime-capture",
	}
}

func TestProjectMapsExactGetterFieldsDisplayAndOpaqueCheckpoints(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	canonicalUser := "expanded canonical prompt with referenced file body"
	messages := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: canonicalUser},
		{Role: provider.RoleAssistant, Content: "answer", ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read_file", Arguments: `{}`}}},
		{Role: provider.RoleTool, Content: "body", ToolCallID: "call-1", Name: "read_file"},
	}
	displays := sessiondisplay.Map{
		filepath.Base(sessionPath): {
			sessiondisplay.MessageKey(canonicalUser): "@guide.md explain this",
		},
	}
	paths := make([]string, 61)
	for index := range paths {
		paths[index] = fmt.Sprintf("src/file-%03d.go", 60-index)
	}
	metas := []checkpoint.Meta{
		{Turn: 3, Time: time.UnixMilli(1_700_000_001_000), Prompt: "first", Paths: []string{}},
		{Turn: 7, Time: time.UnixMilli(1_700_000_002_000), Prompt: "second", Paths: paths},
	}
	input := Input{
		Binding:     captureBinding(),
		SessionPath: sessionPath,
		Displays:    displays,
		Telemetry: sessiontelemetry.Snapshot{
			Version: sessiontelemetry.Version,
			ReadFiles: []sessiontelemetry.ReadFileRecord{
				{Path: "docs/guide.md", Turn: 0, Time: 1_700_000_005_000, Limit: 80, Truncated: true},
				{Path: "src/main.go", Turn: 2, Time: 1_700_000_006_000, Offset: 12},
			},
			Usage: sessiontelemetry.UsageStats{
				PromptTokens: 2000, CompletionTokens: 345, TotalTokens: 2345, ReasoningTokens: 123,
				CacheHitTokens: 410, CacheMissTokens: 90, RequestCount: 7, ElapsedMs: 4321,
				SessionCost: 2.75, SessionCurrency: "¥",
				Sources: map[string]sessiontelemetry.UsageSourceStats{
					"subagent": {
						PromptTokens: 300, CompletionTokens: 45, TotalTokens: 345, ReasoningTokens: 20,
						CacheHitTokens: 10, CacheMissTokens: 20, RequestCount: 2,
						SessionCost: .5, SessionCurrency: "$",
					},
					"executor": {
						PromptTokens: 1700, CompletionTokens: 300, TotalTokens: 2000, ReasoningTokens: 103,
						CacheHitTokens: 400, CacheMissTokens: 70, RequestCount: 5,
						SessionCost: 2.25, SessionCurrency: "¥",
					},
				},
			},
		},
		Getters: GetterSnapshot{
			History: messages,
			Todos: []evidence.TodoItem{
				{Content: "queued", Status: "pending"},
				{Content: "working", Status: "in_progress", ActiveForm: "Working", Level: 1},
				{Content: "done", Status: "completed"},
			},
			UsedTokens:   1200,
			WindowTokens: 64000,
			LastUsage: &provider.Usage{
				PromptTokens: 101, CompletionTokens: 29, TotalTokens: 130,
				ReasoningTokens: 11, CacheHitTokens: 70, CacheMissTokens: 31,
			},
			Jobs: []jobs.View{
				{ID: "job-bash", Kind: "bash", Label: "tests", Status: "running", StartedAt: 1_700_000_003_000},
				{ID: "job-task", Kind: "task", Label: "audit", Status: "running", StartedAt: 1_700_000_004_000},
			},
			Checkpoints:                    metas,
			CheckpointTurnsByMessageIndex:  map[int]int{1: 3},
			CheckpointConversationBoundary: map[int]bool{3: true},
		},
		CheckpointIDs: map[int]protocol.CheckpointID{3: "checkpoint-three", 7: "checkpoint-seven"},
	}

	output, err := Project(input)
	if err != nil {
		t.Fatal(err)
	}
	if output.History.Binding != input.Binding || len(output.History.Metadata) != 1 || len(output.History.Checkpoints) != 1 {
		t.Fatalf("history capture = %+v", output.History)
	}
	if display := output.History.Metadata[0].DisplayContent; display == nil || *display != "@guide.md explain this" {
		t.Fatalf("display metadata = %#v", display)
	}
	if binding := output.History.Checkpoints[0]; binding.MessageIndex != 1 || binding.CheckpointID != "checkpoint-three" {
		t.Fatalf("checkpoint binding = %+v", binding)
	}

	if len(output.Todos) != 3 || output.Todos[0].Status != protocol.TodoPending ||
		output.Todos[1].Status != protocol.TodoInProgress || output.Todos[2].Status != protocol.TodoCompleted ||
		output.Todos[1].Content == nil || *output.Todos[1].Content != "working" || output.Todos[1].ActiveForm != "Working" || output.Todos[1].Level != 1 {
		t.Fatalf("todos = %+v", output.Todos)
	}
	contextView := output.Context
	if contextView.UsedTokens != 1200 || contextView.WindowTokens != 64000 ||
		contextView.PromptTokens != 101 || contextView.CompletionTokens != 29 || contextView.ReasoningTokens != 11 ||
		contextView.CacheHitTokens != 70 || contextView.CacheMissTokens != 31 ||
		contextView.TotalTokens != 2345 || contextView.SessionCacheHitTokens != 410 || contextView.SessionCacheMissTokens != 90 ||
		contextView.SessionCompletionTokens != 345 || contextView.RequestCount != 7 || contextView.ElapsedMs != 4321 ||
		contextView.SessionCost != 2.75 || contextView.SessionCurrency != "¥" {
		t.Fatalf("mapped context = %+v", contextView)
	}
	if len(contextView.Sources) != 2 || contextView.Sources[0].Source != "executor" || contextView.Sources[1].Source != "subagent" ||
		contextView.Sources[0].TotalTokens != 2000 || contextView.Sources[1].SessionCurrency != "$" {
		t.Fatalf("source telemetry is not stable and exact: %+v", contextView.Sources)
	}
	if len(contextView.ReadFiles) != 2 || contextView.ReadFiles[0].Path != "docs/guide.md" || contextView.ReadFiles[0].Turn != 0 ||
		contextView.ReadFiles[0].TimeMs != 1_700_000_005_000 || contextView.ReadFiles[0].Offset != nil ||
		contextView.ReadFiles[0].Limit == nil || *contextView.ReadFiles[0].Limit != 80 || !contextView.ReadFiles[0].Truncated ||
		contextView.ReadFiles[1].Offset == nil || *contextView.ReadFiles[1].Offset != 12 || contextView.ReadFiles[1].Limit != nil {
		t.Fatalf("read-file telemetry = %+v", contextView.ReadFiles)
	}
	if len(output.Jobs) != 2 || output.Jobs[0].ID != "job-bash" || output.Jobs[0].Kind != protocol.JobBash ||
		output.Jobs[1].Kind != protocol.JobTask || output.Jobs[1].Status != protocol.JobRunning {
		t.Fatalf("jobs = %+v", output.Jobs)
	}

	if len(output.Checkpoints) != 2 {
		t.Fatalf("checkpoints = %+v", output.Checkpoints)
	}
	first, second := output.Checkpoints[0], output.Checkpoints[1]
	if first.CheckpointID != "checkpoint-three" || first.DisplayTurn != 3 || first.Prompt == nil || *first.Prompt != "first" ||
		first.CreatedAtMs != 1_700_000_001_000 || !first.CanConversation || !first.CanCode ||
		first.FileCount != 61 || len(first.Files) != 60 || !first.FilesTruncated {
		t.Fatalf("first checkpoint = %+v", first)
	}
	if second.CheckpointID != "checkpoint-seven" || second.DisplayTurn != 7 || second.CanConversation || !second.CanCode ||
		second.FileCount != 61 || len(second.Files) != 60 || !second.FilesTruncated || second.Files[0] != "src/file-000.go" {
		t.Fatalf("second checkpoint = %+v", second)
	}

	// Projection owns its output; later caller mutations cannot alter a capture
	// while the daemon is preparing its retained history snapshot.
	messages[1].Content = "mutated"
	messages[2].ToolCalls[0].Name = "mutated"
	metas[1].Paths[0] = "mutated"
	input.Telemetry.ReadFiles[0].Path = "mutated"
	executorSource := input.Telemetry.Usage.Sources["executor"]
	executorSource.TotalTokens = 999999
	input.Telemetry.Usage.Sources["executor"] = executorSource
	if output.History.Messages[1].Content != canonicalUser || output.History.Messages[2].ToolCalls[0].Name != "read_file" ||
		output.Checkpoints[1].Files[0] != "src/file-000.go" || output.Context.ReadFiles[0].Path != "docs/guide.md" ||
		output.Context.Sources[0].TotalTokens != 2000 {
		t.Fatal("projection retained caller-owned message, checkpoint, or telemetry data")
	}

	store, err := history.New(history.Options{SweepInterval: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CaptureSnapshot(output.History); err != nil {
		t.Fatal(err)
	}
	page, err := store.LatestFitting(input.Binding, 1, func(protocol.HistoryPage) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) < 2 || page.Messages[1].Content == nil || *page.Messages[1].Content != "@guide.md explain this" ||
		page.Messages[1].CheckpointID != "checkpoint-three" {
		t.Fatalf("retained history did not round-trip display/checkpoint metadata: %+v", page.Messages)
	}
}

func TestProjectReturnsNonNilEmptyWireCollections(t *testing.T) {
	output, err := Project(Input{Binding: captureBinding()})
	if err != nil {
		t.Fatal(err)
	}
	if output.Todos == nil || output.Jobs == nil || output.Checkpoints == nil ||
		output.Context.Sources == nil || output.Context.ReadFiles == nil || output.History.Supplemental == nil {
		t.Fatalf("nil collection in empty projection: %+v", output)
	}
}

func TestProjectCheckpointWithoutFilesKeepsNonNilWireCollection(t *testing.T) {
	views, _, err := projectCheckpoints(
		[]checkpoint.Meta{{Turn: 0, Time: time.UnixMilli(1_700_000_000_000), Paths: []string{}}},
		map[int]bool{},
		map[int]protocol.CheckpointID{0: "checkpoint-empty"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Files == nil || len(views[0].Files) != 0 {
		t.Fatalf("empty checkpoint files = %#v", views)
	}
}

func TestProjectAcceptedTurnUsesFrozenPrefixAndOneProvisionalUser(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	prefix := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "older turn"},
		{Role: provider.RoleAssistant, Content: "older answer", ToolCalls: []provider.ToolCall{{ID: "old-call", Name: "read_file", Arguments: `{}`}}},
	}
	current := append([]provider.Message(nil), prefix...)
	current = append(current,
		// The Controller may already have composed and appended this Turn. The
		// running snapshot must not publish it or any later current-Turn state.
		provider.Message{Role: provider.RoleUser, Content: "<memory-update>internal</memory-update>\n\nraw composer text"},
		provider.Message{Role: provider.RoleAssistant, Content: "partial answer"},
		provider.Message{Role: provider.RoleTool, Content: "partial tool"},
		provider.Message{Role: provider.RoleUser, Content: "Continue pursuing the active goal"},
	)
	accepted := &AcceptedTurn{
		TurnID:                      "turn-running",
		Input:                       "raw composer text",
		DisplayText:                 "exact visible text  ",
		HistoryMessageCount:         len(prefix),
		UserMessagesBeforeAdmission: 1,
		HistoryPrefix:               prefix,
	}
	output, err := Project(Input{
		Binding:     captureBinding(),
		SessionPath: sessionPath,
		Getters: GetterSnapshot{
			History:                       current,
			Checkpoints:                   []checkpoint.Meta{{Turn: 4, Time: time.UnixMilli(1_700_000_000_000)}},
			CheckpointTurnsByMessageIndex: map[int]int{len(prefix): 4},
		},
		CheckpointIDs: map[int]protocol.CheckpointID{4: "checkpoint-current-turn"},
		AcceptedTurn:  accepted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.History.Messages) != len(prefix)+1 {
		t.Fatalf("history messages = %+v, want frozen prefix plus one provisional user", output.History.Messages)
	}
	provisional := output.History.Messages[len(prefix)]
	if provisional.Role != provider.RoleUser || provisional.Content != accepted.Input {
		t.Fatalf("provisional message = %+v", provisional)
	}
	for _, message := range output.History.Messages {
		if message.Content == "partial answer" || message.Content == "partial tool" || message.Content == "Continue pursuing the active goal" ||
			strings.Contains(message.Content, "memory-update") {
			t.Fatalf("current-Turn canonical/live state crossed snapshot boundary: %+v", output.History.Messages)
		}
	}
	metadata := metadataAt(output.History.Metadata, len(prefix))
	if metadata == nil || metadata.DisplayContent == nil || *metadata.DisplayContent != accepted.DisplayText {
		t.Fatalf("provisional display metadata = %+v", metadata)
	}
	if len(output.History.Checkpoints) != 1 || output.History.Checkpoints[0].MessageIndex != len(prefix) ||
		output.History.Checkpoints[0].CheckpointID != "checkpoint-current-turn" {
		t.Fatalf("checkpoint boundary did not bind provisional user: %+v", output.History.Checkpoints)
	}
	if len(output.History.Supplemental) != 0 {
		t.Fatalf("accepted user was represented as supplemental: %+v", output.History.Supplemental)
	}

	// The retained capture must not alias either the accepted prefix or the
	// concurrently sampled Controller History.
	prefix[1].Content = "mutated prefix"
	prefix[2].ToolCalls[0].Name = "mutated tool"
	current[0].Content = "mutated current"
	accepted.HistoryPrefix[0].Content = "mutated accepted"
	if output.History.Messages[0].Content != "system" || output.History.Messages[1].Content != "older turn" ||
		output.History.Messages[2].ToolCalls[0].Name != "read_file" {
		t.Fatalf("accepted history output aliases caller state: %+v", output.History.Messages)
	}
}

func TestProjectAcceptedTurnDoesNotDeduplicateSameTextOldTurn(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "same-text.jsonl")
	const repeated = "same prompt"
	prefix := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: repeated},
		{Role: provider.RoleAssistant, Content: "old answer"},
	}
	output, err := Project(Input{
		Binding:     captureBinding(),
		SessionPath: sessionPath,
		Displays: sessiondisplay.Map{
			filepath.Base(sessionPath): {sessiondisplay.MessageKey(repeated): "old visible text"},
		},
		Getters: GetterSnapshot{History: append(append([]provider.Message(nil), prefix...), provider.Message{
			Role: provider.RoleUser, Content: repeated,
		})},
		AcceptedTurn: &AcceptedTurn{
			TurnID:                      "turn-repeated",
			Input:                       repeated,
			DisplayText:                 "new visible text",
			HistoryMessageCount:         len(prefix),
			UserMessagesBeforeAdmission: 1,
			HistoryPrefix:               prefix,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.History.Messages) != 4 || output.History.Messages[1].Content != repeated || output.History.Messages[3].Content != repeated {
		t.Fatalf("same-text turns were deduplicated: %+v", output.History.Messages)
	}
	if len(output.History.Checkpoints) != 0 || len(output.Checkpoints) != 0 || len(output.History.Supplemental) != 0 {
		t.Fatalf("accepted turn manufactured checkpoint or supplemental state: history=%+v checkpoints=%+v", output.History, output.Checkpoints)
	}
	oldMetadata := metadataAt(output.History.Metadata, 1)
	newMetadata := metadataAt(output.History.Metadata, 3)
	if oldMetadata == nil || oldMetadata.DisplayContent == nil || *oldMetadata.DisplayContent != "old visible text" ||
		newMetadata == nil || newMetadata.DisplayContent == nil || *newMetadata.DisplayContent != "new visible text" {
		t.Fatalf("same-text display metadata = old:%+v new:%+v", oldMetadata, newMetadata)
	}

	store, err := history.New(history.Options{SweepInterval: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CaptureSnapshot(output.History); err != nil {
		t.Fatal(err)
	}
	page, err := store.LatestFitting(output.History.Binding, 10, func(protocol.HistoryPage) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalTurns != 2 || len(page.Messages) != 4 || page.Messages[1].Content == nil || *page.Messages[1].Content != "old visible text" ||
		page.Messages[3].Content == nil || *page.Messages[3].Content != "new visible text" {
		t.Fatalf("same-text retained page = %+v", page)
	}
}

func TestProjectAcceptedTurnEmptyDisplayUsesComposeFallback(t *testing.T) {
	composed := control.PlanModeMarker + "\n\nship the change"
	output, err := Project(Input{
		Binding: captureBinding(),
		AcceptedTurn: &AcceptedTurn{
			TurnID:              "turn-display-fallback",
			Input:               composed,
			HistoryMessageCount: 0,
			HistoryPrefix:       []provider.Message{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	metadata := metadataAt(output.History.Metadata, 0)
	if metadata == nil || metadata.DisplayContent == nil || *metadata.DisplayContent != "ship the change" {
		t.Fatalf("fallback display metadata = %+v", metadata)
	}
}

func TestProjectAcceptedTurnWhitespaceDisplayUsesComposeFallback(t *testing.T) {
	input := control.PlanModeMarker + "\n\nvisible prompt"
	output, err := Project(Input{
		Binding: captureBinding(),
		AcceptedTurn: &AcceptedTurn{
			TurnID:              "turn-running",
			Input:               input,
			DisplayText:         " \t\n",
			HistoryPrefix:       []provider.Message{},
			HistoryMessageCount: 0,
		},
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if len(output.History.Metadata) != 1 || output.History.Metadata[0].DisplayContent == nil ||
		*output.History.Metadata[0].DisplayContent != "visible prompt" {
		t.Fatalf("whitespace display metadata = %+v", output.History.Metadata)
	}
}

func TestProjectRejectsInvalidAcceptedTurnAdmissionPrefix(t *testing.T) {
	userPrefix := []provider.Message{{Role: provider.RoleUser, Content: "old"}}
	tests := []struct {
		name     string
		accepted AcceptedTurn
	}{
		{name: "missing turn id", accepted: AcceptedTurn{Input: "new"}},
		{name: "empty input", accepted: AcceptedTurn{TurnID: "turn"}},
		{name: "negative history count", accepted: AcceptedTurn{TurnID: "turn", Input: "new", HistoryMessageCount: -1}},
		{name: "negative user count", accepted: AcceptedTurn{TurnID: "turn", Input: "new", UserMessagesBeforeAdmission: -1}},
		{name: "prefix length mismatch", accepted: AcceptedTurn{TurnID: "turn", Input: "new", HistoryMessageCount: 2, UserMessagesBeforeAdmission: 1, HistoryPrefix: userPrefix}},
		{name: "prefix user count mismatch", accepted: AcceptedTurn{TurnID: "turn", Input: "new", HistoryMessageCount: 1, UserMessagesBeforeAdmission: 0, HistoryPrefix: userPrefix}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Project(Input{Binding: captureBinding(), AcceptedTurn: &test.accepted})
			if !errors.Is(err, ErrInvalidGetterSnapshot) {
				t.Fatalf("error = %v, want ErrInvalidGetterSnapshot", err)
			}
		})
	}
}

func metadataAt(metadata []history.MessageMetadata, messageIndex int) *history.MessageMetadata {
	for index := range metadata {
		if metadata[index].MessageIndex == messageIndex {
			return &metadata[index]
		}
	}
	return nil
}

func TestProjectRejectsInconsistentGetterSnapshots(t *testing.T) {
	validTime := time.UnixMilli(1_700_000_000_000)
	tests := []struct {
		name  string
		input Input
	}{
		{
			name:  "negative context",
			input: Input{Getters: GetterSnapshot{UsedTokens: -1}},
		},
		{
			name:  "invalid todo status",
			input: Input{Getters: GetterSnapshot{Todos: []evidence.TodoItem{{Content: "x", Status: "blocked"}}}},
		},
		{
			name:  "terminal job is not a live job view",
			input: Input{Getters: GetterSnapshot{Jobs: []jobs.View{{ID: "job", Kind: "bash", Label: "x", Status: "done"}}}},
		},
		{
			name:  "missing opaque checkpoint identity",
			input: Input{Getters: GetterSnapshot{Checkpoints: []checkpoint.Meta{{Turn: 1, Time: validTime}}}},
		},
		{
			name: "duplicate opaque checkpoint identity",
			input: Input{
				Getters:       GetterSnapshot{Checkpoints: []checkpoint.Meta{{Turn: 1, Time: validTime}, {Turn: 2, Time: validTime}}},
				CheckpointIDs: map[int]protocol.CheckpointID{1: "same", 2: "same"},
			},
		},
		{
			name: "checkpoint boundary not user message",
			input: Input{
				Getters: GetterSnapshot{
					History:                       []provider.Message{{Role: provider.RoleAssistant, Content: "x"}},
					Checkpoints:                   []checkpoint.Meta{{Turn: 1, Time: validTime}},
					CheckpointTurnsByMessageIndex: map[int]int{0: 1},
				},
				CheckpointIDs: map[int]protocol.CheckpointID{1: "checkpoint-one"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Project(test.input)
			if !errors.Is(err, ErrInvalidGetterSnapshot) {
				t.Fatalf("error = %v, want ErrInvalidGetterSnapshot", err)
			}
		})
	}
}

func TestProjectRejectsUnsafeOrInvalidTelemetry(t *testing.T) {
	base := sessiontelemetry.Snapshot{
		Version:   sessiontelemetry.Version,
		ReadFiles: []sessiontelemetry.ReadFileRecord{{Path: "docs/guide.md", Turn: 0, Time: 1, Offset: 1, Limit: 1}},
		Usage: sessiontelemetry.UsageStats{
			TotalTokens: 1, CacheHitTokens: 1, RequestCount: 1,
			SessionCost: 1, SessionCurrency: "USD",
			Sources: map[string]sessiontelemetry.UsageSourceStats{
				"executor": {TotalTokens: 1, RequestCount: 1, SessionCost: 1, SessionCurrency: "USD"},
			},
		},
	}
	tests := []struct {
		name   string
		mutate func(*sessiontelemetry.Snapshot)
	}{
		{name: "negative total", mutate: func(value *sessiontelemetry.Snapshot) { value.Usage.TotalTokens = -1 }},
		{name: "negative source counter", mutate: func(value *sessiontelemetry.Snapshot) {
			stats := value.Usage.Sources["executor"]
			stats.RequestCount = -1
			value.Usage.Sources["executor"] = stats
		}},
		{name: "NaN cost", mutate: func(value *sessiontelemetry.Snapshot) { value.Usage.SessionCost = math.NaN() }},
		{name: "infinite compatibility cost", mutate: func(value *sessiontelemetry.Snapshot) { value.Usage.SessionCostUsd = math.Inf(1) }},
		{name: "cost without currency", mutate: func(value *sessiontelemetry.Snapshot) { value.Usage.SessionCurrency = "" }},
		{name: "untrimmed currency", mutate: func(value *sessiontelemetry.Snapshot) { value.Usage.SessionCurrency = " USD " }},
		{name: "currency control character", mutate: func(value *sessiontelemetry.Snapshot) { value.Usage.SessionCurrency = "US\nD" }},
		{name: "currency too long", mutate: func(value *sessiontelemetry.Snapshot) { value.Usage.SessionCurrency = "currency-identifier-too-long" }},
		{name: "empty source", mutate: func(value *sessiontelemetry.Snapshot) {
			stats := value.Usage.Sources["executor"]
			delete(value.Usage.Sources, "executor")
			value.Usage.Sources[""] = stats
		}},
		{name: "runtime-only active turn", mutate: func(value *sessiontelemetry.Snapshot) { value.Usage.ActiveTurnStartedAt = 1 }},
		{name: "absolute POSIX path", mutate: func(value *sessiontelemetry.Snapshot) { value.ReadFiles[0].Path = "/etc/passwd" }},
		{name: "absolute Windows path", mutate: func(value *sessiontelemetry.Snapshot) { value.ReadFiles[0].Path = `C:\\Users\\secret.txt` }},
		{name: "parent escape", mutate: func(value *sessiontelemetry.Snapshot) { value.ReadFiles[0].Path = "../secret.txt" }},
		{name: "noncanonical parent segment", mutate: func(value *sessiontelemetry.Snapshot) { value.ReadFiles[0].Path = "docs/../secret.txt" }},
		{name: "noncanonical dot segment", mutate: func(value *sessiontelemetry.Snapshot) { value.ReadFiles[0].Path = "./guide.md" }},
		{name: "negative turn", mutate: func(value *sessiontelemetry.Snapshot) { value.ReadFiles[0].Turn = -1 }},
		{name: "negative time", mutate: func(value *sessiontelemetry.Snapshot) { value.ReadFiles[0].Time = -1 }},
		{name: "negative offset", mutate: func(value *sessiontelemetry.Snapshot) { value.ReadFiles[0].Offset = -1 }},
		{name: "negative limit", mutate: func(value *sessiontelemetry.Snapshot) { value.ReadFiles[0].Limit = -1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			telemetry := base.Clone()
			test.mutate(&telemetry)
			_, err := Project(Input{Binding: captureBinding(), Telemetry: telemetry})
			if !errors.Is(err, ErrInvalidGetterSnapshot) {
				t.Fatalf("error = %v, want ErrInvalidGetterSnapshot", err)
			}
		})
	}
}

func TestProjectUsesDeprecatedTelemetryCostOnlyAsFallback(t *testing.T) {
	output, err := Project(Input{
		Binding: captureBinding(),
		Telemetry: sessiontelemetry.Snapshot{Usage: sessiontelemetry.UsageStats{
			SessionCostUsd:  1.5,
			SessionCurrency: "USD",
			Sources: map[string]sessiontelemetry.UsageSourceStats{
				"executor": {SessionCostUsd: .75, SessionCurrency: "USD"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.Context.SessionCost != 1.5 || len(output.Context.Sources) != 1 || output.Context.Sources[0].SessionCost != .75 {
		t.Fatalf("compatibility cost fallback = %+v", output.Context)
	}
}
