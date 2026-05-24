/** Pure tool-result summarizer — shared by ToolCard, replay, and transcript export. */

const MAX_SUMMARY_CHARS = 80;
const TRAILING_ELLIPSIS = "…";

export interface ToolSummary {
  /** Single-line summary text. Empty string if the result was empty. */
  summary: string;
  /** True when the tool result represents a failure the renderer should color red. */
  isError: boolean;
}

export type ToolPreviewRow =
  | { kind: "hidden"; count: number }
  | { kind: "line"; index: number; text: string };

export interface ToolPreview {
  rows: ToolPreviewRow[];
  hidden: number;
  truncated: boolean;
}

const SHELL_TOOL_NAMES = new Set(["run_command", "run_background"]);
const FAILURE_LINE_PATTERN = /(AssertionError|Error:|FAIL|FAILED|expected|actual|✗)/;
const FAILURE_SCAN_LINES = 200;
const FAILURE_PINNED_LINES = 3;

function isShellTool(toolName: string): boolean {
  return SHELL_TOOL_NAMES.has(toolName);
}

function clip(s: string, max: number): string {
  if (s.length <= max) return s;
  return s.slice(0, Math.max(1, max - TRAILING_ELLIPSIS.length)) + TRAILING_ELLIPSIS;
}

function firstNonEmptyLine(text: string): string {
  for (const line of text.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (trimmed) return trimmed;
  }
  return "";
}

export function extractToolExitCode(toolName: string, output: string): number | undefined {
  if (!isShellTool(toolName)) return undefined;
  const match = output.match(/(?:^|\r?\n)\[exit (-?\d+)\](?=\r?\n|$)/);
  if (!match?.[1]) return undefined;
  return Number.parseInt(match[1], 10);
}

export function selectToolPreviewLines(input: {
  toolName: string;
  output: string;
  exitCode?: number;
  tailLines: number;
  verbose: boolean;
  maxScanLines?: number;
  maxPinnedLines?: number;
}): ToolPreview {
  const lines = input.output.length > 0 ? input.output.split("\n") : [];
  if (input.verbose || lines.length <= input.tailLines) {
    return {
      rows: lines.map((text, index) => ({ kind: "line", index, text })),
      hidden: 0,
      truncated: false,
    };
  }

  const indexes = new Set<number>();
  const failedShell =
    isShellTool(input.toolName) && input.exitCode !== undefined && input.exitCode !== 0;

  if (failedShell) {
    const scanLimit = Math.min(lines.length, input.maxScanLines ?? FAILURE_SCAN_LINES);
    const pinnedLimit = input.maxPinnedLines ?? FAILURE_PINNED_LINES;
    let pinned = 0;
    for (let i = 0; i < scanLimit && pinned < pinnedLimit; i++) {
      if (FAILURE_LINE_PATTERN.test(lines[i] ?? "")) {
        indexes.add(i);
        pinned++;
      }
    }
  }

  const tailStart = Math.max(0, lines.length - input.tailLines);
  for (let i = tailStart; i < lines.length; i++) {
    indexes.add(i);
  }

  return buildPreviewRows(lines, indexes);
}

function buildPreviewRows(lines: string[], indexes: ReadonlySet<number>): ToolPreview {
  const rows: ToolPreviewRow[] = [];
  let hidden = 0;
  let previous = -1;
  const sorted = [...indexes].filter((i) => i >= 0 && i < lines.length).sort((a, b) => a - b);

  for (const index of sorted) {
    const gap = index - previous - 1;
    if (gap > 0) {
      rows.push({ kind: "hidden", count: gap });
      hidden += gap;
    }
    rows.push({ kind: "line", index, text: lines[index] ?? "" });
    previous = index;
  }

  const trailing = lines.length - previous - 1;
  if (trailing > 0) {
    rows.push({ kind: "hidden", count: trailing });
    hidden += trailing;
  }

  return { rows, hidden, truncated: hidden > 0 };
}

export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return "";
  if (ms < 100) return `${Math.round(ms)}ms`;
  if (ms < 1000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms < 10_000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms < 60_000) return `${Math.round(ms / 1000)}s`;
  const totalSec = Math.round(ms / 1000);
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  return s === 0 ? `${m}m` : `${m}m${s}s`;
}

function formatBytes(n: number): string {
  if (n < 1000) return `${n}B`;
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}KB`;
  return `${(n / 1_000_000).toFixed(1)}MB`;
}

function formatLineCount(text: string): string {
  // Cheap line count — the +1 covers files without a trailing newline.
  const lines = text.split(/\r?\n/).length;
  return `${lines} line${lines === 1 ? "" : "s"}`;
}

function summarizeStructured(content: string): ToolSummary | null {
  const trimmed = content.trim();
  if (!trimmed.startsWith("{")) return null;
  try {
    const parsed = JSON.parse(trimmed);
    if (!parsed || typeof parsed !== "object") return null;
    const obj = parsed as Record<string, unknown>;
    // Plan / choice signals come through as errors carrying structured
    // payloads — the App-level handlers extract the structured part.
    // For the tool row here we just want the tag.
    if (typeof obj.error === "string") {
      const tag = obj.error.split(":", 1)[0]?.trim() ?? "error";
      const detail = obj.error.slice(tag.length + 1).trim();
      // The tag-only case (no colon body) — show the bare tag.
      const summary = detail ? `${tag} — ${detail}` : tag;
      // Plan / Choice errors are control-flow signals, not real errors.
      const isControlSignal =
        tag === "PlanProposedError" ||
        tag === "PlanRevisionProposedError" ||
        tag === "ChoiceRequestedError" ||
        tag === "NeedsConfirmationError";
      return { summary: clip(summary, MAX_SUMMARY_CHARS), isError: !isControlSignal };
    }
    // step_completed payload (when used outside the error path, kept
    // for forward-compat with non-throwing variants).
    if (obj.kind === "step_completed" && typeof obj.stepId === "string") {
      const result = typeof obj.result === "string" ? obj.result : "";
      return {
        summary: clip(`✓ ${obj.stepId}: ${result}`, MAX_SUMMARY_CHARS),
        isError: false,
      };
    }
    return null;
  } catch {
    return null;
  }
}

/** Suffix-match so MCP-prefixed tools (`filesystem_read_file`) pick up the same specialized summary. */
function summarizeKnownTool(toolName: string, content: string): ToolSummary | null {
  const hasSuffix = (s: string) => toolName === s || toolName.endsWith(`_${s}`);
  if (hasSuffix("read_file")) {
    const lines = formatLineCount(content);
    const bytes = formatBytes(content.length);
    const head = clip(
      firstNonEmptyLine(content),
      MAX_SUMMARY_CHARS - lines.length - bytes.length - 8,
    );
    return {
      summary: head ? `${head} · ${lines} · ${bytes}` : `${lines} · ${bytes}`,
      isError: false,
    };
  }
  if (hasSuffix("list_directory") || hasSuffix("directory_tree")) {
    const entries = content.split(/\r?\n/).filter((l) => l.trim()).length;
    return { summary: `${entries} entr${entries === 1 ? "y" : "ies"}`, isError: false };
  }
  if (hasSuffix("search_files") || hasSuffix("search_content")) {
    const matches = content.split(/\r?\n/).filter((l) => l.trim()).length;
    if (matches === 0) return { summary: "no matches", isError: false };
    const first = firstNonEmptyLine(content);
    return {
      summary: clip(`${matches} match${matches === 1 ? "" : "es"} · ${first}`, MAX_SUMMARY_CHARS),
      isError: false,
    };
  }
  if (hasSuffix("write_file")) {
    const lines = formatLineCount(content);
    const bytes = formatBytes(content.length);
    return { summary: `wrote ${lines} · ${bytes}`, isError: false };
  }
  if (hasSuffix("multi_edit")) {
    const m = content.match(/applied (\d+) edits? to (\S+)/);
    if (m) {
      return { summary: `${m[1]} edit${m[1] === "1" ? "" : "s"} · ${m[2]}`, isError: false };
    }
    return { summary: clip(firstNonEmptyLine(content), MAX_SUMMARY_CHARS), isError: false };
  }
  if (hasSuffix("todo_write")) {
    if (/^todos cleared/.test(content)) return { summary: "todos cleared", isError: false };
    const m = content.match(/^todos updated · (\d+) done · (\d+) in progress · (\d+) pending/);
    if (m) {
      return { summary: `${m[1]} done · ${m[2]} in progress · ${m[3]} pending`, isError: false };
    }
    return { summary: clip(firstNonEmptyLine(content), MAX_SUMMARY_CHARS), isError: false };
  }
  if (hasSuffix("run_command") || hasSuffix("run_background")) {
    // Native shell tools prepend "exit 0:" / "exit N:" or the result
    // already mentions exit code. Try to surface it.
    const exitMatch = content.match(/exit (?:code )?(-?\d+)/i);
    const first = firstNonEmptyLine(content);
    if (exitMatch) {
      const code = exitMatch[1];
      const isError = code !== "0";
      return {
        summary: clip(`exit ${code} · ${first}`, MAX_SUMMARY_CHARS),
        isError,
      };
    }
    return { summary: clip(first || "(no output)", MAX_SUMMARY_CHARS), isError: false };
  }
  return null;
}

export function summarizeToolResult(toolName: string, content: string): ToolSummary {
  const isExplicitError = content.startsWith("ERROR:");
  if (isExplicitError) {
    const stripped = content.slice("ERROR:".length).trim();
    return { summary: clip(stripped || "(unknown error)", MAX_SUMMARY_CHARS), isError: true };
  }
  const structured = summarizeStructured(content);
  if (structured) return structured;
  const known = summarizeKnownTool(toolName, content);
  if (known) return known;
  // Generic: first line + size hint.
  const first = firstNonEmptyLine(content);
  if (!content.trim()) return { summary: "(empty)", isError: false };
  if (content.length <= MAX_SUMMARY_CHARS) {
    return { summary: clip(first, MAX_SUMMARY_CHARS), isError: false };
  }
  const sizeHint = formatBytes(content.length);
  const head = clip(first, MAX_SUMMARY_CHARS - sizeHint.length - 3);
  return { summary: `${head} · ${sizeHint}`, isError: false };
}
