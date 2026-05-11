/** Formats one-liner MCP lifecycle events per `docs/design/agent-tui-terminal.html` §37. */

import { t } from "../../i18n/index.js";

export type McpLifecycleEvent =
  | { state: "handshake"; name: string }
  | {
      state: "connected";
      name: string;
      tools: number;
      resources?: number;
      prompts?: number;
      ms: number;
    }
  | { state: "failed"; name: string; reason: string }
  | { state: "disabled"; name: string }
  | { state: "reconnect"; name: string };

const STATE: Record<McpLifecycleEvent["state"], { glyph: string; label: () => string }> = {
  handshake: { glyph: "↻", label: () => t("mcpLifecycle.handshake") },
  connected: { glyph: "✓", label: () => t("mcpLifecycle.connected") },
  failed: { glyph: "✖", label: () => t("mcpLifecycle.failed") },
  disabled: { glyph: "○", label: () => t("mcpLifecycle.disabled") },
  reconnect: { glyph: "↻", label: () => t("mcpLifecycle.reconnect") },
};

const NAME_COL = 22;
const STATE_COL = 15;

export function formatMcpLifecycleEvent(ev: McpLifecycleEvent): string {
  const { glyph, label } = STATE[ev.state];
  const namePart = `MCP · ${ev.name}`;
  const namePad = " ".repeat(Math.max(1, NAME_COL - namePart.length));
  const stateField = `${glyph} ${label()}`.padEnd(STATE_COL);
  return `⌘ ${namePart}${namePad}${stateField}${describeDetail(ev)}`;
}

function describeDetail(ev: McpLifecycleEvent): string {
  if (ev.state === "handshake") return t("mcpLifecycle.initDetail");
  if (ev.state === "failed") return ev.reason;
  if (ev.state === "disabled") return t("mcpLifecycle.disabledDetail", { name: ev.name });
  if (ev.state === "reconnect") return t("mcpLifecycle.reconnectDetail");
  const parts: string[] = [`${ev.tools} tools`];
  if (ev.resources && ev.resources > 0) parts.push(`${ev.resources} resources`);
  if (ev.prompts && ev.prompts > 0) parts.push(`${ev.prompts} prompts`);
  parts.push(`${ev.ms}ms`);
  return parts.join(" · ");
}
