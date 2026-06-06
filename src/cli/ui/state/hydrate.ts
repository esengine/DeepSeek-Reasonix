import { isCompactionSummary, stripCompactionMarker } from "@reasonix/core-utils";
import type { ChatMessage } from "../../../types.js";
import { extractToolExitCode } from "../tool-summary.js";
import { elideHydratedCards } from "./card-elision.js";
import type { Card, ToolCard } from "./cards.js";

/** Rebuild cards from a persisted ChatMessage[] so resumed sessions render their history. */
export function hydrateCardsFromMessages(messages: ReadonlyArray<ChatMessage>): Card[] {
  const cards: Card[] = [];
  const toolCardByCallId = new Map<string, ToolCard>();
  let seq = 0;
  const ts = Date.now();
  const id = (k: string) => `hyd-${k}-${++seq}`;

  for (const m of messages) {
    if (m.role === "system") continue;

    if (m.role === "user") {
      const text = typeof m.content === "string" ? m.content : "";
      if (text) cards.push({ kind: "user", id: id("user"), ts, text });
      continue;
    }

    if (m.role === "assistant") {
      const reasoning = m.reasoning_content;
      if (typeof reasoning === "string" && reasoning.length > 0) {
        cards.push({
          kind: "reasoning",
          id: id("reasoning"),
          ts,
          text: reasoning,
          paragraphs: reasoning.split(/\n\n+/).length,
          tokens: 0,
          streaming: false,
        });
      }
      const text = typeof m.content === "string" ? m.content : "";
      if (text) {
        if (isCompactionSummary(text)) {
          cards.push({
            kind: "compaction",
            id: id("compaction"),
            ts,
            summary: stripCompactionMarker(text),
          });
        } else {
          cards.push({ kind: "streaming", id: id("streaming"), ts, text, done: true });
        }
      }
      if (m.tool_calls?.length) {
        for (const tc of m.tool_calls) {
          let parsedArgs: unknown = tc.function.arguments;
          try {
            parsedArgs = JSON.parse(tc.function.arguments);
          } catch {
            /* keep raw string when args aren't valid JSON */
          }
          const card: ToolCard = {
            kind: "tool",
            id: id("tool"),
            ts,
            name: tc.function.name,
            args: parsedArgs,
            output: "",
            done: false,
            elapsedMs: 0,
          };
          cards.push(card);
          if (tc.id) toolCardByCallId.set(tc.id, card);
        }
      }
      continue;
    }

    if (m.role === "tool") {
      const callId = m.tool_call_id;
      const card = callId ? toolCardByCallId.get(callId) : undefined;
      const text = typeof m.content === "string" ? m.content : "";
      if (card) {
        card.output = text;
        const exitCode = extractToolExitCode(card.name, text);
        if (exitCode !== undefined) card.exitCode = exitCode;
        card.done = true;
      }
    }
  }

  return [...elideHydratedCards(cards)];
}
