import { parseRateLimitedToolResult } from "../tools/rate-limit.js";
import type { ChatMessage, ToolCall } from "../types.js";
import type { LoopEvent } from "./types.js";

export interface RunOneToolCallResult {
  preWarnings: LoopEvent[];
  postWarnings: LoopEvent[];
  result: string;
}

export interface DispatchContext {
  turn: number;
  signal: AbortSignal;
  isParallelSafe: (name: string) => boolean;
  inflightIdFor: (call: ToolCall) => string;
  inflightAdd: (id: string) => void;
  runOne: (call: ToolCall, signal: AbortSignal) => Promise<RunOneToolCallResult>;
  appendAndPersist: (msg: ChatMessage) => void;
  /** Mutable across iter cycles — single rate-limit warning per step(). */
  rateLimitState: { shown: boolean };
}

function readParallelMax(): number {
  const raw = Number.parseInt(process.env.REASONIX_PARALLEL_MAX ?? "", 10);
  return Number.isFinite(raw) && raw >= 1 ? Math.min(raw, 16) : 3;
}

function readDispatchSerial(): boolean {
  return (process.env.REASONIX_TOOL_DISPATCH ?? "auto").toLowerCase() === "serial";
}

export async function* dispatchToolCallsChunked(
  repairedCalls: ToolCall[],
  ctx: DispatchContext,
): AsyncGenerator<LoopEvent, void, void> {
  const dispatchSerial = readDispatchSerial();
  const parallelMax = readParallelMax();

  let callIdx = 0;
  while (callIdx < repairedCalls.length) {
    const chunk: ToolCall[] = [];
    if (!dispatchSerial) {
      while (
        callIdx < repairedCalls.length &&
        chunk.length < parallelMax &&
        ctx.isParallelSafe(repairedCalls[callIdx]?.function?.name ?? "")
      ) {
        chunk.push(repairedCalls[callIdx++]!);
      }
    }
    if (chunk.length === 0) {
      chunk.push(repairedCalls[callIdx++]!);
    }

    for (const call of chunk) {
      const callId = ctx.inflightIdFor(call);
      ctx.inflightAdd(callId);
      yield {
        turn: ctx.turn,
        role: "tool_start",
        content: "",
        toolName: call.function?.name ?? "",
        toolArgs: call.function?.arguments ?? "{}",
        callId,
      };
    }

    const settled = await Promise.allSettled(chunk.map((c) => ctx.runOne(c, ctx.signal)));

    for (let k = 0; k < chunk.length; k++) {
      const call = chunk[k]!;
      const name = call.function?.name ?? "";
      const args = call.function?.arguments ?? "{}";
      const s = settled[k]!;

      let result: string;
      let preWarnings: LoopEvent[] = [];
      let postWarnings: LoopEvent[] = [];
      if (s.status === "fulfilled") {
        preWarnings = s.value.preWarnings;
        postWarnings = s.value.postWarnings;
        result = s.value.result;
      } else {
        const err = s.reason instanceof Error ? s.reason : new Error(String(s.reason));
        result = JSON.stringify({ error: `${err.name}: ${err.message}` });
      }

      for (const w of preWarnings) yield w;
      for (const w of postWarnings) yield w;

      const rateLimited = parseRateLimitedToolResult(result);
      if (rateLimited && !ctx.rateLimitState.shown) {
        ctx.rateLimitState.shown = true;
        yield {
          turn: ctx.turn,
          role: "warning",
          content: rateLimited.message,
        };
      }

      ctx.appendAndPersist({
        role: "tool",
        tool_call_id: call.id ?? "",
        name,
        content: result,
      });

      yield {
        turn: ctx.turn,
        role: "tool",
        content: result,
        toolName: name,
        toolArgs: args,
        callId: ctx.inflightIdFor(call),
      };
    }
  }
}
