package main

// ListProjectTopics keeps authoritative topic-state failures visible to the
// Wails caller instead of converting a future-schema or unreadable database
// into an apparently empty sidebar page.
func (a *App) ListProjectTopics(req ProjectTopicPageRequest) (ProjectTopicPage, error) {
	if err := topicStateReadable(topicTitleRoot(req.Scope, req.WorkspaceRoot)); err != nil {
		return ProjectTopicPage{Items: []ProjectNode{}}, err
	}
	return a.listProjectTopics(req)
}
