export const STREAMING_REASONING_TAIL_CHARS = 12_000;
export const STREAMING_REASONING_TAIL_LINES = 240;
export const STREAMING_REASONING_WINDOW_STEP_CHARS = 2_000;
export const STREAMING_REASONING_WINDOW_STEP_LINES = 40;

type ReasoningDisplayOptions = {
  streaming: boolean;
  truncateStreaming?: boolean;
  maxChars?: number;
  maxLines?: number;
  stableWindowChars?: number;
  stableWindowLines?: number;
};

export function displayReasoningText(
  reasoning: string,
  {
    streaming,
    truncateStreaming = true,
    maxChars = STREAMING_REASONING_TAIL_CHARS,
    maxLines = STREAMING_REASONING_TAIL_LINES,
    stableWindowChars = 0,
    stableWindowLines = 0,
  }: ReasoningDisplayOptions,
): string {
  if (!streaming || !truncateStreaming) return reasoning;

  let text = reasoning;
  let truncated = false;

  if (maxChars > 0 && text.length > maxChars) {
    const overflow = text.length - maxChars;
    const start = stableWindowChars > 0
      ? Math.floor(overflow / stableWindowChars) * stableWindowChars
      : overflow;
    if (start > 0) {
      text = text.slice(start);
      truncated = true;
    }
  }

  if (maxLines > 0) {
    const lines = text.split(/\r?\n/);
    if (lines.length > maxLines) {
      const overflow = lines.length - maxLines;
      const start = stableWindowLines > 0
        ? Math.floor(overflow / stableWindowLines) * stableWindowLines
        : overflow;
      if (start > 0) {
        text = lines.slice(start).join("\n");
        truncated = true;
      }
    }
  }

  return truncated ? `...\n${text}` : text;
}
