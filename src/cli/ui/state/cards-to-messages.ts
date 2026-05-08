import type { DashboardMessage } from "../../../server/context.js";
import type { Card, ReasoningCard } from "./cards.js";

/** Project state.cards onto the wire shape /api/messages serves to the web SPA. */
export function cardsToDashboardMessages(cards: ReadonlyArray<Card>): DashboardMessage[] {
  const out: DashboardMessage[] = [];
  let pendingReasoning: ReasoningCard | null = null;

  for (const card of cards) {
    switch (card.kind) {
      case "reasoning":
        pendingReasoning = card;
        break;
      case "user":
        out.push({ id: card.id, role: "user", text: card.text });
        break;
      case "streaming": {
        const msg: DashboardMessage = { id: card.id, role: "assistant", text: card.text };
        if (pendingReasoning?.text) msg.reasoning = pendingReasoning.text;
        pendingReasoning = null;
        out.push(msg);
        break;
      }
      case "tool": {
        const msg: DashboardMessage = {
          id: card.id,
          role: "tool",
          text: card.output,
          toolName: card.name,
        };
        if (card.args !== undefined) msg.toolArgs = JSON.stringify(card.args);
        out.push(msg);
        break;
      }
      case "live":
        // Persistent surface only — drop transient hints (thinking / aborted /
        // retry / checkpoint / mcpEvent) that don't belong in chat scrollback.
        if (card.variant === "stepProgress" || card.variant === "sessionOp") {
          out.push({
            id: card.id,
            role: card.tone === "warn" ? "warning" : "info",
            text: card.meta ? `${card.text}\n${card.meta}` : card.text,
          });
        } else if (card.tone === "warn" || card.tone === "err") {
          out.push({
            id: card.id,
            role: "warning",
            text: card.meta ? `${card.text}: ${card.meta}` : card.text,
          });
        }
        break;
      case "ctx":
        out.push({ id: card.id, role: "info", text: card.text });
        break;
      case "plan": {
        const done = card.steps.filter((s) => s.status === "done").length;
        const tag =
          card.variant === "resumed" ? "[resumed]" : card.variant === "replay" ? "[replay]" : "";
        const head = `▸ ${card.title}${tag ? ` ${tag}` : ""} — ${done}/${card.steps.length} done`;
        out.push({ id: card.id, role: "info", text: head });
        break;
      }
      default:
        // approval / diff / task / usage / memory / subagent / search /
        // error / warn — surfaced through other dashboard channels (modals,
        // SSE), not the boot snapshot.
        break;
    }
  }
  return out;
}
