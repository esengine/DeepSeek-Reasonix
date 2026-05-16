import { useStdout } from "ink";
import { useEffect } from "react";
import { box, frame, text } from "../scene/build.js";
import { emitSceneFrame, isSceneTraceEnabled } from "../scene/trace.js";
import type { Color, SceneFrame, SceneNode, TextRun } from "../scene/types.js";
import type { Card } from "../state/cards.js";

export type SceneTraceCard = { kind: string; summary: string };
export type SceneSlashMatch = { cmd: string; summary: string; argsHint?: string };
export type SceneSessionItem = { title: string; meta?: string };

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
  /** When set, replaces the composer row with a `❓ <prompt> [y/n]` modal stub. */
  approvalKind?: string;
  approvalPrompt?: string;
  /** JSON-encoded `SceneSessionItem[]`; non-empty replaces composer/slash with the picker block. */
  sessionsJson?: string;
  sessionsFocusedIndex?: number;
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
  approvalKind?: string;
  approvalPrompt?: string;
  sessions?: ReadonlyArray<SceneSessionItem>;
  sessionsFocusedIndex?: number;
};

const APPROVAL_PROMPT_MAX = 60;
const MAX_SESSION_ROWS = 8;

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
  const sessions = input.sessions ?? [];
  const pickerOwnsBottom = sessions.length > 0;
  if (pickerOwnsBottom) {
    children.push(sessionsHeaderRow(sessions.length));
    const sel = Math.max(0, Math.min(sessions.length - 1, input.sessionsFocusedIndex ?? 0));
    const { startIndex, matches: shown } = listWindow(sessions, sel, MAX_SESSION_ROWS);
    for (let i = 0; i < shown.length; i++) {
      const item = shown[i] as SceneSessionItem;
      children.push(sessionRow(item, startIndex + i === sel));
    }
    const hidden = sessions.length - shown.length;
    if (hidden > 0) children.push(slashOverflowRow(hidden));
    children.push(sessionsHintRow());
  } else if (input.approvalPrompt) {
    children.push(approvalRow(input.approvalKind, input.approvalPrompt));
  } else {
    children.push(composerRow(input));
  }
  const slash = input.slashMatches ?? [];
  if (!pickerOwnsBottom && !input.approvalPrompt && slash.length > 0) {
    const sel = Math.max(0, Math.min(slash.length - 1, input.slashSelectedIndex ?? 0));
    const { startIndex, matches: shown } = listWindow(slash, sel, MAX_SLASH_ROWS);
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

function approvalRow(kind: string | undefined, prompt: string): SceneNode {
  const clipped =
    prompt.length > APPROVAL_PROMPT_MAX ? `${prompt.slice(0, APPROVAL_PROMPT_MAX - 1)}…` : prompt;
  const runs: TextRun[] = [{ text: "❓ ", style: { color: "yellow", bold: true } }];
  if (kind) runs.push({ text: `[${kind}] `, style: { dim: true } });
  runs.push({ text: clipped });
  runs.push({ text: "  [y/n]", style: { color: "yellow", bold: true } });
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

export function listWindow<T>(
  items: ReadonlyArray<T>,
  selected: number,
  windowSize: number,
): { startIndex: number; matches: ReadonlyArray<T> } {
  if (items.length <= windowSize) return { startIndex: 0, matches: items };
  const half = Math.floor(windowSize / 2);
  const maxStart = items.length - windowSize;
  const startIndex = Math.max(0, Math.min(maxStart, selected - half));
  return { startIndex, matches: items.slice(startIndex, startIndex + windowSize) };
}

export function slashWindow(
  matches: ReadonlyArray<SceneSlashMatch>,
  selected: number,
): { startIndex: number; matches: ReadonlyArray<SceneSlashMatch> } {
  return listWindow(matches, selected, MAX_SLASH_ROWS);
}

function sessionsHeaderRow(total: number): SceneNode {
  return text([
    { text: "📂 ", style: { color: "cyan", bold: true } },
    { text: "sessions", style: { bold: true } },
    { text: ` (${total} saved)`, style: { dim: true } },
  ]);
}

function sessionRow(item: SceneSessionItem, focused: boolean): SceneNode {
  const runs: TextRun[] = [];
  runs.push({ text: focused ? "▸ " : "  ", style: { color: "cyan" } });
  runs.push({ text: item.title, style: focused ? { bold: true, color: "cyan" } : undefined });
  if (item.meta) {
    runs.push({ text: "  " });
    runs.push({ text: item.meta, style: { dim: true } });
  }
  return text(runs);
}

function sessionsHintRow(): SceneNode {
  return text([
    { text: "↑↓ ", style: { color: "cyan" } },
    { text: "navigate · ", style: { dim: true } },
    { text: "⏎ ", style: { color: "cyan" } },
    { text: "open · ", style: { dim: true } },
    { text: "n ", style: { color: "cyan" } },
    { text: "new · ", style: { dim: true } },
    { text: "esc ", style: { color: "cyan" } },
    { text: "cancel", style: { dim: true } },
  ]);
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

export function parseSessions(json: string | undefined): SceneSessionItem[] {
  if (!json || json.length === 0) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(json);
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  const out: SceneSessionItem[] = [];
  for (const item of parsed) {
    if (typeof item !== "object" || item === null) continue;
    const obj = item as Record<string, unknown>;
    if (typeof obj.title !== "string") continue;
    const s: SceneSessionItem = { title: obj.title };
    if (typeof obj.meta === "string") s.meta = obj.meta;
    out.push(s);
  }
  return out;
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
    approvalKind,
    approvalPrompt,
    sessionsJson,
    sessionsFocusedIndex,
  } = input;
  useEffect(() => {
    if (!isSceneTraceEnabled()) return;
    const parsed = parseRecentCards(recentCardsJson);
    const cards = cardsForHeight(parsed, rows);
    const slashMatches = parseSlashMatches(slashMatchesJson);
    const sessions = parseSessions(sessionsJson);
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
          approvalKind,
          approvalPrompt,
          sessions,
          sessionsFocusedIndex,
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
    approvalKind,
    approvalPrompt,
    sessionsJson,
    sessionsFocusedIndex,
  ]);
}

export type SetupSceneInput = {
  bufferLength: number;
  error?: string;
};

export function buildSetupFrame(input: SetupSceneInput, cols: number, rows: number): SceneFrame {
  const children: SceneNode[] = [];
  children.push(
    text([
      { text: "✦ ", style: { color: "cyan", bold: true } },
      { text: "reasonix", style: { bold: true } },
      { text: " · welcome", style: { dim: true } },
    ]),
  );
  children.push(text([{ text: "Enter your DeepSeek API key:", style: { color: "cyan" } }]));
  children.push(
    text([{ text: "  get one at https://platform.deepseek.com", style: { dim: true } }]),
  );
  const maskedRuns: TextRun[] = [{ text: "❯ ", style: { color: "cyan", bold: true } }];
  if (input.bufferLength === 0) {
    maskedRuns.push({ text: "(start typing your key)", style: { dim: true } });
  } else {
    maskedRuns.push({ text: "•".repeat(input.bufferLength) });
    maskedRuns.push({ text: "▮", style: { color: "cyan" } });
  }
  children.push(text(maskedRuns));
  if (input.error) {
    children.push(
      text([
        { text: "✗ ", style: { color: "red", bold: true } },
        { text: input.error, style: { color: "red" } },
      ]),
    );
  }
  children.push(text([{ text: "Ctrl+C to exit · /exit to quit", style: { dim: true } }]));
  return frame(cols, rows, box(children, { direction: "column", paddingX: 1 }));
}

export function useSetupSceneTrace(input: SetupSceneInput): void {
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const rows = stdout?.rows ?? 24;
  const { bufferLength, error } = input;
  useEffect(() => {
    if (!isSceneTraceEnabled()) return;
    emitSceneFrame(buildSetupFrame({ bufferLength, error }, cols, rows));
  }, [cols, rows, bufferLength, error]);
}
