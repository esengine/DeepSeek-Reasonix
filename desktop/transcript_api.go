package main

import (
	"context"
	"fmt"

	"reasonix/internal/control"
	"reasonix/internal/transcript"
)

// ResumeTranscriptSessionForTab adopts a session without materializing a legacy
// history response. The caller obtains its bounded page from TranscriptSnapshotForTab.
func (a *App) ResumeTranscriptSessionForTab(tabID, path string) (HistorySwitchPhases, error) {
	page, err := a.resumeSessionForTranscript(tabID, path, 0, false)
	if page.Switch == nil {
		return HistorySwitchPhases{}, err
	}
	return *page.Switch, err
}

func (a *App) OpenChannelTranscriptSessionForTab(tabID, path string) (HistorySwitchPhases, error) {
	page, err := a.openChannelSessionForTranscript(tabID, path, 0, false)
	if page.Switch == nil {
		return HistorySwitchPhases{}, err
	}
	return *page.Switch, err
}

func (a *App) transcriptAPIForTab(tabID string) (control.TranscriptProjectionAPI, func() bool, error) {
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if ctrl == nil {
		return nil, nil, a.workspaceNotReadyErr(tab)
	}
	api, ok := ctrl.(control.TranscriptProjectionAPI)
	if !ok {
		return nil, nil, control.ErrTranscriptProjectionUnavailable
	}
	a.mu.RLock()
	bound := tab != nil && a.tabs[tabID] == tab && tab.Ctrl == ctrl
	epoch := a.runtimeEpochForTabLocked(tab)
	a.mu.RUnlock()
	if !bound {
		return nil, nil, fmt.Errorf("runtime changed while binding transcript")
	}
	return api, func() bool {
		a.mu.RLock()
		defer a.mu.RUnlock()
		return a.tabs[tabID] == tab && tab.Ctrl == ctrl && a.runtimeEpochForTabLocked(tab) == epoch
	}, nil
}

func (a *App) TranscriptSnapshotForTab(tabID string, req transcript.PageRequest) (transcript.Snapshot, error) {
	api, current, err := a.transcriptAPIForTab(tabID)
	if err != nil {
		return transcript.Snapshot{}, err
	}
	result, err := api.TranscriptSnapshot(req)
	if !current() {
		return transcript.Snapshot{}, fmt.Errorf("runtime changed while reading transcript")
	}
	return result, err
}

func (a *App) TranscriptFollowForTab(tabID string, req transcript.FollowRequest) (control.TranscriptFollowResponse, error) {
	api, current, err := a.transcriptAPIForTab(tabID)
	if err != nil {
		return control.TranscriptFollowResponse{}, err
	}
	follow, ok := api.(control.TranscriptFollowAPI)
	if !ok {
		return control.TranscriptFollowResponse{}, control.ErrTranscriptProjectionUnavailable
	}
	// Follow is a subscription, not a bounded command. Its 25-second idle
	// poll must not be cancelled by the 15-second command deadline.
	ctx := a.bootContext()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	result, err := follow.TranscriptFollow(ctx, req)
	if !current() {
		if result.Subscription != "" {
			_, _ = follow.TranscriptFollow(context.Background(), transcript.FollowRequest{Subscription: result.Subscription, Close: true})
		}
		return control.TranscriptFollowResponse{}, fmt.Errorf("runtime changed while following transcript")
	}
	return result, err
}

func (a *App) TranscriptPageForTab(tabID string, req transcript.PageRequest) (transcript.Snapshot, error) {
	return a.TranscriptSnapshotForTab(tabID, req)
}

// TranscriptOutlineForTab pages the complete turn index of one snapshot. The
// capability is optional beside the projection, so a controller without it is
// reported as unavailable instead of answering an empty outline.
func (a *App) TranscriptOutlineForTab(tabID string, req transcript.OutlineRequest) (transcript.OutlinePage, error) {
	api, current, err := a.transcriptAPIForTab(tabID)
	if err != nil {
		return transcript.OutlinePage{}, err
	}
	outline, ok := api.(control.TranscriptOutlineAPI)
	if !ok {
		return transcript.OutlinePage{}, control.ErrTranscriptProjectionUnavailable
	}
	result, err := outline.TranscriptOutline(req)
	if !current() {
		return transcript.OutlinePage{}, fmt.Errorf("runtime changed while reading transcript outline")
	}
	return result, err
}

func (a *App) TranscriptContentForTab(tabID string, req transcript.ContentRequest) (transcript.ContentChunk, error) {
	api, current, err := a.transcriptAPIForTab(tabID)
	if err != nil {
		return transcript.ContentChunk{}, err
	}
	result, err := api.TranscriptContent(req)
	if !current() {
		return transcript.ContentChunk{}, fmt.Errorf("runtime changed while reading transcript content")
	}
	return result, err
}

func (a *App) TranscriptReplayForTab(tabID string, req control.TranscriptReplayRequest) (control.TranscriptReplay, error) {
	api, current, err := a.transcriptAPIForTab(tabID)
	if err != nil {
		return control.TranscriptReplay{}, err
	}
	result, err := api.TranscriptReplay(req)
	if !current() {
		return control.TranscriptReplay{}, fmt.Errorf("runtime changed while replaying transcript")
	}
	return result, err
}
