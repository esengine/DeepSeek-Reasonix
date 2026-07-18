package main

import (
	"fmt"

	"reasonix/internal/runtimeapi"
	"reasonix/internal/sessiontelemetry"
)

func (a *App) remoteContextPanel(tabID string) (ContextPanelInfo, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return emptyRemoteContextPanel(), err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()

	view, err := api.SessionContext(ctx, runtimeapi.SessionContextInput{Session: session.Created.Session})
	if err != nil {
		return emptyRemoteContextPanel(), err
	}
	info := projectRemoteContextPanel(view)

	cursor := runtimeapi.Cursor("")
	seen := map[runtimeapi.Cursor]struct{}{cursor: {}}
	for pages := 1; ; pages++ {
		page, pageErr := api.WorkspaceChanges(ctx, runtimeapi.WorkspaceChangesInput{
			Session: session.Created.Session,
			Cursor:  cursor,
			Limit:   runtimeapi.PageMaxItems,
		})
		if pageErr != nil {
			return info, pageErr
		}
		for _, file := range page.Files {
			sources := make([]string, len(file.Sources))
			for index, source := range file.Sources {
				sources[index] = string(source)
			}
			changed := ChangedFileInfo{
				Path:         file.Path,
				OldPath:      file.OldPath,
				Sources:      sources,
				Turns:        append([]int(nil), file.Turns...),
				GitStatus:    file.GitStatus,
				LatestPrompt: file.LatestPrompt,
			}
			if file.LatestTimeMillis != nil {
				changed.LatestTime = *file.LatestTimeMillis
			}
			info.ChangedFiles = append(info.ChangedFiles, changed)
		}
		next, more, cursorErr := advanceRemoteLegacyCursor("workspace/changes", cursor, page.Next, page.HasMore, seen, pages)
		if cursorErr != nil {
			return info, cursorErr
		}
		if !more {
			return info, nil
		}
		cursor = next
	}
}

func emptyRemoteContextPanel() ContextPanelInfo {
	return ContextPanelInfo{
		Sources:      map[string]usageSourceStats{},
		ReadFiles:    []readFileRecord{},
		ChangedFiles: []ChangedFileInfo{},
	}
}

func projectRemoteContextPanel(view runtimeapi.ContextView) ContextPanelInfo {
	info := emptyRemoteContextPanel()
	info.UsedTokens = view.UsedTokens
	info.WindowTokens = view.WindowTokens
	info.PromptTokens = view.PromptTokens
	info.CompletionTokens = view.CompletionTokens
	info.TotalTokens = view.TotalTokens
	info.ReasoningTokens = view.ReasoningTokens
	info.CacheHitTokens = view.CacheHitTokens
	info.CacheMissTokens = view.CacheMissTokens
	info.SessionCacheHitTokens = view.SessionCacheHitTokens
	info.SessionCacheMissTokens = view.SessionCacheMissTokens
	info.SessionCompletionTokens = view.SessionCompletionTokens
	info.RequestCount = view.RequestCount
	info.ElapsedMs = view.ElapsedMillis
	info.SessionCost = view.SessionCost
	info.SessionCurrency = view.SessionCurrency
	for _, source := range view.Sources {
		info.Sources[source.Source] = usageSourceStats{
			PromptTokens:     source.PromptTokens,
			CompletionTokens: source.CompletionTokens,
			TotalTokens:      source.TotalTokens,
			ReasoningTokens:  source.ReasoningTokens,
			CacheHitTokens:   source.CacheHitTokens,
			CacheMissTokens:  source.CacheMissTokens,
			RequestCount:     source.RequestCount,
			SessionCost:      source.SessionCost,
			SessionCurrency:  source.SessionCurrency,
		}
	}
	for _, record := range view.ReadFiles {
		projected := sessiontelemetry.ReadFileRecord{
			Path:      record.Path,
			Turn:      record.Turn,
			Time:      record.TimeMs,
			Truncated: record.Truncated,
		}
		var conversionErr error
		projected.Offset, conversionErr = remoteInt64ToInt(record.Offset)
		if conversionErr != nil {
			continue
		}
		projected.Limit, conversionErr = remoteInt64ToInt(record.Limit)
		if conversionErr != nil {
			continue
		}
		info.ReadFiles = append(info.ReadFiles, projected)
	}
	return info
}

func remoteInt64ToInt(value *int64) (int, error) {
	if value == nil {
		return 0, nil
	}
	maxInt := int64(^uint(0) >> 1)
	if *value < 0 || *value > maxInt {
		return 0, fmt.Errorf("Remote telemetry integer is outside the Desktop range")
	}
	return int(*value), nil
}
