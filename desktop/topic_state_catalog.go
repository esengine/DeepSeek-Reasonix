package main

import "strings"

// ListProjectTopics keeps authoritative topic-state failures visible to the
// Wails caller instead of converting a future-schema or unreadable database
// into an apparently empty sidebar page.
func (a *App) ListProjectTopics(req ProjectTopicPageRequest) (ProjectTopicPage, error) {
	// Remote roots are virtual identities whose sessions come from the Serve
	// catalog. Never build local topic-state or legacy metadata paths from them;
	// their colon is an invalid Windows path component.
	if strings.HasPrefix(strings.TrimSpace(req.WorkspaceRoot), "remote-project:") {
		return ProjectTopicPage{Items: []ProjectNode{}}, nil
	}
	if err := topicStateReadable(topicTitleRoot(req.Scope, req.WorkspaceRoot)); err != nil {
		return ProjectTopicPage{Items: []ProjectNode{}}, err
	}
	return a.listProjectTopics(req)
}
