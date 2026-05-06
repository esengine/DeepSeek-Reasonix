/** Accepts `<<<NEEDS_PRO>>>` or `<<<NEEDS_PRO: reason>>>` (reason trimmed, may be empty). */
const NEEDS_PRO_MARKER_PREFIX = "<<<NEEDS_PRO";
const NEEDS_PRO_MARKER_RE = /^<<<NEEDS_PRO(?::\s*([^>]*))?>>>/;
/** Buffer cap before flushing — must fit `<<<NEEDS_PRO: reason>>>` without premature flush. */
export const NEEDS_PRO_BUFFER_CHARS = 256;

/** Anchored to lead — mid-text matches are normal content (user asking about the marker). */
export function parseEscalationMarker(content: string): { matched: boolean; reason?: string } {
  const m = NEEDS_PRO_MARKER_RE.exec(content.trimStart());
  if (!m) return { matched: false };
  const reason = m[1]?.trim();
  return { matched: true, reason: reason || undefined };
}

/** Convenience boolean — same gate the streaming path used to call. */
export function isEscalationRequest(content: string): boolean {
  return parseEscalationMarker(content).matched;
}

/** Drives streaming flush — while plausibly partial, keep accumulating; else flush. */
export function looksLikePartialEscalationMarker(buf: string): boolean {
  const t = buf.trimStart();
  if (t.length === 0) return true;
  if (t.length <= NEEDS_PRO_MARKER_PREFIX.length) {
    return NEEDS_PRO_MARKER_PREFIX.startsWith(t);
  }
  if (!t.startsWith(NEEDS_PRO_MARKER_PREFIX)) return false;
  const rest = t.slice(NEEDS_PRO_MARKER_PREFIX.length);
  if (rest[0] !== ">" && rest[0] !== ":") return false;
  return true;
}
