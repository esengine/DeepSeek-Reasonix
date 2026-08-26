package main

import "reasonix/internal/plugin"

func pluginServerToView(server plugin.ServerStatus) ServerView {
	return ServerView{
		Name: server.Name, Transport: server.Transport, Status: "connected",
		RuntimeState: mcpSessionRuntimeState(server.SessionState),
		Tools:        server.Tools, Prompts: server.Prompts, Resources: server.Resources,
		HasTools: server.HasTools, ToolList: pluginToolsToView(server.ToolList),
		ProtocolVersion: server.ProtocolVersion, SessionState: string(server.SessionState),
		ReconnectAttempts: server.ReconnectAttempts, ErrorKind: string(server.LastErrorKind), Error: server.LastError,
	}
}

func mcpSessionRuntimeState(state plugin.SessionState) string {
	switch state {
	case plugin.SessionStateConnecting, plugin.SessionStateListening, plugin.SessionStateReconnecting:
		return "connecting"
	case plugin.SessionStateFailed:
		return "issue"
	default:
		return "ready"
	}
}
