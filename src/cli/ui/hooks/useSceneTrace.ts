import { useStdout } from "ink";
import { useEffect } from "react";
import { box, frame, text } from "../scene/build.js";
import { emitSceneFrame, isSceneTraceEnabled } from "../scene/trace.js";
import type { Color, SceneFrame, SceneNode, TextRun } from "../scene/types.js";
import type { Card } from "../state/cards.js";

export type SceneTraceInput = {
  model?: string;
  cardCount: number;
  lastCardKind?: string;
  lastCardSummary?: string;
  busy: boolean;
  activity?: string;
};

const SUMMARY_MAX = 70;

export function summarizeCard(card: Card | undefined): string | undefined {
  if (!card) return undefined;
  switch (card.kind) {
    case "user":
    case "reasoning":
    case "streaming":
      return clip(card.text);
    case "tool":
      return clip(card.done ? card.name : `${card.name} …`);
    case "error":
    case "warn":
      return clip(card.title);
    case "task":
    case "plan":
      return clip(card.title);
    case "diff":
      return clip(card.file);
    default:
      return card.kind;
  }
}

function clip(s: string): string {
  const firstLine = s.split("\n", 1)[0] ?? "";
  return firstLine.length > SUMMARY_MAX ? `${firstLine.slice(0, SUMMARY_MAX - 1)}…` : firstLine;
}

export function buildTraceFrame(input: SceneTraceInput, cols: number, rows: number): SceneFrame {
  return frame(
    cols,
    rows,
    box([titleRow(input), cardRow(input), statusRow(input)], {
      direction: "column",
      paddingX: 1,
    }),
  );
}

function titleRow(s: SceneTraceInput): SceneNode {
  const runs: TextRun[] = [{ text: "reasonix", style: { bold: true } }];
  if (s.model) runs.push({ text: ` · ${s.model}`, style: { dim: true } });
  return text(runs);
}

function cardRow(s: SceneTraceInput): SceneNode {
  if (!s.lastCardKind) {
    return text([{ text: "(no cards yet)", style: { dim: true } }]);
  }
  const runs: TextRun[] = [
    { text: iconFor(s.lastCardKind), style: { color: colorFor(s.lastCardKind) } },
    { text: " " },
    {
      text: s.lastCardSummary ?? s.lastCardKind,
      style: s.lastCardKind === "user" ? { bold: true } : undefined,
    },
  ];
  return text(runs);
}

function statusRow(s: SceneTraceInput): SceneNode {
  const runs: TextRun[] = [
    { text: `${s.cardCount} cards`, style: { dim: true } },
    { text: " · " },
    { text: s.busy ? "busy" : "idle", style: { color: s.busy ? "yellow" : "green" } },
  ];
  if (s.activity) runs.push({ text: ` · ${s.activity}`, style: { dim: true } });
  return text(runs);
}

function iconFor(kind: string): string {
  switch (kind) {
    case "user":
      return ">";
    case "streaming":
      return "●";
    case "reasoning":
      return "⟐";
    case "tool":
      return "▸";
    case "error":
      return "✗";
    case "warn":
      return "!";
    case "plan":
    case "task":
      return "□";
    case "diff":
      return "Δ";
    default:
      return "·";
  }
}

function colorFor(kind: string): Color {
  switch (kind) {
    case "user":
      return "cyan";
    case "streaming":
      return "green";
    case "reasoning":
      return "magenta";
    case "tool":
    case "plan":
    case "task":
      return "blue";
    case "error":
      return "red";
    case "warn":
    case "diff":
      return "yellow";
    default:
      return "default";
  }
}

export function useSceneTrace(input: SceneTraceInput): void {
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const rows = stdout?.rows ?? 24;
  const { model, cardCount, lastCardKind, lastCardSummary, busy, activity } = input;
  useEffect(() => {
    if (!isSceneTraceEnabled()) return;
    emitSceneFrame(
      buildTraceFrame(
        { model, cardCount, lastCardKind, lastCardSummary, busy, activity },
        cols,
        rows,
      ),
    );
  }, [cols, rows, model, cardCount, lastCardKind, lastCardSummary, busy, activity]);
}
