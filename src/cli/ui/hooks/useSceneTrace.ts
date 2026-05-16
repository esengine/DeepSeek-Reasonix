import { useStdout } from "ink";
import { useEffect } from "react";
import { box, frame, text } from "../scene/build.js";
import { PALETTE } from "../scene/theme.js";
import { emitSceneFrame, isSceneTraceEnabled } from "../scene/trace.js";
import type { Color, SceneFrame, SceneNode, TextRun } from "../scene/types.js";
import type { Card } from "../state/cards.js";

export type SceneTraceCard = {
  kind: string;
  summary: string;
  args?: string;
  status?: "ok" | "err" | "running";
  elapsed?: string;
  id?: string;
};
export type SceneSlashMatch = { cmd: string; summary: string; argsHint?: string };
export type SceneSessionItem = { title: string; meta?: string };

export type SceneTraceInput = {
  model?: string;
  cardCount: number;
  recentCardsJson?: string;
  busy: boolean;
  activity?: string;
  composerText?: string;
  composerCursor?: number;
  slashMatchesJson?: string;
  slashSelectedIndex?: number;
  approvalKind?: string;
  approvalPrompt?: string;
  sessionsJson?: string;
  sessionsFocusedIndex?: number;
  walletBalance?: number;
  walletCurrency?: string;
  sidebarSessionsJson?: string;
  sidebarActiveSession?: string;
  mcpServerCount?: number;
  editMode?: "review" | "auto" | "yolo";
  cwd?: string;
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
  walletBalance?: number;
  walletCurrency?: string;
  sidebarSessions?: ReadonlyArray<SceneSessionItem>;
  sidebarActiveSession?: string;
  mcpServerCount?: number;
  editMode?: "review" | "auto" | "yolo";
  cwd?: string;
};

const SUMMARY_MAX = 70;
const RESERVED_ROWS = 4;
const MAX_CARDS = 24;
const MAX_SLASH_ROWS = 6;
const MAX_SESSION_ROWS = 8;
const APPROVAL_PROMPT_MAX = 60;

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

export function toSceneCard(card: Card): SceneTraceCard {
  const summary = summarizeCard(card) ?? "";
  if (card.kind !== "tool") return { kind: card.kind, summary };
  return {
    kind: "tool",
    summary: card.name,
    args: extractToolArgs(card.args),
    status: toolStatus(card),
    elapsed: card.done ? formatElapsed(card.elapsedMs) : undefined,
    id: shortenId(card.id),
  };
}

function toolStatus(card: Card & { kind: "tool" }): "ok" | "err" | "running" {
  if (!card.done) return "running";
  if (card.rejected || card.aborted || (card.exitCode !== undefined && card.exitCode !== 0)) {
    return "err";
  }
  return "ok";
}

function extractToolArgs(args: unknown): string | undefined {
  if (args === null || args === undefined) return undefined;
  if (typeof args === "string") return clip(args);
  if (typeof args !== "object") return clip(String(args));
  const obj = args as Record<string, unknown>;
  for (const key of ["path", "file", "command", "cmd", "pattern", "query", "url"]) {
    const v = obj[key];
    if (typeof v === "string" && v.length > 0) return clip(v);
  }
  for (const v of Object.values(obj)) {
    if (typeof v === "string" && v.length > 0) return clip(v);
  }
  return undefined;
}

function formatElapsed(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return "";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function shortenId(id: string): string | undefined {
  if (!id) return undefined;
  const tail = id.replace(/[^a-z0-9]/gi, "").slice(-4);
  return tail ? `#${tail}` : undefined;
}

function clip(s: string): string {
  const firstLine = s.split("\n", 1)[0] ?? "";
  return firstLine.length > SUMMARY_MAX ? `${firstLine.slice(0, SUMMARY_MAX - 1)}…` : firstLine;
}

export function buildTraceFrame(input: BuildInput, cols: number, rows: number): SceneFrame {
  return frame(
    cols,
    rows,
    box([scrollArea(input), dock(input)], {
      direction: "column",
      background: PALETTE.bg,
    }),
  );
}

function scrollArea(input: BuildInput): SceneNode {
  const children: SceneNode[] = [];
  if (input.cards.length === 0) {
    children.push(...bootBlock(input));
  } else {
    for (const c of input.cards) children.push(cardBlock(c));
  }
  return box(children, {
    direction: "column",
    height: "fill",
    width: "fill",
    paddingX: 2,
    paddingY: 1,
  });
}

const LOGO_LINES: readonly string[] = [
  "██████╗ ███████╗ █████╗ ███████╗ ██████╗ ███╗   ██╗██╗██╗  ██╗",
  "██╔══██╗██╔════╝██╔══██╗██╔════╝██╔═══██╗████╗  ██║██║╚██╗██╔╝",
  "██████╔╝█████╗  ███████║███████╗██║   ██║██╔██╗ ██║██║ ╚███╔╝ ",
  "██╔══██╗██╔══╝  ██╔══██║╚════██║██║   ██║██║╚██╗██║██║ ██╔██╗ ",
  "██║  ██║███████╗██║  ██║███████║╚██████╔╝██║ ╚████║██║██╔╝ ██╗",
  "╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚══════╝ ╚═════╝ ╚═╝  ╚═══╝╚═╝╚═╝  ╚═╝",
];

function bootBlock(input: BuildInput): SceneNode[] {
  const rows: SceneNode[] = [];
  rows.push(blankRow());
  for (const line of LOGO_LINES) {
    rows.push(text([{ text: line, style: { color: PALETTE.ds, bold: true } }]));
  }
  rows.push(blankRow());
  rows.push(
    text([
      { text: " DeepSeek code agent  ", style: { color: PALETTE.fg } },
      { text: "· terminal-native, cache-first ·", style: { color: PALETTE.fg2 } },
    ]),
  );
  rows.push(blankRow());
  if (input.model) rows.push(bootField("model", input.model, PALETTE.dsBright));
  if (input.cwd) rows.push(bootField("workdir", input.cwd, PALETTE.fg));
  if (input.mcpServerCount !== undefined && input.mcpServerCount > 0) {
    rows.push(bootField("mcp", `${input.mcpServerCount} server(s) connected`, PALETTE.fg));
  }
  rows.push(bootField("tools", "read · write · edit · bash · grep · fetch · todo", PALETTE.fg));
  rows.push(blankRow());
  rows.push(
    text([
      { text: " ", style: {} },
      { text: "type to chat  ", style: { color: PALETTE.fg2 } },
      { text: "·  ", style: { color: PALETTE.fg3 } },
      { text: "/", style: { color: PALETTE.ds, bold: true } },
      { text: " commands  ", style: { color: PALETTE.fg2 } },
      { text: "·  ", style: { color: PALETTE.fg3 } },
      { text: "@", style: { color: PALETTE.ds, bold: true } },
      { text: " file refs  ", style: { color: PALETTE.fg2 } },
      { text: "·  ", style: { color: PALETTE.fg3 } },
      { text: "!", style: { color: PALETTE.ds, bold: true } },
      { text: " shell  ", style: { color: PALETTE.fg2 } },
      { text: "·  ", style: { color: PALETTE.fg3 } },
      { text: "Ctrl+C", style: { color: PALETTE.ds, bold: true } },
      { text: " cancel  ", style: { color: PALETTE.fg2 } },
      { text: "·  ", style: { color: PALETTE.fg3 } },
      { text: "Ctrl+D", style: { color: PALETTE.ds, bold: true } },
      { text: " exit", style: { color: PALETTE.fg2 } },
    ]),
  );
  return rows;
}

function blankRow(): SceneNode {
  return text([{ text: "", style: {} }]);
}

function bootField(key: string, value: string, valueColor: Color): SceneNode {
  return text([
    { text: ` ${key.padEnd(10)}`, style: { color: PALETTE.fg2 } },
    { text: value, style: { color: valueColor } },
  ]);
}

export const REASONIX_LOGO_LINES = LOGO_LINES;

function cardBlock(c: SceneTraceCard): SceneNode {
  if (c.kind === "tool") return toolCardBlock(c);
  const color = colorFor(c.kind);
  const label = kindLabel(c.kind);
  const runs: TextRun[] = [{ text: glyphFor(c.kind), style: { color, bold: true } }, { text: " " }];
  if (label) {
    runs.push({ text: label, style: { color, bold: true } });
    runs.push({ text: "  " });
  }
  runs.push({ text: c.summary || c.kind, style: { color: PALETTE.fg } });
  return text(runs);
}

function toolCardBlock(c: SceneTraceCard): SceneNode {
  const runs: TextRun[] = [
    { text: "▸ ", style: { color: PALETTE.fg2 } },
    { text: c.summary || "tool", style: { color: PALETTE.fg, bold: true } },
  ];
  if (c.args) {
    runs.push({ text: " (", style: { color: PALETTE.fg2 } });
    runs.push({ text: c.args, style: { color: PALETTE.dsBright } });
    runs.push({ text: ")", style: { color: PALETTE.fg2 } });
  }
  runs.push({ text: "  ", style: {} });
  if (c.status === "ok") {
    runs.push({ text: "✓", style: { color: PALETTE.ok, bold: true } });
  } else if (c.status === "err") {
    runs.push({ text: "✗", style: { color: PALETTE.err, bold: true } });
  } else {
    runs.push({ text: "…", style: { color: PALETTE.warn } });
  }
  if (c.elapsed) runs.push({ text: ` ${c.elapsed}`, style: { color: PALETTE.fg2 } });
  if (c.id) runs.push({ text: `  ${c.id}`, style: { color: PALETTE.fg3 } });
  return text(runs);
}

function glyphFor(kind: string): string {
  switch (kind) {
    case "user":
      return "❯";
    case "reasoning":
      return "◆";
    case "streaming":
      return "◆";
    case "tool":
      return "▸";
    case "diff":
      return "Δ";
    case "error":
      return "✗";
    case "warn":
      return "!";
    case "plan":
    case "task":
      return "◆";
    default:
      return "·";
  }
}

function colorFor(kind: string): Color {
  switch (kind) {
    case "user":
      return PALETTE.ds;
    case "reasoning":
      return PALETTE.dsPurple;
    case "streaming":
      return PALETTE.ok;
    case "tool":
      return PALETTE.fg1;
    case "diff":
      return PALETTE.dsPurple;
    case "error":
      return PALETTE.err;
    case "warn":
      return PALETTE.warn;
    case "plan":
    case "task":
      return PALETTE.dsPurple;
    default:
      return PALETTE.fg2;
  }
}

function kindLabel(kind: string): string | null {
  switch (kind) {
    case "user":
      return "YOU";
    case "reasoning":
      return "THINKING";
    case "streaming":
      return "reasonix";
    case "tool":
    case "diff":
    case "error":
    case "warn":
    case "plan":
    case "task":
      return null;
    default:
      return null;
  }
}

function dock(input: BuildInput): SceneNode {
  const children: SceneNode[] = [];
  const sessions = input.sessions ?? [];
  const slash = input.slashMatches ?? [];
  if (sessions.length > 0) {
    children.push(...sessionsPickerBlock(input, sessions));
  } else if (slash.length > 0 && !input.approvalPrompt) {
    children.push(...slashOverlayBlock(input, slash));
  }
  if (input.approvalPrompt) {
    children.push(approvalRow(input.approvalKind, input.approvalPrompt));
  } else {
    children.push(composerRow(input));
  }
  children.push(metaRow());
  children.push(statusBarRow(input));
  return box(children, { direction: "column" });
}

function composerRow(s: BuildInput): SceneNode {
  const runs: TextRun[] = [
    { text: " ", style: { color: PALETTE.fg } },
    { text: "❯ ", style: { color: PALETTE.ds, bold: true } },
  ];
  const t = s.composerText ?? "";
  if (t.length === 0) {
    runs.push({
      text: "type to chat · / for commands · @ for files",
      style: { color: PALETTE.fg2 },
    });
    return box([text(runs)], { direction: "row", background: PALETTE.bg2, height: 1 });
  }
  const cur = Math.max(0, Math.min(t.length, s.composerCursor ?? t.length));
  if (cur > 0) runs.push({ text: t.slice(0, cur), style: { color: PALETTE.fg } });
  runs.push({ text: "▮", style: { color: PALETTE.ds } });
  if (cur < t.length) runs.push({ text: t.slice(cur), style: { color: PALETTE.fg } });
  return box([text(runs)], { direction: "row", background: PALETTE.bg2, height: 1 });
}

function metaRow(): SceneNode {
  const left = text([
    { text: " ", style: {} },
    { text: "↵", style: { color: PALETTE.ds } },
    { text: " send  ", style: { color: PALETTE.fg2 } },
    { text: "⇧↵", style: { color: PALETTE.ds } },
    { text: " newline  ", style: { color: PALETTE.fg2 } },
    { text: "/", style: { color: PALETTE.ds } },
    { text: " cmd  ", style: { color: PALETTE.fg2 } },
    { text: "@", style: { color: PALETTE.ds } },
    { text: " file  ", style: { color: PALETTE.fg2 } },
    { text: "!", style: { color: PALETTE.ds } },
    { text: " shell", style: { color: PALETTE.fg2 } },
  ]);
  const right = text([
    { text: "esc", style: { color: PALETTE.ds } },
    { text: " cancel  ", style: { color: PALETTE.fg2 } },
    { text: "↑", style: { color: PALETTE.ds } },
    { text: " history ", style: { color: PALETTE.fg2 } },
  ]);
  return box([left, box([], { width: "fill" }), right], { direction: "row", height: 1 });
}

function statusBarRow(s: BuildInput): SceneNode {
  const children: SceneNode[] = [];
  children.push(
    text([
      { text: " ●", style: { color: PALETTE.ok } },
      { text: " reasonix", style: { bold: true, color: PALETTE.fg } },
    ]),
  );
  if (s.model) {
    children.push(
      text([
        { text: "  model ", style: { color: PALETTE.fg2 } },
        { text: s.model, style: { color: PALETTE.ds } },
      ]),
    );
  }
  if (s.editMode) {
    const modeColor =
      s.editMode === "yolo" ? PALETTE.err : s.editMode === "auto" ? PALETTE.warn : PALETTE.ds;
    children.push(
      text([
        { text: "  mode ", style: { color: PALETTE.fg2 } },
        { text: s.editMode, style: { color: modeColor, bold: true } },
      ]),
    );
  }
  children.push(
    text([
      { text: "  ", style: {} },
      { text: s.busy ? "busy" : "idle", style: { color: s.busy ? PALETTE.warn : PALETTE.ok } },
    ]),
  );
  if (s.activity) {
    children.push(text([{ text: ` · ${s.activity}`, style: { color: PALETTE.fg2 } }]));
  }
  children.push(box([], { width: "fill" }));
  const wallet = formatWallet(s.walletBalance, s.walletCurrency);
  if (wallet) {
    children.push(
      text([
        { text: "wallet ", style: { color: PALETTE.fg2 } },
        { text: `${wallet} `, style: { color: PALETTE.ok, bold: true } },
      ]),
    );
  }
  if (s.cwd) {
    children.push(
      text([
        { text: "cwd ", style: { color: PALETTE.fg2 } },
        { text: `${truncCwd(s.cwd)} `, style: { color: PALETTE.fg1 } },
      ]),
    );
  }
  return box(children, { direction: "row", background: PALETTE.bg2, height: 1 });
}

function truncCwd(cwd: string): string {
  if (cwd.length <= 30) return cwd;
  return `…${cwd.slice(-29)}`;
}

function formatWallet(total: number | undefined, currency: string | undefined): string | null {
  if (total === undefined || !Number.isFinite(total)) return null;
  const symbol = currencySymbol(currency);
  return `${symbol}${total.toFixed(2)}`;
}

function currencySymbol(currency: string | undefined): string {
  switch ((currency ?? "").toUpperCase()) {
    case "CNY":
    case "RMB":
      return "¥";
    case "USD":
      return "$";
    case "EUR":
      return "€";
    case "GBP":
      return "£";
    case "JPY":
      return "¥";
    default:
      return currency ? `${currency} ` : "";
  }
}

function approvalRow(kind: string | undefined, prompt: string): SceneNode {
  const clipped =
    prompt.length > APPROVAL_PROMPT_MAX ? `${prompt.slice(0, APPROVAL_PROMPT_MAX - 1)}…` : prompt;
  const runs: TextRun[] = [{ text: " ❓ ", style: { color: PALETTE.warn, bold: true } }];
  if (kind) runs.push({ text: `[${kind}] `, style: { color: PALETTE.fg2 } });
  runs.push({ text: clipped, style: { color: PALETTE.fg } });
  runs.push({ text: "  [y/n]", style: { color: PALETTE.warn, bold: true } });
  return box([text(runs)], { direction: "row", background: PALETTE.bg2, height: 1 });
}

function slashOverlayBlock(
  input: BuildInput,
  matches: ReadonlyArray<SceneSlashMatch>,
): SceneNode[] {
  const sel = Math.max(0, Math.min(matches.length - 1, input.slashSelectedIndex ?? 0));
  const { startIndex, matches: shown } = listWindow(matches, sel, MAX_SLASH_ROWS);
  const rows: SceneNode[] = [];
  rows.push(
    text([
      { text: " ", style: {} },
      { text: "/", style: { color: PALETTE.ds, bold: true } },
      { text: " commands", style: { color: PALETTE.fg2 } },
      {
        text: `  ${matches.length} match${matches.length === 1 ? "" : "es"}`,
        style: { color: PALETTE.fg3 },
      },
    ]),
  );
  for (let i = 0; i < shown.length; i++) {
    const absoluteIndex = startIndex + i;
    rows.push(slashRow(shown[i] as SceneSlashMatch, absoluteIndex === sel));
  }
  const hidden = matches.length - shown.length;
  if (hidden > 0) rows.push(overflowRow(hidden));
  return rows;
}

function slashRow(m: SceneSlashMatch, selected: boolean): SceneNode {
  const runs: TextRun[] = [];
  runs.push({
    text: selected ? " ▸ " : "   ",
    style: { color: selected ? PALETTE.ds : PALETTE.fg3 },
  });
  runs.push({
    text: m.cmd,
    style: selected ? { bold: true, color: PALETTE.dsBright } : { color: PALETTE.fg1 },
  });
  if (m.argsHint) runs.push({ text: ` ${m.argsHint}`, style: { color: PALETTE.fg2 } });
  if (m.summary) {
    runs.push({ text: "  ", style: {} });
    runs.push({ text: m.summary, style: { color: PALETTE.fg2 } });
  }
  return text(runs);
}

function sessionsPickerBlock(
  input: BuildInput,
  list: ReadonlyArray<SceneSessionItem>,
): SceneNode[] {
  const sel = Math.max(0, Math.min(list.length - 1, input.sessionsFocusedIndex ?? 0));
  const { startIndex, matches: shown } = listWindow(list, sel, MAX_SESSION_ROWS);
  const rows: SceneNode[] = [];
  rows.push(
    text([
      { text: " ", style: {} },
      { text: "◇", style: { color: PALETTE.ds, bold: true } },
      { text: " sessions", style: { color: PALETTE.fg2 } },
      { text: `  ${list.length} saved`, style: { color: PALETTE.fg3 } },
    ]),
  );
  for (let i = 0; i < shown.length; i++) {
    const absoluteIndex = startIndex + i;
    rows.push(sessionRow(shown[i] as SceneSessionItem, absoluteIndex === sel));
  }
  const hidden = list.length - shown.length;
  if (hidden > 0) rows.push(overflowRow(hidden));
  rows.push(
    text([
      { text: " ", style: {} },
      { text: "↑↓", style: { color: PALETTE.ds } },
      { text: " navigate  ", style: { color: PALETTE.fg2 } },
      { text: "⏎", style: { color: PALETTE.ds } },
      { text: " open  ", style: { color: PALETTE.fg2 } },
      { text: "n", style: { color: PALETTE.ds } },
      { text: " new  ", style: { color: PALETTE.fg2 } },
      { text: "esc", style: { color: PALETTE.ds } },
      { text: " cancel", style: { color: PALETTE.fg2 } },
    ]),
  );
  return rows;
}

function sessionRow(item: SceneSessionItem, focused: boolean): SceneNode {
  const runs: TextRun[] = [];
  runs.push({
    text: focused ? " ▸ " : "   ",
    style: { color: focused ? PALETTE.ds : PALETTE.fg3 },
  });
  runs.push({
    text: item.title,
    style: focused ? { bold: true, color: PALETTE.dsBright } : { color: PALETTE.fg1 },
  });
  if (item.meta) {
    runs.push({ text: "  ", style: {} });
    runs.push({ text: item.meta, style: { color: PALETTE.fg2 } });
  }
  return text(runs);
}

function overflowRow(hidden: number): SceneNode {
  return text([{ text: `   …${hidden} more`, style: { color: PALETTE.fg3 } }]);
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
    const card: SceneTraceCard = { kind: obj.kind, summary: obj.summary };
    if (typeof obj.args === "string") card.args = obj.args;
    if (obj.status === "ok" || obj.status === "err" || obj.status === "running") {
      card.status = obj.status;
    }
    if (typeof obj.elapsed === "string") card.elapsed = obj.elapsed;
    if (typeof obj.id === "string") card.id = obj.id;
    out.push(card);
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
    walletBalance,
    walletCurrency,
    mcpServerCount,
    editMode,
    cwd,
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
          walletBalance,
          walletCurrency,
          mcpServerCount,
          editMode,
          cwd,
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
    walletBalance,
    walletCurrency,
    mcpServerCount,
    editMode,
    cwd,
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
      { text: " ● ", style: { color: PALETTE.ds, bold: true } },
      { text: "REASONIX", style: { bold: true, color: PALETTE.dsBright } },
      { text: "  welcome", style: { color: PALETTE.fg2 } },
    ]),
  );
  children.push(text([{ text: "", style: {} }]));
  children.push(text([{ text: " Enter your DeepSeek API key:", style: { color: PALETTE.ds } }]));
  children.push(
    text([{ text: "   get one at https://platform.deepseek.com", style: { color: PALETTE.fg2 } }]),
  );
  const maskedRuns: TextRun[] = [{ text: " ❯ ", style: { color: PALETTE.ds, bold: true } }];
  if (input.bufferLength === 0) {
    maskedRuns.push({ text: "(start typing your key)", style: { color: PALETTE.fg2 } });
  } else {
    maskedRuns.push({ text: "•".repeat(input.bufferLength), style: { color: PALETTE.fg } });
    maskedRuns.push({ text: "▮", style: { color: PALETTE.ds } });
  }
  children.push(text(maskedRuns));
  if (input.error) {
    children.push(
      text([
        { text: " ✗ ", style: { color: PALETTE.err, bold: true } },
        { text: input.error, style: { color: PALETTE.err } },
      ]),
    );
  }
  children.push(text([{ text: "", style: {} }]));
  children.push(text([{ text: " Ctrl+C to exit · /exit to quit", style: { color: PALETTE.fg2 } }]));
  return frame(cols, rows, box(children, { direction: "column", background: PALETTE.bg }));
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
