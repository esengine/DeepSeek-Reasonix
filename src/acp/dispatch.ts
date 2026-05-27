/** Map kernel events (model.delta / tool.preparing|intent|result) to ACP session/update notifications. */

import { toolKindFor } from "@reasonix/core-utils";
import type { Event as KernelEvent } from "../core/events.js";
import type { SessionUpdateParams } from "./protocol.js";
import type { AcpServer } from "./server.js";
export { toolKindFor } from "@reasonix/core-utils";
export type { AcpToolKind } from "@reasonix/core-utils";

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
    case "error": {
      emit(server, {
        sessionId,
        update: {
          sessionUpdate: "agent_message_chunk",
          content: { type: "text", text: `\n\n[error] ${ev.message}` },
          metadata: {
            error: {
              name: ev.name ?? "Error",
              message: ev.message,
              code: ev.code,
              phase: ev.phase,
              retryable: ev.retryable,
            },
          },
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
