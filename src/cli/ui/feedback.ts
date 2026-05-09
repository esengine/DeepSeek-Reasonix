/** Pre-fills the GitHub new-issue body with version + platform + terminal + Node + locale + model. No transcripts, paths, or secrets. */

import { compareVersions } from "../../version.js";

export interface FeedbackDiagnosticInput {
  version: string;
  latestVersion?: string | null;
  platform: string;
  osRelease: string;
  termProgram?: string;
  term?: string;
  colorTerm?: string;
  inWindowsTerminal?: boolean;
  inTmux?: boolean;
  inSsh?: boolean;
  wslDistro?: string;
  cols?: number;
  rows?: number;
  nodeVersion: string;
  locale: string;
  theme?: string;
  model: string;
  reasoningEffort?: string;
  editMode?: string;
  planMode?: boolean;
  mcpServerCount?: number;
  sessionId?: string;
}

const FEEDBACK_ISSUE_BASE = "https://github.com/esengine/DeepSeek-Reasonix/issues/new";

/** Bare URL used as a fallback when query-pre-fill isn't possible (only really if the body somehow blew past URL limits). */
export const FEEDBACK_ISSUE_URL = FEEDBACK_ISSUE_BASE;

/** GitHub safely accepts ~7000 chars in the body query param — well above our ~300-char diagnostic, but cap defensively. */
const FEEDBACK_BODY_QUERY_LIMIT = 6000;

export function buildFeedbackIssueUrl(diagnostic: string): string {
  const trimmed =
    diagnostic.length > FEEDBACK_BODY_QUERY_LIMIT
      ? diagnostic.slice(0, FEEDBACK_BODY_QUERY_LIMIT)
      : diagnostic;
  return `${FEEDBACK_ISSUE_BASE}?body=${encodeURIComponent(trimmed)}`;
}

export function buildFeedbackDiagnostic(input: FeedbackDiagnosticInput): string {
  const lines: string[] = [];
  lines.push(`**Reasonix**: ${formatVersion(input.version, input.latestVersion)}`);
  lines.push(`**Platform**: ${input.platform} (${input.osRelease})`);
  lines.push(`**Terminal**: ${formatTerminal(input)}`);
  if (typeof input.cols === "number" && typeof input.rows === "number") {
    lines.push(`**Size**: ${input.cols}×${input.rows}`);
  }
  lines.push(`**Node**: ${input.nodeVersion}`);
  lines.push(`**Locale**: ${input.locale}`);
  if (input.theme) lines.push(`**Theme**: ${input.theme}`);
  lines.push(`**Model**: ${formatModel(input.model, input.reasoningEffort)}`);
  const modeLine = formatMode(input.editMode, input.planMode);
  if (modeLine) lines.push(`**Mode**: ${modeLine}`);
  if (typeof input.mcpServerCount === "number") {
    lines.push(`**MCP**: ${input.mcpServerCount} server(s)`);
  }
  if (input.sessionId) lines.push(`**Session**: ${input.sessionId}`);
  lines.push("", "<!-- describe what you were doing when this happened -->", "");
  return lines.join("\n");
}

function formatVersion(installed: string, latest: string | null | undefined): string {
  if (!latest) return installed;
  const cmp = compareVersions(installed, latest);
  if (cmp === 0) return `${installed} (latest)`;
  if (cmp > 0) return installed;
  return `${installed} (latest: ${latest})`;
}

function formatModel(model: string, effort: string | undefined): string {
  return effort ? `${model} · effort=${effort}` : model;
}

function formatMode(editMode: string | undefined, planMode: boolean | undefined): string {
  const parts: string[] = [];
  if (editMode) parts.push(`edit=${editMode}`);
  parts.push(`plan=${planMode ? "on" : "off"}`);
  return parts.join(" · ");
}

function formatTerminal(input: FeedbackDiagnosticInput): string {
  const head = input.termProgram ?? "(unknown)";
  const env: string[] = [];
  if (input.termProgram) env.push(`TERM_PROGRAM=${input.termProgram}`);
  if (input.term) env.push(`TERM=${input.term}`);
  if (input.colorTerm) env.push(`COLORTERM=${input.colorTerm}`);
  if (input.inWindowsTerminal) env.push("WT_SESSION=set");
  if (input.inTmux) env.push("TMUX=set");
  if (input.inSsh) env.push("SSH_TTY=set");
  if (input.wslDistro) env.push(`WSL=${input.wslDistro}`);
  if (env.length === 0) return head;
  return `${head} (${env.join(", ")})`;
}
