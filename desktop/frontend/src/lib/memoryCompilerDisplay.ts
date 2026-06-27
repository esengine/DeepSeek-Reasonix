const COMPLETE_BLOCK_RE = /<memory-compiler-execution>[\s\S]*?<\/memory-compiler-execution>\s*/g;
const DANGLING_BLOCK_RE = /<memory-compiler-execution>[\s\S]*$/;

/**
 * Removes the Memory v5 `<memory-compiler-execution>` contract block from a user
 * turn before it is rendered in the transcript.
 *
 * The block is model-internal planning metadata that REPLACES the user turn; the
 * backend already unwraps it to the original prompt (`source_event`) for display.
 * This is the display-boundary safety net: a corrupted/accreted contract from the
 * pre-fix goal loop (#5342) could otherwise surface as raw JSON "乱码" after
 * switching between conversations (#5361). Complete blocks are removed, then any
 * dangling (unclosed/truncated) block is cut from its opening tag onward so raw
 * contract JSON is never shown.
 */
export function stripMemoryCompilerExecution(text: string): string {
  return text.replace(COMPLETE_BLOCK_RE, "").replace(DANGLING_BLOCK_RE, "").trimStart();
}

/**
 * Extracts the <memory-compiler-execution> block from text. Returns the cleaned
 * text (block removed) and the raw block content. When no block is present,
 * block is null and cleaned equals the original text.
 */
export function extractMemoryCompilerExecution(text: string): { cleaned: string; block: string | null } {
  const match = COMPLETE_BLOCK_RE.exec(text);
  if (!match) return { cleaned: text, block: null };
  COMPLETE_BLOCK_RE.lastIndex = 0;
  return {
    cleaned: text.replace(COMPLETE_BLOCK_RE, "").trimStart(),
    block: match[0].trim(),
  };
}
