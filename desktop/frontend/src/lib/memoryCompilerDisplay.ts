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
