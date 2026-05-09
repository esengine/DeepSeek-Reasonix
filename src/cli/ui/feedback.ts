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

export const FEEDBACK_ISSUE_URL = "https://github.com/esengine/DeepSeek-Reasonix/issues/new";

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
