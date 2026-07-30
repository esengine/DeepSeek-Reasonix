package main

import (
	"path/filepath"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
)

// The native workbench keeps the existing AppBindings surface stable. These
// projections translate the frozen Remote protocol into the same values the
// main window already hydrates for Local sessions; no second HTML application
// or Remote-only transcript implementation is involved.

func workbenchHistoryPage(page protocol.HistoryPage) HistoryPage {
	out := HistoryPage{
		Messages:   make([]HistoryMessage, 0, len(page.Messages)),
		StartTurn:  page.StartTurn,
		EndTurn:    page.EndTurn,
		TotalTurns: page.TotalTurns,
		HasOlder:   page.HasOlder,
	}
	for _, message := range page.Messages {
		projected := HistoryMessage{
			Role: message.Role, Content: workbenchString(message.Content), Detail: workbenchString(message.Detail),
			Code: message.Code, SubmitText: workbenchString(message.SubmitText), CreatedAt: message.CreatedAtMs,
			Reasoning: workbenchString(message.Reasoning), WorkDurationMs: message.WorkDurationMs,
			Level: message.Level, ToolCallID: message.ToolCallID, ToolName: message.ToolName,
			ToolResultArchived: message.ToolResultArchived, ToolResultError: workbenchString(message.ToolResultError),
			Pending: message.Pending, Trigger: message.Trigger, Messages: message.Messages,
			Summary: workbenchString(message.Summary), Archive: workbenchString(message.Archive),
		}
		// Checkpoint IDs are opaque. Display turns come from Snapshot.Checkpoints,
		// so a history message never guesses a numeric turn from the identifier.
		projected.MemoryCitations = make([]provider.MemoryCitation, 0, len(message.MemoryCitations))
		for _, citation := range message.MemoryCitations {
			projected.MemoryCitations = append(projected.MemoryCitations, provider.MemoryCitation{
				ID: citation.ID, Source: citation.Source, LineStart: citation.LineStart,
				LineEnd: citation.LineEnd, Note: citation.Note, Kind: citation.Kind,
			})
		}
		projected.ToolCalls = make([]HistoryToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			projected.ToolCalls = append(projected.ToolCalls, HistoryToolCall{
				ID: call.ID, Name: call.Name, Arguments: workbenchString(call.Arguments),
				ResolvedName: call.ResolvedName, CapabilityID: call.CapabilityID,
				ResolvedReadOnly: call.ResolvedReadOnly,
				Subject:          call.Subject, Summary: workbenchString(call.Summary), Diff: workbenchString(call.Diff),
				Added: call.Added, Removed: call.Removed, ArgumentsArchived: call.ArgumentsArchived,
			})
		}
		out.Messages = append(out.Messages, projected)
	}
	return out
}

func workbenchCheckpointMetas(values []protocol.CheckpointView) []CheckpointMeta {
	out := make([]CheckpointMeta, 0, len(values))
	for _, checkpoint := range values {
		out = append(out, CheckpointMeta{
			Turn: checkpoint.DisplayTurn, Prompt: workbenchString(checkpoint.Prompt),
			Files: append([]string(nil), checkpoint.Files...), FileCount: checkpoint.FileCount,
			FilesTruncated: checkpoint.FilesTruncated, Time: checkpoint.CreatedAtMs,
			CanCode: checkpoint.CanCode, CanConversation: checkpoint.CanConversation,
		})
	}
	return out
}

func workbenchContextInfo(view protocol.ContextView) ContextInfo {
	sources := make(map[string]usageSourceStats, len(view.Sources))
	for _, source := range view.Sources {
		sources[source.Source] = usageSourceStats{
			PromptTokens: source.PromptTokens, CompletionTokens: source.CompletionTokens,
			TotalTokens: source.TotalTokens, ReasoningTokens: source.ReasoningTokens,
			CacheHitTokens: source.CacheHitTokens, CacheMissTokens: source.CacheMissTokens,
			RequestCount: source.RequestCount, SessionCost: source.SessionCost,
			SessionCurrency: source.SessionCurrency,
		}
	}
	return ContextInfo{
		Used: view.UsedTokens, Window: view.WindowTokens, SessionTokens: view.TotalTokens,
		SessionCost: view.SessionCost, SessionCurrency: view.SessionCurrency,
		CacheHitTokens: view.SessionCacheHitTokens, CacheMissTokens: view.SessionCacheMissTokens,
		Sources: sources,
	}
}

func workbenchJobs(values []protocol.JobView) []JobView {
	out := make([]JobView, 0, len(values))
	for _, job := range values {
		out = append(out, JobView{ID: string(job.ID), Kind: string(job.Kind), Label: job.Label, Status: string(job.Status), StartedAt: job.StartedAt})
	}
	return out
}

func workbenchModels(catalog protocol.WorkspaceCatalogResult, current string) []ModelInfo {
	out := make([]ModelInfo, 0, len(catalog.Models))
	for _, model := range catalog.Models {
		out = append(out, ModelInfo{
			Ref: string(model.Ref), Provider: model.Provider, Model: model.Model,
			Current: string(model.Ref) == current,
		})
	}
	return out
}

func workbenchCanonicalTodos(values []protocol.TodoItem) *[]evidence.TodoItem {
	if values == nil {
		return nil
	}
	out := make([]evidence.TodoItem, 0, len(values))
	for _, todo := range values {
		switch todo.Status {
		case protocol.TodoPending, protocol.TodoInProgress, protocol.TodoCompleted:
		default:
			continue
		}
		level := todo.Level
		if level < 0 {
			level = 0
		}
		out = append(out, evidence.TodoItem{
			Content: workbenchString(todo.Content), Status: string(todo.Status),
			ActiveForm: todo.ActiveForm, Level: level,
		})
	}
	return &out
}

func workbenchMeta(snapshot protocol.SessionSnapshot, workspace string) Meta {
	profile := snapshot.Meta.ResolvedProfile
	label := profile.Model
	return Meta{
		Label: label, Ready: true, EventChannel: "agent:event", Cwd: workspace,
		WorkspaceRoot: workspace, WorkspaceName: filepath.Base(filepath.Clean(workspace)), WorkspacePath: workspace,
		AutoApproveTools:  profile.ToolApprovalMode == protocol.ToolApprovalAuto || profile.ToolApprovalMode == protocol.ToolApprovalYOLO,
		Bypass:            profile.ToolApprovalMode == protocol.ToolApprovalYOLO,
		CollaborationMode: string(profile.CollaborationMode), ToolApprovalMode: string(profile.ToolApprovalMode),
		TokenMode: string(profile.TokenMode), Goal: workbenchString(snapshot.Meta.Goal), GoalStatus: string(snapshot.Meta.GoalStatus),
		CanonicalTodos: workbenchCanonicalTodos(snapshot.Todos),
	}
}

func workbenchString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
