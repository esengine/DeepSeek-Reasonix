import { t } from "../../../i18n/index.js";
import type { Card } from "../state/cards.js";

export type SnapshotLineKind = "header" | "text" | "blank";

export interface SnapshotLine {
  readonly cardId: string;
  readonly kind: SnapshotLineKind;
  readonly role: "user" | "assistant" | "reasoning";
  readonly text: string;
}

export function buildSnapshot(cards: ReadonlyArray<Card>): SnapshotLine[] {
  const out: SnapshotLine[] = [];
  for (const card of cards) {
    if (card.kind === "user") {
      pushCard(out, card.id, "user", t("copyMode.labelUser"), card.text);
    } else if (card.kind === "streaming") {
      pushCard(out, card.id, "assistant", t("copyMode.labelAssistant"), card.text);
    } else if (card.kind === "reasoning") {
      pushCard(out, card.id, "reasoning", t("copyMode.labelReasoning"), card.text);
    }
  }
  return out;
}

function pushCard(
  out: SnapshotLine[],
  cardId: string,
  role: SnapshotLine["role"],
  label: string,
  body: string,
): void {
  if (out.length > 0) out.push({ cardId, kind: "blank", role, text: "" });
  out.push({ cardId, kind: "header", role, text: `─── ${label} ───` });
  const lines = body.length === 0 ? [""] : body.split("\n");
  for (const line of lines) out.push({ cardId, kind: "text", role, text: line });
}

export function yankRange(
  snapshot: ReadonlyArray<SnapshotLine>,
  fromIdx: number,
  toIdx: number,
): string {
  const lo = Math.min(fromIdx, toIdx);
  const hi = Math.max(fromIdx, toIdx);
  const picks: string[] = [];
  for (let i = lo; i <= hi; i++) {
    const line = snapshot[i];
    if (!line) continue;
    if (line.kind === "header") continue;
    picks.push(line.text);
  }
  while (picks.length > 0 && picks[picks.length - 1] === "") picks.pop();
  while (picks.length > 0 && picks[0] === "") picks.shift();
  return picks.join("\n");
}

export function isYankable(line: SnapshotLine | undefined): boolean {
  return !!line && line.kind !== "header";
}
