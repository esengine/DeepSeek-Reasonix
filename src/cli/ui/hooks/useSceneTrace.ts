import { useStdout } from "ink";
import { useEffect } from "react";
import { box, frame, text } from "../scene/build.js";
import { emitSceneFrame, isSceneTraceEnabled } from "../scene/trace.js";
import type { Color, SceneFrame, SceneNode, TextRun } from "../scene/types.js";
import type { Card } from "../state/cards.js";

export type SceneTraceCard = { kind: string; summary: string };
export type SceneSlashMatch = { cmd: string; summary: string; argsHint?: string };

export type SceneTraceInput = {
  model?: string;
  cardCount: number;
  /** JSON-encoded `SceneTraceCard[]`, most recent last. Passed as a string so React deps stay primitive — array refs would re-fire the effect every render. */
  recentCardsJson?: string;
  busy: boolean;
  activity?: string;
  /** Current composer text — what the user is typing but has not yet submitted. */
  composerText?: string;
  composerCursor?: number;
  /** JSON-encoded `SceneSlashMatch[]`; undefined/empty hides the overlay. */
  slashMatchesJson?: string;
  slashSelectedIndex?: number;
};

type BuildInput = {
  model?: string;
  cardCount: number;
  cards: ReadonlyArray<SceneTraceCard>;
  busy: boolean;
  activity?: string;
  composerText?: string;
  composerCursor?: number;
  slashMatches?: ReadonlyArray<SceneSlashMatch>;
  slashSelectedIndex?: number;
};

const SUMMARY_MAX = 70;
/** Reserved rows for title + status + composer; the rest can hold cards. */
const RESERVED_ROWS = 3;
/** Hard cap so a tall terminal doesn't make the payload absurd. */
const MAX_CARDS = 24;
const MAX_SLASH_ROWS = 6;

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

export function buildTraceFrame(input: BuildInput, cols: number, rows: number): SceneFrame {
  const children: SceneNode[] = [titleRow(input)];
  if (input.cards.length === 0) {
    children.push(noCardsRow());
  } else {
    for (const c of input.cards) children.push(cardRow(c));
  }
  children.push(statusRow(input));
  children.push(composerRow(input));
  const slash = input.slashMatches ?? [];
  if (slash.length > 0) {
    const sel = Math.max(0, Math.min(slash.length - 1, input.slashSelectedIndex ?? 0));
    const { startIndex, matches: shown } = slashWindow(slash, sel);
    for (let i = 0; i < shown.length; i++) {
      const absoluteIndex = startIndex + i;
      children.push(slashRow(shown[i] as SceneSlashMatch, absoluteIndex === sel));
    }
    const hidden = slash.length - shown.length;
    if (hidden > 0) children.push(slashOverflowRow(hidden));
  }
  return frame(cols, rows, box(children, { direction: "column", paddingX: 1 }));
}

function titleRow(s: BuildInput): SceneNode {
  const runs: TextRun[] = [{ text: "reasonix", style: { bold: true } }];
  if (s.model) runs.push({ text: ` · ${s.model}`, style: { dim: true } });
  return text(runs);
}

function noCardsRow(): SceneNode {
  return text([{ text: "(no cards yet)", style: { dim: true } }]);
}

function cardRow(c: SceneTraceCard): SceneNode {
  const runs: TextRun[] = [
    { text: iconFor(c.kind), style: { color: colorFor(c.kind) } },
    { text: " " },
    { text: c.summary || c.kind, style: c.kind === "user" ? { bold: true } : undefined },
  ];
  return text(runs);
}

function statusRow(s: BuildInput): SceneNode {
  const runs: TextRun[] = [
    { text: `${s.cardCount} cards`, style: { dim: true } },
    { text: " · " },
    { text: s.busy ? "busy" : "idle", style: { color: s.busy ? "yellow" : "green" } },
  ];
  if (s.activity) runs.push({ text: ` · ${s.activity}`, style: { dim: true } });
  return text(runs);
}

function slashRow(m: SceneSlashMatch, selected: boolean): SceneNode {
  const runs: TextRun[] = [];
  runs.push({ text: selected ? "▸ " : "  ", style: { color: "cyan" } });
  runs.push({ text: m.cmd, style: selected ? { bold: true, color: "cyan" } : undefined });
  if (m.argsHint) runs.push({ text: ` ${m.argsHint}`, style: { dim: true } });
  if (m.summary) {
    runs.push({ text: " — ", style: { dim: true } });
    runs.push({ text: m.summary, style: { dim: true } });
  }
  return text(runs);
}

function slashOverflowRow(hidden: number): SceneNode {
  return text([{ text: `…${hidden} more`, style: { dim: true } }]);
}

export function slashWindow(
  matches: ReadonlyArray<SceneSlashMatch>,
  selected: number,
): { startIndex: number; matches: ReadonlyArray<SceneSlashMatch> } {
  if (matches.length <= MAX_SLASH_ROWS) return { startIndex: 0, matches };
  const half = Math.floor(MAX_SLASH_ROWS / 2);
  const maxStart = matches.length - MAX_SLASH_ROWS;
  const startIndex = Math.max(0, Math.min(maxStart, selected - half));
  return { startIndex, matches: matches.slice(startIndex, startIndex + MAX_SLASH_ROWS) };
}

function composerRow(s: BuildInput): SceneNode {
  const runs: TextRun[] = [{ text: "❯ ", style: { color: "cyan", bold: true } }];
  const t = s.composerText ?? "";
  if (t.length === 0) {
    runs.push({ text: "(type a message)", style: { dim: true } });
    return text(runs);
  }
  const cur = Math.max(0, Math.min(t.length, s.composerCursor ?? t.length));
  if (cur > 0) runs.push({ text: t.slice(0, cur) });
  runs.push({ text: "▮", style: { color: "cyan" } });
  if (cur < t.length) runs.push({ text: t.slice(cur) });
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

export function parseSlashMatches(json: string | undefined): SceneSlashMatch[] {
  if (!json || json.length === 0) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(json);
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  const out: SceneSlashMatch[] = [];
  for (const item of parsed) {
    if (typeof item !== "object" || item === null) continue;
    const obj = item as Record<string, unknown>;
    if (typeof obj.cmd !== "string" || typeof obj.summary !== "string") continue;
    const m: SceneSlashMatch = { cmd: obj.cmd, summary: obj.summary };
    if (typeof obj.argsHint === "string") m.argsHint = obj.argsHint;
    out.push(m);
  }
  return out;
}

export function parseRecentCards(json: string | undefined): SceneTraceCard[] {
  if (!json || json.length === 0) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(json);
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  const out: SceneTraceCard[] = [];
  for (const item of parsed) {
    if (typeof item !== "object" || item === null) continue;
    const obj = item as Record<string, unknown>;
    if (typeof obj.kind !== "string" || typeof obj.summary !== "string") continue;
    out.push({ kind: obj.kind, summary: obj.summary });
  }
  return out;
}

export function cardsForHeight(
  cards: ReadonlyArray<SceneTraceCard>,
  rows: number,
): SceneTraceCard[] {
  const room = Math.max(1, Math.min(MAX_CARDS, rows - RESERVED_ROWS));
  return cards.slice(-room);
}

export function useSceneTrace(input: SceneTraceInput): void {
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const rows = stdout?.rows ?? 24;
  const {
    model,
    cardCount,
    recentCardsJson,
    busy,
    activity,
    composerText,
    composerCursor,
    slashMatchesJson,
    slashSelectedIndex,
  } = input;
  useEffect(() => {
    if (!isSceneTraceEnabled()) return;
    const parsed = parseRecentCards(recentCardsJson);
    const cards = cardsForHeight(parsed, rows);
    const slashMatches = parseSlashMatches(slashMatchesJson);
    emitSceneFrame(
      buildTraceFrame(
        {
          model,
          cardCount,
          cards,
          busy,
          activity,
          composerText,
          composerCursor,
          slashMatches,
          slashSelectedIndex,
        },
        cols,
        rows,
      ),
    );
  }, [
    cols,
    rows,
    model,
    cardCount,
    recentCardsJson,
    busy,
    activity,
    composerText,
    composerCursor,
    slashMatchesJson,
    slashSelectedIndex,
  ]);
}
