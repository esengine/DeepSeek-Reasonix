/** LoopEvent → AgentEvent translator. Stateful: tracks reasoning / streaming / tool ids per turn. */

import type { LoopEvent } from "../../loop.js";
import type { AgentEvent } from "./state/events.js";

export interface LoopBridge {
  /** Translate one loop event. May produce zero or more agent events. */
  consume(ev: LoopEvent): AgentEvent[];
}

export function makeLoopBridge(turnId: string): LoopBridge {
  let reasoningId: string | null = null;
  let streamingId: string | null = null;
  let activeToolId: string | null = null;
  let toolStartedAt = 0;
  let nextToolSeq = 0;
  let turnStarted = false;

  const ensureTurnStarted = (out: AgentEvent[]): void => {
    if (turnStarted) return;
    turnStarted = true;
    out.push({ type: "turn.start", turnId });
  };

  return {
    consume(ev) {
      const out: AgentEvent[] = [];
      switch (ev.role) {
        case "assistant_delta": {
          ensureTurnStarted(out);
          if (ev.reasoningDelta) {
            if (!reasoningId) {
              reasoningId = `${turnId}-reasoning`;
              out.push({ type: "reasoning.start", id: reasoningId });
            }
            out.push({ type: "reasoning.chunk", id: reasoningId, text: ev.reasoningDelta });
          }
          if (ev.content) {
            // First non-empty content chunk closes the reasoning block — by
            // convention DeepSeek's response stream switches from reasoning to
            // content at the boundary and never goes back.
            if (reasoningId) {
              out.push({
                type: "reasoning.end",
                id: reasoningId,
                paragraphs: 0,
                tokens: 0,
              });
              reasoningId = null;
            }
            if (!streamingId) {
              streamingId = `${turnId}-streaming`;
              out.push({ type: "streaming.start", id: streamingId });
            }
            out.push({ type: "streaming.chunk", id: streamingId, text: ev.content });
          }
          return out;
        }
        case "tool_start": {
          ensureTurnStarted(out);
          // Pending streaming text is settled at the tool boundary; the model
          // may emit more content after the tool completes, which starts a
          // fresh streaming card.
          if (streamingId) {
            out.push({ type: "streaming.end", id: streamingId });
            streamingId = null;
          }
          activeToolId = `${turnId}-tool-${nextToolSeq++}`;
          toolStartedAt = Date.now();
          out.push({
            type: "tool.start",
            id: activeToolId,
            name: ev.toolName ?? "tool",
            args: parseArgs(ev.toolArgs),
          });
          return out;
        }
        case "tool": {
          ensureTurnStarted(out);
          // Some loops emit `tool` without a preceding `tool_start` (e.g. the
          // synthetic harvest tool). Fabricate a start so the reducer sees a
          // matching pair.
          if (!activeToolId) {
            activeToolId = `${turnId}-tool-${nextToolSeq++}`;
            toolStartedAt = Date.now();
            out.push({
              type: "tool.start",
              id: activeToolId,
              name: ev.toolName ?? "tool",
              args: parseArgs(ev.toolArgs),
            });
          }
          out.push({
            type: "tool.end",
            id: activeToolId,
            output: ev.content,
            elapsedMs: Math.max(0, Date.now() - toolStartedAt),
          });
          activeToolId = null;
          return out;
        }
        case "assistant_final": {
          if (reasoningId) {
            out.push({
              type: "reasoning.end",
              id: reasoningId,
              paragraphs: 0,
              tokens: ev.stats?.usage?.completionTokens ?? 0,
            });
            reasoningId = null;
          }
          if (streamingId) {
            out.push({ type: "streaming.end", id: streamingId });
            streamingId = null;
          }
          return out;
        }
        case "done": {
          // Wrap up any in-flight cards before the turn closes.
          if (reasoningId) {
            out.push({
              type: "reasoning.end",
              id: reasoningId,
              paragraphs: 0,
              tokens: 0,
            });
            reasoningId = null;
          }
          if (streamingId) {
            out.push({ type: "streaming.end", id: streamingId });
            streamingId = null;
          }
          out.push({
            type: "turn.end",
            usage: usageFromEvent(ev),
          });
          return out;
        }
        case "error": {
          if (reasoningId) {
            out.push({
              type: "reasoning.end",
              id: reasoningId,
              paragraphs: 0,
              tokens: 0,
              aborted: true,
            });
            reasoningId = null;
          }
          if (streamingId) {
            out.push({ type: "streaming.end", id: streamingId, aborted: true });
            streamingId = null;
          }
          if (activeToolId) {
            out.push({
              type: "tool.end",
              id: activeToolId,
              output: ev.error ?? "",
              elapsedMs: Math.max(0, Date.now() - toolStartedAt),
              aborted: true,
            });
            activeToolId = null;
          }
          out.push({ type: "turn.abort" });
          return out;
        }
        // status / tool_call_delta / warning / branch_* — no-op in chat-v2
        // for now; surface via dedicated cards in a follow-up.
        default:
          return out;
      }
    },
  };
}

function parseArgs(raw: string | undefined): unknown {
  if (!raw) return undefined;
  try {
    return JSON.parse(raw);
  } catch {
    return raw;
  }
}

function usageFromEvent(ev: LoopEvent): {
  prompt: number;
  reason: number;
  output: number;
  cacheHit: number;
  cost: number;
} {
  const u = ev.stats?.usage;
  return {
    prompt: u?.promptTokens ?? 0,
    reason: 0,
    output: u?.completionTokens ?? 0,
    cacheHit: u?.cacheHitRatio ?? 0,
    cost: ev.stats?.cost ?? 0,
  };
}
