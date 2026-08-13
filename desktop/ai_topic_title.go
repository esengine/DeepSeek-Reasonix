package main

import (
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/sessioncatalog"
)

// AI topic-title generation: rename a session from its own conversation using
// the session's active model. The Go side only gathers the bounded transcript
// and applies the result — the model call lives in control.GenerateTopicTitle
// (no tools, no transcript mutation), so this stays cheap and safe.

const (
	// aiTopicTitleMaxTurns caps how many user-authored turns are analyzed.
	aiTopicTitleMaxTurns = 3
	// aiTopicTitleMaxTurnRunes caps each turn sent to the model.
	aiTopicTitleMaxTurnRunes = 500
)

// AiRenameTopic analyzes the topic's own conversation with the session's
// active model and applies the generated title exactly like RenameTopic. It
// returns the new title so the frontend can show it without a reload. The
// topic must have an open controller: the controller owns the provider/model
// configuration the title should be generated with.
func (a *App) AiRenameTopic(topicID string) (string, error) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return "", fmt.Errorf("empty topic id")
	}
	ctrl := a.controllerForTopic(topicID)
	if ctrl == nil {
		return "", fmt.Errorf("topic %q is not open: open the session first to AI-rename it", topicID)
	}
	sessionPath := ctrl.SessionPath()
	if strings.TrimSpace(sessionPath) == "" {
		if scope, workspaceRoot, ok := a.findTopicLocation(topicID); ok {
			sessionPath = a.catalogSessionPathForTopic(scope, workspaceRoot, topicID)
		}
	}
	if strings.TrimSpace(sessionPath) == "" {
		return "", fmt.Errorf("topic %q has no session to analyze", topicID)
	}
	users := topicTitleUserTurnsFromSession(sessionPath)
	if len(users) == 0 {
		return "", fmt.Errorf("topic %q has no user messages to analyze", topicID)
	}
	title, err := ctrl.GenerateTopicTitle(a.ctx, topicTitleTranscript(users))
	if err != nil {
		return "", err
	}
	if err := a.RenameTopic(topicID, title); err != nil {
		return "", err
	}
	return title, nil
}

// controllerForTopic returns the open controller whose tab owns topicID, or
// nil when no open tab matches. Tab controllers are always *control.Controller
// in the desktop app; the assertion documents that contract.
func (a *App) controllerForTopic(topicID string) *control.Controller {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.tabs {
		if tab == nil || strings.TrimSpace(tab.TopicID) != topicID || tab.Ctrl == nil {
			continue
		}
		if ctrl, ok := tab.Ctrl.(*control.Controller); ok {
			return ctrl
		}
	}
	return nil
}

// topicTitleTranscript renders the first few user-authored turns into a
// bounded transcript for the title model.
func topicTitleTranscript(users []string) string {
	parts := make([]string, 0, aiTopicTitleMaxTurns)
	for i, u := range users {
		if i >= aiTopicTitleMaxTurns {
			break
		}
		u = strings.TrimSpace(u)
		if r := []rune(u); len(r) > aiTopicTitleMaxTurnRunes {
			u = string(r[:aiTopicTitleMaxTurnRunes])
		}
		if u != "" {
			parts = append(parts, u)
		}
	}
	return strings.Join(parts, "\n\n")
}

// sessionPreviewForPath returns the sidebar preview (first user message,
// already truncated by the catalog) of a session file, or "" when the file is
// missing or unreadable. Tooltips use this so hovering a topic shows the real
// conversation opener instead of the truncated topic title. It is only called
// for open/runtime sessions (a small bounded set); indexed topics use the
// in-memory catalog helpers below.
func sessionPreviewForPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if meta, ok, err := agent.LoadBranchMeta(path); err == nil && ok {
		if p := strings.TrimSpace(meta.Preview); p != "" {
			return p
		}
	}
	msgs, _, _, err := agent.LoadSessionDisplayMessages(path)
	if err != nil {
		return ""
	}
	preview, _ := agent.SessionPreviewFromMessages(msgs)
	return preview
}

// catalogSessionPreviewForTopic returns the catalog's stored preview for a
// topic's canonical session. Unlike sessionPreviewForPath it never touches
// disk — the catalog is an in-memory projection — so it is safe to call for
// every topic in a sidebar snapshot.
func (a *App) catalogSessionPreviewForTopic(scope, workspaceRoot, topicID string) string {
	path := a.catalogSessionPathForTopic(scope, workspaceRoot, topicID)
	if path == "" {
		return ""
	}
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		return ""
	}
	topic, ok, err := catalog.GetTopic(a.bootContext(), sessioncatalog.TopicKey{Scope: scope, WorkspaceRoot: workspaceRoot, TopicID: topicID})
	if err != nil || !ok {
		return ""
	}
	return topicSessionPreview(topic.Sessions, path)
}

// topicSessionPreview returns the stored preview of the session whose path
// matches, from an in-memory catalog session list.
func topicSessionPreview(sessions []sessioncatalog.SessionRecord, path string) string {
	for _, s := range sessions {
		if s.Path == path {
			return strings.TrimSpace(s.Preview)
		}
	}
	return ""
}
