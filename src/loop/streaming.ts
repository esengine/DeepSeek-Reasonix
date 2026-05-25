import type { DeepSeekClient, Usage } from "../client.js";
import type { ReasoningEffort } from "../config.js";
import type { ChatMessage, ToolCall, ToolSpec } from "../types.js";
import { looksLikeCompleteJson } from "./shrink.js";
import { thinkingModeForModel } from "./thinking.js";
import type { LoopEvent } from "./types.js";

export interface StreamModelOptions {
  client: DeepSeekClient;
  model: string;
  messages: ChatMessage[];
  toolSpecs: ToolSpec[];
  signal: AbortSignal;
  reasoningEffort: ReasoningEffort;
  turn: number;
}

export interface StreamModelResult {
  assistantContent: string;
  reasoningContent: string;
  toolCalls: ToolCall[];
  usage: Usage | null;
}

export async function* streamModelResponse(
  opts: StreamModelOptions,
): AsyncGenerator<LoopEvent, StreamModelResult, void> {
  const { client, model, messages, toolSpecs, signal, reasoningEffort, turn } = opts;
  let assistantContent = "";
  let reasoningContent = "";
  let usage: Usage | null = null;
  const callBuf: Map<number, ToolCall> = new Map();
  const readyIndices = new Set<number>();

  for await (const chunk of client.stream({
    model,
    messages,
    tools: toolSpecs.length ? toolSpecs : undefined,
    signal,
    thinking: thinkingModeForModel(model),
    reasoningEffort,
  })) {
    if (chunk.reasoningDelta) {
      reasoningContent += chunk.reasoningDelta;
      yield {
        turn,
        role: "assistant_delta",
        content: "",
        reasoningDelta: chunk.reasoningDelta,
      };
    }
    if (chunk.contentDelta) {
      assistantContent += chunk.contentDelta;
      yield {
        turn,
        role: "assistant_delta",
        content: chunk.contentDelta,
      };
    }
    if (chunk.toolCallDelta) {
      const d = chunk.toolCallDelta;
      const cur = callBuf.get(d.index) ?? {
        id: d.id,
        type: "function" as const,
        function: { name: "", arguments: "" },
      };
      if (d.id) cur.id = d.id;
      if (d.name) cur.function.name = (cur.function.name ?? "") + d.name;
      if (d.argumentsDelta)
        cur.function.arguments = (cur.function.arguments ?? "") + d.argumentsDelta;
      callBuf.set(d.index, cur);

      if (
        !readyIndices.has(d.index) &&
        cur.function.name &&
        looksLikeCompleteJson(cur.function.arguments ?? "")
      ) {
        readyIndices.add(d.index);
      }

      if (cur.function.name) {
        yield {
          turn,
          role: "tool_call_delta",
          content: "",
          toolName: cur.function.name,
          toolCallArgsChars: (cur.function.arguments ?? "").length,
          toolCallIndex: d.index,
          toolCallReadyCount: readyIndices.size,
        };
      }
    }
    if (chunk.usage) usage = chunk.usage;
  }

  return {
    assistantContent,
    reasoningContent,
    toolCalls: [...callBuf.values()],
    usage,
  };
}
