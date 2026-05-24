/** Prefix the context-manager prepends when fold replaces older turns with a synthesized recap.
 *  Single-sourced here so the agent producer (src/context-manager) and the three UI surfaces
 *  (TUI, Desktop, Dashboard) all agree on the wire string. */
export const COMPACTION_SUMMARY_MARKER =
  "[CONVERSATION HISTORY SUMMARY — earlier turns folded for context efficiency]\n\n";

export function isCompactionSummary(text: string | null | undefined): boolean {
  return typeof text === "string" && text.startsWith(COMPACTION_SUMMARY_MARKER);
}

export function stripCompactionMarker(text: string): string {
  return text.startsWith(COMPACTION_SUMMARY_MARKER)
    ? text.slice(COMPACTION_SUMMARY_MARKER.length)
    : text;
}
