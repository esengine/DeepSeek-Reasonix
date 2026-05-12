/** Map kernel events (model.delta / tool.preparing|intent|result) to ACP session/update notifications. */

import type { Event as KernelEvent } from "../core/events.js";
import type { SessionUpdateParams } from "./protocol.js";
import type { AcpServer } from "./server.js";

const READ_TOOLS = new Set([
  "read_file",
  "list_directory",
  "directory_tree",
  "get_file_info",
  "glob",
]);
const EDIT_TOOLS = new Set([
  "write_file",
  "edit_file",
  "multi_edit",
  "create_directory",
  "delete_file",
  "delete_directory",
  "move_file",
  "copy_file",
]);
const SEARCH_TOOLS = new Set(["search_content", "search_files"]);
const EXECUTE_TOOLS = new Set(["run_command", "run_background"]);

export type AcpToolKind = "read" | "edit" | "search" | "execute" | "other";

export function toolKindFor(name: string): AcpToolKind {
  if (READ_TOOLS.has(name)) return "read";
  if (EDIT_TOOLS.has(name)) return "edit";
  if (SEARCH_TOOLS.has(name)) return "search";
  if (EXECUTE_TOOLS.has(name)) return "execute";
  return "other";
}

function tryParseJson(raw: string): unknown {
  if (!raw) return undefined;
  try {
    return JSON.parse(raw);
  } catch {
    return raw;
  }
}

/** Stateless mapping from one kernel event to (zero or more) ACP session/update notifications. */
export function dispatchKernelEvent(server: AcpServer, sessionId: string, ev: KernelEvent): void {
  switch (ev.type) {
    case "model.delta": {
      if (!ev.text) return;
      const variant = ev.channel === "reasoning" ? "agent_thought_chunk" : "agent_message_chunk";
      emit(server, {
        sessionId,
        update: { sessionUpdate: variant, content: { type: "text", text: ev.text } },
      });
      return;
    }
    case "tool.preparing": {
      emit(server, {
        sessionId,
        update: {
          sessionUpdate: "tool_call",
          toolCallId: ev.callId,
          title: ev.name,
          kind: toolKindFor(ev.name),
          status: "pending",
        },
      });
      return;
    }
    case "tool.intent": {
      emit(server, {
        sessionId,
        update: {
          sessionUpdate: "tool_call_update",
          toolCallId: ev.callId,
          status: "in_progress",
        },
      });
      const rawInput = tryParseJson(ev.args);
      if (rawInput !== undefined) {
        emit(server, {
          sessionId,
          update: {
            sessionUpdate: "tool_call",
            toolCallId: ev.callId,
            title: ev.name,
            kind: toolKindFor(ev.name),
            status: "in_progress",
            rawInput,
          },
        });
      }
      return;
    }
    case "tool.result": {
      emit(server, {
        sessionId,
        update: {
          sessionUpdate: "tool_call_update",
          toolCallId: ev.callId,
          status: ev.ok ? "completed" : "failed",
          content: [
            {
              type: "content",
              content: { type: "text", text: clip(ev.output) },
            },
          ],
        },
      });
      return;
    }
    default:
      return;
  }
}

const MAX_RESULT_CHARS = 8000;
function clip(text: string): string {
  if (text.length <= MAX_RESULT_CHARS) return text;
  return `${text.slice(0, MAX_RESULT_CHARS)}\n…(${text.length - MAX_RESULT_CHARS} more chars truncated)`;
}

function emit(server: AcpServer, params: SessionUpdateParams): void {
  server.sendNotification("session/update", params);
}
