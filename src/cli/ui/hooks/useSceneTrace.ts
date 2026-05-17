import { useStdout } from "ink";
import { useEffect } from "react";
import { emitSceneMessage, isSceneTraceEnabled } from "../scene/trace.js";
import type { Card } from "../state/cards.js";

export type SceneCard = {
  kind: string;
  summary: string;
  args?: string;
  status?: "ok" | "err" | "running";
  elapsed?: string;
  id?: string;
  body?: string;
  ts?: number;
  meta?: string;
};

export type SceneSlashMatch = { cmd: string; summary: string; argsHint?: string };
export type SceneSessionItem = { title: string; meta?: string };

export type SceneTraceCard = SceneCard;

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
  preset?: "auto" | "flash" | "pro";
  cwd?: string;
  /** Local URL of the running dashboard server — surfaced in the rust boot block so users can discover the web UI. Null when --no-dashboard or while the server is still starting. */
  dashboardUrl?: string;
  ctxTokens?: number;
  ctxCap?: number;
  sessionCostUsd?: number;
  lastTurnCostUsd?: number;
  cacheHitRatio?: number;
  lastTurnMs?: number;
  sessionInputTokens?: number;
  sessionOutputTokens?: number;
  slashCatalogJson?: string;
  /** Resolved {cmd, partial, matches} for the active slash-arg picker — covers static AND dynamic completers (models / path / mcp-resources / mcp-prompts / skills) since Node has the data to resolve them. */
  slashArgStateJson?: string;
  promptHistoryJson?: string;
  approvalJson?: string;
  atStateJson?: string;
  /** Active text-input prompt — rust intercepts composer Enter to send the answer back as `prompt-response` instead of submitting it as a chat message. Used by /qq connect and friends under integrated mode. */
  promptInputJson?: string;
  /** Active list picker — rust renders a modal of {key,label} options and posts the chosen key back as `list-picker-response`. Used by /sessions, /theme, /model, /restore under integrated mode. */
  listPickerJson?: string;
};

export type SetupSceneInput = {
  bufferLength: number;
  error?: string;
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

export function toSceneCard(card: Card): SceneCard {
  const summary = summarizeCard(card) ?? "";
  switch (card.kind) {
    case "tool":
      return {
        kind: "tool",
        summary: card.name,
        args: extractToolArgs(card.args),
        status: toolStatus(card),
        elapsed: card.done ? formatElapsed(card.elapsedMs) : undefined,
        id: shortenId(card.id),
      };
    case "user":
      return { kind: "user", summary, body: card.text, ts: card.ts };
    case "reasoning": {
      const meta = card.endedAt
        ? `${card.paragraphs} steps · ${formatElapsed(card.endedAt - card.ts)}`
        : `${card.paragraphs} steps`;
      return { kind: "reasoning", summary, body: card.text, ts: card.ts, meta };
    }
    case "streaming":
      return {
        kind: "streaming",
        summary,
        body: card.text,
        ts: card.ts,
        meta: card.done ? "done" : "streaming…",
      };
    case "live":
      return { kind: "info", summary, body: card.text, ts: card.ts, meta: card.meta };
    case "warn":
      return {
        kind: "warn",
        summary: card.title,
        body: card.message,
        ts: card.ts,
        meta: card.detail,
      };
    case "usage":
      return {
        kind: "usage",
        summary: `cost · turn ${card.turn}`,
        body: formatUsageBody(card),
        ts: card.ts,
        meta: card.elapsedMs ? formatElapsed(card.elapsedMs) : undefined,
      };
    case "tip":
      return { kind: "info", summary, body: formatTipBody(card), ts: card.ts };
    case "doctor":
      return {
        kind: "doctor",
        summary: "/doctor",
        body: formatDoctorBody(card),
        ts: card.ts,
      };
    case "memory":
      return {
        kind: "memory",
        summary: `memory · ${card.entries.length} entr${card.entries.length === 1 ? "y" : "ies"}`,
        body: formatMemoryBody(card),
        ts: card.ts,
        meta: `${formatTokens(card.tokens)} tok`,
      };
    case "ctx":
      return {
        kind: "ctx",
        summary: card.text,
        body: formatCtxBody(card),
        ts: card.ts,
      };
    case "plan":
      return {
        kind: "plan-card",
        summary: card.title,
        body: formatPlanBody(card),
        ts: card.ts,
        meta: card.variant,
      };
    case "task":
      return {
        kind: "task",
        summary: `${card.title} · ${card.index + 1}/${card.total}`,
        body: formatTaskBody(card),
        ts: card.ts,
        meta: card.status,
      };
    default:
      return { kind: card.kind, summary };
  }
}

function formatDoctorBody(card: Extract<Card, { kind: "doctor" }>): string {
  return card.checks
    .map((c) => {
      const glyph = c.level === "ok" ? "✓" : c.level === "warn" ? "▲" : "✕";
      return `${glyph} ${c.label}  —  ${c.detail}`;
    })
    .join("\n");
}

function formatMemoryBody(card: Extract<Card, { kind: "memory" }>): string {
  if (card.entries.length === 0) return "(no memories)";
  return card.entries.map((e) => `· [${e.category}] ${e.summary}`).join("\n");
}

function formatCtxBody(card: Extract<Card, { kind: "ctx" }>): string {
  const used = card.systemTokens + card.toolsTokens + card.logTokens + card.inputTokens;
  const pct = card.ctxMax > 0 ? Math.round((used / card.ctxMax) * 100) : 0;
  const lines = [
    `${formatTokens(used)} / ${formatTokens(card.ctxMax)} used (${pct}%)`,
    `system ${formatTokens(card.systemTokens)}    tools ${formatTokens(card.toolsTokens)} (${card.toolsCount})    log ${formatTokens(card.logTokens)} (${card.logMessages} msgs)    input ${formatTokens(card.inputTokens)}`,
  ];
  if (card.topTools.length > 0) {
    lines.push("top tools:");
    for (const t of card.topTools.slice(0, 5)) {
      lines.push(`  · ${t.name}  ${formatTokens(t.tokens)} (turn ${t.turn})`);
    }
  }
  return lines.join("\n");
}

function formatPlanBody(card: Extract<Card, { kind: "plan" }>): string {
  return card.steps
    .map((s) => {
      const glyph = (() => {
        switch (s.status) {
          case "done":
            return "✓";
          case "running":
            return "◆";
          case "failed":
            return "✕";
          case "blocked":
            return "⊘";
          case "skipped":
            return "·";
          default:
            return "○";
        }
      })();
      return `${glyph} ${s.title}`;
    })
    .join("\n");
}

function formatTaskBody(card: Extract<Card, { kind: "task" }>): string {
  if (card.steps.length === 0) return "(no steps)";
  return card.steps
    .map((s) => {
      const glyph = (() => {
        switch (s.status) {
          case "done":
            return "✓";
          case "running":
            return "◆";
          case "failed":
            return "✕";
          default:
            return "○";
        }
      })();
      const detail = s.detail ? `  — ${s.detail}` : "";
      return `${glyph} ${s.title}${detail}`;
    })
    .join("\n");
}

function formatUsageBody(card: Extract<Card, { kind: "usage" }>): string {
  const { tokens } = card;
  const pct = Math.round(card.cacheHit * 100);
  const lines = [
    `turn ${card.turn} · this ${formatCost(card.cost)} · session ${formatCost(card.sessionCost)}`,
    `prompt ${formatTokens(tokens.prompt)} / ${formatTokens(tokens.promptCap)}    output ${formatTokens(tokens.output)} (reason ${formatTokens(tokens.reason)})`,
    `cache ${pct}%`,
  ];
  if (card.balance !== undefined) {
    lines.push(`balance ${card.balance.toFixed(2)} ${card.balanceCurrency ?? ""}`.trim());
  }
  return lines.join("\n");
}

function formatTipBody(card: Extract<Card, { kind: "tip" }>): string {
  const parts: string[] = [];
  for (const sec of card.sections) {
    if (sec.title) parts.push(sec.title);
    for (const r of sec.rows) {
      parts.push(r.key ? `  ${r.key}  ${r.text}` : `  ${r.text}`);
    }
  }
  if (card.footer) parts.push(card.footer);
  return parts.join("\n");
}

function formatCost(usd: number): string {
  return usd < 0.01 ? `$${usd.toFixed(5)}` : `$${usd.toFixed(4)}`;
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
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

export function parseSlashMatches(json: string | undefined): SceneSlashMatch[] {
  return parseArrayJson(json, (obj) => {
    if (typeof obj.cmd !== "string" || typeof obj.summary !== "string") return undefined;
    const m: SceneSlashMatch = { cmd: obj.cmd, summary: obj.summary };
    if (typeof obj.argsHint === "string") m.argsHint = obj.argsHint;
    return m;
  });
}

export function parseSessions(json: string | undefined): SceneSessionItem[] {
  return parseArrayJson(json, (obj) => {
    if (typeof obj.title !== "string") return undefined;
    const s: SceneSessionItem = { title: obj.title };
    if (typeof obj.meta === "string") s.meta = obj.meta;
    return s;
  });
}

export function parseRecentCards(json: string | undefined): SceneCard[] {
  return parseArrayJson(json, (obj) => {
    if (typeof obj.kind !== "string" || typeof obj.summary !== "string") return undefined;
    const card: SceneCard = { kind: obj.kind, summary: obj.summary };
    if (typeof obj.args === "string") card.args = obj.args;
    if (obj.status === "ok" || obj.status === "err" || obj.status === "running") {
      card.status = obj.status;
    }
    if (typeof obj.elapsed === "string") card.elapsed = obj.elapsed;
    if (typeof obj.id === "string") card.id = obj.id;
    if (typeof obj.body === "string") card.body = obj.body;
    if (typeof obj.ts === "number") card.ts = obj.ts;
    if (typeof obj.meta === "string") card.meta = obj.meta;
    return card;
  });
}

function parseArrayJson<T>(
  json: string | undefined,
  pick: (obj: Record<string, unknown>) => T | undefined,
): T[] {
  if (!json || json.length === 0) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(json);
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  const out: T[] = [];
  for (const item of parsed) {
    if (typeof item !== "object" || item === null) continue;
    const picked = pick(item as Record<string, unknown>);
    if (picked !== undefined) out.push(picked);
  }
  return out;
}

export function useSceneTrace(input: SceneTraceInput): void {
  const { stdout } = useStdout();
  const fallbackCols = stdout?.columns ?? 80;
  const fallbackRows = stdout?.rows ?? 24;
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
    preset,
    cwd,
    dashboardUrl,
    ctxTokens,
    ctxCap,
    sessionCostUsd,
    lastTurnCostUsd,
    cacheHitRatio,
    lastTurnMs,
    sessionInputTokens,
    sessionOutputTokens,
    slashCatalogJson,
    slashArgStateJson,
    promptHistoryJson,
    approvalJson,
    atStateJson,
    promptInputJson,
    listPickerJson,
  } = input;
  useEffect(() => {
    if (!isSceneTraceEnabled()) return;
    emitSceneMessage({
      type: "trace",
      model,
      cardCount,
      cards: parseRecentCards(recentCardsJson),
      busy,
      activity,
      composerText,
      composerCursor,
      slashMatches: parseSlashMatches(slashMatchesJson),
      slashSelectedIndex,
      approvalKind,
      approvalPrompt,
      sessions: parseSessions(sessionsJson),
      sessionsFocusedIndex,
      walletBalance,
      walletCurrency,
      mcpServerCount,
      editMode,
      preset,
      cwd,
      dashboardUrl,
      ctxTokens,
      ctxCap,
      sessionCostUsd,
      lastTurnCostUsd,
      cacheHitRatio,
      lastTurnMs,
      sessionInputTokens,
      sessionOutputTokens,
      slashCatalog: parseSlashMatches(slashCatalogJson),
      slashArgState: parseApproval(slashArgStateJson),
      promptHistory: parseStringArray(promptHistoryJson),
      approval: parseApproval(approvalJson),
      atState: parseApproval(atStateJson),
      promptInput: parseApproval(promptInputJson),
      listPicker: parseApproval(listPickerJson),
      fallbackCols,
      fallbackRows,
    });
  }, [
    fallbackCols,
    fallbackRows,
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
    preset,
    cwd,
    dashboardUrl,
    ctxTokens,
    ctxCap,
    sessionCostUsd,
    lastTurnCostUsd,
    cacheHitRatio,
    lastTurnMs,
    sessionInputTokens,
    sessionOutputTokens,
    slashCatalogJson,
    slashArgStateJson,
    promptHistoryJson,
    approvalJson,
    atStateJson,
    promptInputJson,
    listPickerJson,
  ]);
}

function parseApproval(json: string | undefined): unknown {
  if (!json || json.length === 0) return undefined;
  try {
    return JSON.parse(json);
  } catch {
    return undefined;
  }
}

function parseStringArray(json: string | undefined): string[] {
  if (!json || json.length === 0) return [];
  try {
    const parsed = JSON.parse(json);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((s): s is string => typeof s === "string");
  } catch {
    return [];
  }
}

export function useSetupSceneTrace(input: SetupSceneInput): void {
  const { bufferLength, error } = input;
  useEffect(() => {
    if (!isSceneTraceEnabled()) return;
    emitSceneMessage({ type: "setup", bufferLength, error });
  }, [bufferLength, error]);
}
