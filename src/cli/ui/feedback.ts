/** Pre-fills the GitHub new-issue body with version + platform + terminal + Node + locale + model. No transcripts, paths, or secrets. */

export interface FeedbackDiagnosticInput {
  version: string;
  platform: string;
  osRelease: string;
  termProgram?: string;
  term?: string;
  nodeVersion: string;
  locale: string;
  model: string;
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
  const termLine = formatTerminal(input.termProgram, input.term);
  const sessionLine = input.sessionId ? `**Session**: ${input.sessionId}\n` : "";
  return [
    `**Reasonix**: ${input.version}`,
    `**Platform**: ${input.platform} (${input.osRelease})`,
    `**Terminal**: ${termLine}`,
    `**Node**: ${input.nodeVersion}`,
    `**Locale**: ${input.locale}`,
    `**Model**: ${input.model}`,
    sessionLine.trimEnd(),
    "",
    "<!-- describe what you were doing when this happened -->",
    "",
  ]
    .filter((l) => l.length > 0 || l === "")
    .join("\n");
}

function formatTerminal(termProgram: string | undefined, term: string | undefined): string {
  const parts: string[] = [];
  if (termProgram) parts.push(termProgram);
  const env: string[] = [];
  if (termProgram) env.push(`TERM_PROGRAM=${termProgram}`);
  if (term) env.push(`TERM=${term}`);
  if (env.length === 0) return parts.length > 0 ? parts.join(" ") : "(unknown)";
  return parts.length > 0 ? `${parts.join(" ")} (${env.join(", ")})` : `(${env.join(", ")})`;
}
