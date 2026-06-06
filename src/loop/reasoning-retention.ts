import type { ChatMessage } from "../types.js";

export interface ReasoningPruneResult {
  messages: ChatMessage[];
  prunedCount: number;
  charsDropped: number;
}

function hasToolCalls(msg: ChatMessage): boolean {
  return Array.isArray(msg.tool_calls) && msg.tool_calls.length > 0;
}

/** Keep tool-call reasoning for DeepSeek validation; drop stale plain-turn reasoning. */
export function stripDroppableReasoningContent(messages: ChatMessage[]): ReasoningPruneResult {
  let lastUser = -1;
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i]!.role === "user") {
      lastUser = i;
      break;
    }
  }
  if (lastUser < 0) {
    return { messages, prunedCount: 0, charsDropped: 0 };
  }

  let next: ChatMessage[] | null = null;
  let prunedCount = 0;
  let charsDropped = 0;
  for (let i = 0; i < messages.length; i++) {
    const msg = messages[i]!;
    if (
      msg.role !== "assistant" ||
      i > lastUser ||
      hasToolCalls(msg) ||
      !Object.hasOwn(msg, "reasoning_content")
    ) {
      continue;
    }
    if (next === null) next = messages.slice();
    const { reasoning_content: dropped, ...replacement } = msg;
    if (typeof dropped === "string") charsDropped += dropped.length;
    next[i] = replacement;
    prunedCount += 1;
  }

  return {
    messages: next ?? messages,
    prunedCount,
    charsDropped,
  };
}
