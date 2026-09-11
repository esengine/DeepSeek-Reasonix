import { memo } from "react";

// Streaming reasoning renders as plain, append-only text: no truncation
// window and no markdown re-parse, so the visible text keeps a stable prefix
// and the region grows monotonically (#8657/#8688). The completed reasoning
// swaps to the formatted Markdown view exactly once, when the stream ends.
export const StreamingReasoningText = memo(function StreamingReasoningText({ text }: { text: string }) {
  return <pre className="reasoning__stream-text">{text}</pre>;
});
