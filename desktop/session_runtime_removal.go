package main

import "reasonix/internal/control"

type removedSessionRuntime struct {
	tab           *WorkspaceTab
	ctrl          control.SessionAPI
	sink          *tabEventSink
	sessionDir    string
	sessionPath   string
	scope         string
	workspaceRoot string
	topicID       string
	readOnly      bool
	failedStartup bool
}
