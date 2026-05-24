import type { LoopEvent } from "../../../loop.js";
import type { DashboardEvent } from "../../../server/context.js";

export function loopEventToDashboard(
  ev: LoopEvent,
  ctx: { assistantId: string },
): DashboardEvent | null {
  const id = `${ctx.assistantId}-${ev.role}-${Date.now()}`;
  switch (ev.role) {
    case "assistant_delta":
      return {
        kind: "assistant_delta",
        id: ctx.assistantId,
        contentDelta: ev.content || undefined,
        reasoningDelta: ev.reasoningDelta,
      };
    case "tool_start":
      if (!ev.toolName) return null;
      return { kind: "tool_start", id, toolName: ev.toolName, args: ev.toolArgs };
    case "tool":
      if (!ev.toolName) return null;
      return {
        kind: "tool",
        id,
        toolName: ev.toolName,
        content: ev.content,
        args: ev.toolArgs,
      };
    case "warning":
      return { kind: "warning", id, text: ev.content, severity: ev.severity };
    case "error":
      return { kind: "error", id, text: ev.content };
    case "status":
      return { kind: "status", text: ev.content };
    case "steer":
      return { kind: "user", id, text: ev.content };
    default:
      return null;
  }
}
