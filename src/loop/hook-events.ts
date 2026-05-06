import { type HookOutcome, formatHookOutcomeMessage } from "../hooks.js";
import type { LoopEvent } from "./types.js";

export function safeParseToolArgs(raw: string): unknown {
  try {
    return JSON.parse(raw);
  } catch {
    return raw;
  }
}

/** Format non-pass hook outcomes as `LoopEvent`s of role `warning`. */
export function* hookWarnings(outcomes: HookOutcome[], turn: number): Generator<LoopEvent> {
  for (const o of outcomes) {
    if (o.decision === "pass") continue;
    yield { turn, role: "warning", content: formatHookOutcomeMessage(o) };
  }
}
