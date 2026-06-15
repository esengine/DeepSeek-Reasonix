export const STREAMING_REASONING_TAIL_CHARS = 12_000;
export const STREAMING_REASONING_TAIL_LINES = 240;

type ReasoningDisplayOptions = {
  streaming: boolean;
  truncateStreaming?: boolean;
  maxChars?: number;
  maxLines?: number;
};

export function displayReasoningText(
  reasoning: string,
  {
    streaming,
    truncateStreaming = true,
    maxChars = STREAMING_REASONING_TAIL_CHARS,
    maxLines = STREAMING_REASONING_TAIL_LINES,
  }: ReasoningDisplayOptions,
): string {
  if (!streaming || !truncateStreaming) return reasoning;

  let text = reasoning;
  let truncated = false;

  if (maxChars > 0 && text.length > maxChars) {
    text = text.slice(-maxChars);
    truncated = true;
  }

  if (maxLines > 0) {
    const lines = text.split(/\r?\n/);
    if (lines.length > maxLines) {
      text = lines.slice(-maxLines).join("\n");
      truncated = true;
    }
  }

  return truncated ? `...\n${text}` : text;
}
