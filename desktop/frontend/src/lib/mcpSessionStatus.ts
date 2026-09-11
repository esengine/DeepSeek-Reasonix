import type { Translator } from "./i18n";
import type { ServerView } from "./types";

export function mcpSessionStateLabel(server: ServerView, t: Translator, readyLabel: string): string {
	switch (server.sessionState) {
		case "connecting":
		case "listening":
			return t("status.connecting");
		case "reconnecting":
			return `${t("remote.status.reconnecting")} ${server.reconnectAttempts || 1}/5`;
		case "failed":
			return t("caps.failed");
		default:
			return readyLabel;
	}
}

export function mcpSettingsSearchText(server: ServerView, command: string): string {
	return [
		server.name,
		server.transport,
		command,
		server.error,
		server.source,
		server.configSource,
		server.protocolVersion,
		server.sessionState,
		server.errorKind,
		server.managedByPlugin,
		...(server.toolList ?? []).flatMap((tool) => [tool.name, tool.description]),
	].filter(Boolean).join(" ").toLowerCase();
}
