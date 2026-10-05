package doctor

import (
	"encoding/json"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/usagereport"
)

// isVerificationToolCall reports whether a persisted tool call is a shell
// command whose exit status provides implementation evidence. It intentionally
// returns only a boolean so diagnostic callers never need to expose arguments.
func isVerificationToolCall(name, args string) bool {
	return evidence.IsVerificationToolCall(name, json.RawMessage(args))
}

const qualityReportSchemaVersion = 2

// QualityOptions selects one persisted session and produces a content-free
// diagnostic summary suitable for a public issue or discussion.
type QualityOptions struct {
	Version    string
	SessionRef string
}

type QualityReport struct {
	SchemaVersion int               `json:"schema_version"`
	Version       string            `json:"version"`
	Profile       QualityProfile    `json:"profile"`
	Transcript    QualityTranscript `json:"transcript"`
	Usage         QualityUsage      `json:"usage"`
	Signals       QualitySignals    `json:"signals"`
	Warnings      []string          `json:"warnings,omitempty"`
}

type QualityProfile struct {
	ModelID           string `json:"model_id"`
	RuntimeProfile    string `json:"runtime_profile"`
	CollaborationMode string `json:"collaboration_mode"`
	ToolApprovalMode  string `json:"tool_approval_mode"`
	GoalActive        bool   `json:"goal_active"`
	Recovered         bool   `json:"recovered"`
}

type QualityTranscript struct {
	Messages                      int `json:"messages"`
	UserMessages                  int `json:"user_messages"`
	AssistantMessages             int `json:"assistant_messages"`
	ToolResults                   int `json:"tool_results"`
	ToolCalls                     int `json:"tool_calls"`
	WriterCalls                   int `json:"writer_calls"`
	VerificationCalls             int `json:"verification_calls"`
	ReasoningTurns                int `json:"reasoning_turns"`
	ToolCallTurnsWithoutReasoning int `json:"tool_call_turns_without_reasoning"`
	CompactionSummaries           int `json:"compaction_summaries"`
}

type QualityUsage struct {
	Available        bool `json:"available"`
	Requests         int  `json:"requests"`
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	ReasoningTokens  int  `json:"reasoning_tokens"`
	CacheHitTokens   int  `json:"cache_hit_tokens"`
	CacheMissTokens  int  `json:"cache_miss_tokens"`
	Estimated        bool `json:"estimated,omitempty"`
	CacheHitPercent  *int `json:"cache_hit_percent,omitempty"`
	// ColdStart* describe the session's opening executor request on its own.
	// The session percent above averages over requests that reuse the prefix
	// this one paid for, so it cannot say what a cold start could reuse.
	ColdStartPromptTokens   int  `json:"cold_start_prompt_tokens,omitempty"`
	ColdStartCacheHitTokens int  `json:"cold_start_cache_hit_tokens,omitempty"`
	ColdStartHitPercent     *int `json:"cold_start_cache_hit_percent,omitempty"`
}

type QualitySignals struct {
	ExecutorRequests   int `json:"executor_requests"`
	PlannerRequests    int `json:"planner_requests"`
	SubagentRequests   int `json:"subagent_requests"`
	CompactionRequests int `json:"compaction_requests"`
	ClassifierRequests int `json:"classifier_requests"`
	OtherRequests      int `json:"other_requests"`
}

// CollectQuality reads a session without returning any transcript text, path,
// identifier, tool argument/output, custom model name, or provider endpoint.
func CollectQuality(opts QualityOptions) (QualityReport, error) {
	path, err := resolveSessionBundlePath(strings.TrimSpace(opts.SessionRef))
	if err != nil {
		return QualityReport{}, err
	}
	session, err := sessionstore.LoadSession(path)
	if err != nil {
		return QualityReport{}, err
	}
	meta, _, err := sessionstore.LoadBranchMeta(path)
	if err != nil {
		return QualityReport{}, err
	}

	report := QualityReport{
		SchemaVersion: qualityReportSchemaVersion,
		Version:       strings.TrimSpace(opts.Version),
		Profile: QualityProfile{
			ModelID:           publicModelID(meta.Model),
			RuntimeProfile:    publicTokenMode(meta.TokenMode),
			CollaborationMode: publicCollaborationMode(meta.Mode, meta.Goal),
			ToolApprovalMode:  publicToolApprovalMode(meta.Mode, meta.ToolApprovalMode),
			GoalActive:        strings.TrimSpace(meta.Goal) != "",
			Recovered:         meta.Recovered,
		},
	}
	report.Transcript = summarizeQualityTranscript(session.Snapshot())
	if telemetry, ok := loadQualityTelemetry(path); ok {
		report.Usage = summarizeQualityUsage(telemetry)
		report.Signals = summarizeQualitySignals(telemetry.Usage.Sources, telemetry.Usage.RequestCount)
	} else {
		report.Warnings = append(report.Warnings, "desktop telemetry is unavailable for this session")
	}
	// The session says whether reasoning was expected: a model that produced any
	// should produce it on tool-call turns too. Read from the evidence, this
	// reports the same fault for every vendor and stays quiet for plain models.
	if report.Transcript.ToolCallTurnsWithoutReasoning > 0 &&
		(report.Transcript.ReasoningTurns > 0 || report.Usage.ReasoningTokens > 0) {
		report.Warnings = append(report.Warnings, "tool-call turns have no persisted reasoning content although this session produced reasoning elsewhere")
	}
	if report.Transcript.WriterCalls > 0 && report.Transcript.VerificationCalls == 0 {
		report.Warnings = append(report.Warnings, "writer tools were used without a persisted verification-shaped shell call")
	}
	return report, nil
}

func summarizeQualityTranscript(messages []provider.Message) QualityTranscript {
	var out QualityTranscript
	out.Messages = len(messages)
	for _, message := range messages {
		switch message.Role {
		case provider.RoleUser:
			out.UserMessages++
			if agent.IsCompactionSummary(message) {
				out.CompactionSummaries++
			}
		case provider.RoleAssistant:
			out.AssistantMessages++
			if strings.TrimSpace(message.ReasoningContent) != "" {
				out.ReasoningTurns++
			} else if len(message.ToolCalls) > 0 {
				out.ToolCallTurnsWithoutReasoning++
			}
			for _, call := range message.ToolCalls {
				out.ToolCalls++
				if qualityWriterTool(call.Name) {
					out.WriterCalls++
				}
				if isVerificationToolCall(call.Name, call.Arguments) {
					out.VerificationCalls++
				}
			}
		case provider.RoleTool:
			out.ToolResults++
		}
	}
	return out
}

func qualityWriterTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "edit_file", "multi_edit", "write_file", "move_file", "delete_range", "delete_symbol", "notebook_edit":
		return true
	default:
		return false
	}
}

func loadQualityTelemetry(sessionPath string) (usagereport.Report, bool) {
	report, ok := usagereport.Load(sessionPath)
	if !ok || report.Version <= 0 {
		return usagereport.Report{}, false
	}
	return report, true
}

func summarizeQualityUsage(telemetry usagereport.Report) QualityUsage {
	usage := QualityUsage{
		Available:        true,
		Requests:         telemetry.Usage.RequestCount,
		PromptTokens:     telemetry.Usage.PromptTokens,
		CompletionTokens: telemetry.Usage.CompletionTokens,
		ReasoningTokens:  telemetry.Usage.ReasoningTokens,
		CacheHitTokens:   telemetry.Usage.CacheHitTokens,
		CacheMissTokens:  telemetry.Usage.CacheMissTokens,
		Estimated:        telemetry.Usage.Estimated,
	}
	if total := usage.CacheHitTokens + usage.CacheMissTokens; total > 0 {
		percent := usage.CacheHitTokens * 100 / total
		usage.CacheHitPercent = &percent
	}
	if cold := telemetry.Usage.ColdStart; cold != nil {
		usage.ColdStartPromptTokens = cold.PromptTokens
		usage.ColdStartCacheHitTokens = cold.CacheHitTokens
		if total := cold.CacheHitTokens + cold.CacheMissTokens; total > 0 {
			percent := cold.CacheHitTokens * 100 / total
			usage.ColdStartHitPercent = &percent
		}
	}
	return usage
}

func summarizeQualitySignals(sources map[string]usagereport.Source, total int) QualitySignals {
	var out QualitySignals
	for source, usage := range sources {
		switch strings.ToLower(strings.TrimSpace(source)) {
		case "", "executor":
			out.ExecutorRequests += usage.RequestCount
		case "planner":
			out.PlannerRequests += usage.RequestCount
		case "subagent":
			out.SubagentRequests += usage.RequestCount
		case "compaction":
			out.CompactionRequests += usage.RequestCount
		case "classifier":
			out.ClassifierRequests += usage.RequestCount
		default:
			out.OtherRequests += usage.RequestCount
		}
	}
	accounted := out.ExecutorRequests + out.PlannerRequests + out.SubagentRequests + out.CompactionRequests + out.ClassifierRequests + out.OtherRequests
	if total > accounted {
		out.OtherRequests += total - accounted
	}
	return out
}

// publicModelID returns the session's model id when the curated preset catalog
// declares it. Those ids ship in the binary, so naming one leaks nothing. Any
// other id may be a private deployment name, and the report withholds it
// rather than guessing a vendor out of how it is spelled.
func publicModelID(modelRef string) string {
	ref := strings.TrimSpace(modelRef)
	if ref == "" {
		return "unknown"
	}
	model := ref
	if _, after, ok := strings.Cut(ref, "/"); ok {
		model = strings.TrimSpace(after)
	}
	if config.CuratedModelDeclared(model) {
		return model
	}
	return "custom"
}

func publicTokenMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "economy":
		return "economy"
	case "delivery":
		return "delivery"
	default:
		return "balanced"
	}
}

func publicCollaborationMode(mode, goal string) string {
	if strings.Contains(strings.ToLower(strings.TrimSpace(mode)), "plan") {
		return "plan"
	}
	if strings.TrimSpace(goal) != "" {
		return "goal"
	}
	return "normal"
}

func publicToolApprovalMode(mode, approval string) string {
	switch strings.ToLower(strings.TrimSpace(approval)) {
	case "auto":
		return "auto"
	case "yolo":
		return "yolo"
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(mode)), "yolo") {
		return "yolo"
	}
	return "ask"
}

func RenderQualityText(report QualityReport) string {
	cache := "n/a"
	if report.Usage.CacheHitPercent != nil {
		cache = fmt.Sprintf("%d%%", *report.Usage.CacheHitPercent)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Reasonix quality diagnostics\n")
	fmt.Fprintf(&out, "- version: %s\n", valueOrUnknown(report.Version))
	fmt.Fprintf(&out, "- model: %s\n", report.Profile.ModelID)
	fmt.Fprintf(&out, "- profile: runtime=%s collaboration=%s approval=%s goal=%t recovered=%t\n",
		report.Profile.RuntimeProfile, report.Profile.CollaborationMode, report.Profile.ToolApprovalMode,
		report.Profile.GoalActive, report.Profile.Recovered)
	fmt.Fprintf(&out, "- transcript: messages=%d user=%d assistant=%d tool_results=%d tool_calls=%d writers=%d verification=%d compaction_summaries=%d\n",
		report.Transcript.Messages, report.Transcript.UserMessages, report.Transcript.AssistantMessages,
		report.Transcript.ToolResults, report.Transcript.ToolCalls, report.Transcript.WriterCalls,
		report.Transcript.VerificationCalls, report.Transcript.CompactionSummaries)
	fmt.Fprintf(&out, "- reasoning: tool_call_turns_without_reasoning=%d\n", report.Transcript.ToolCallTurnsWithoutReasoning)
	if report.Usage.Available {
		fmt.Fprintf(&out, "- usage: requests=%d prompt=%d completion=%d reasoning=%d cache_hit=%s\n",
			report.Usage.Requests, report.Usage.PromptTokens, report.Usage.CompletionTokens,
			report.Usage.ReasoningTokens, cache)
		fmt.Fprintf(&out, "- request sources: executor=%d planner=%d subagent=%d compaction=%d classifier=%d other=%d\n",
			report.Signals.ExecutorRequests, report.Signals.PlannerRequests, report.Signals.SubagentRequests,
			report.Signals.CompactionRequests, report.Signals.ClassifierRequests, report.Signals.OtherRequests)
	} else {
		fmt.Fprintf(&out, "- usage: unavailable\n")
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(&out, "- warning: %s\n", warning)
	}
	fmt.Fprintf(&out, "- privacy: transcript text, paths, session identifiers, tool arguments/output, endpoints, and custom model names are omitted\n")
	return out.String()
}

func valueOrUnknown(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "unknown"
}
