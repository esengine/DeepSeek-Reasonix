const COMPLETE_BLOCK_RE = /<memory-compiler-execution>[\s\S]*?<\/memory-compiler-execution>\s*/g;
const DANGLING_BLOCK_RE = /<memory-compiler-execution>[\s\S]*$/;

/**
 * Removes the legacy Memory v5 `<memory-compiler-execution>` contract block from
 * a user turn before it is rendered in the transcript.
 *
 * The Memory v5 compiler was removed, but transcripts recorded by releases up to
 * v1.17.x can still carry injected contracts: the block was model-internal
 * planning metadata that REPLACED the user turn, and the backend unwraps it to
 * the original prompt (`source_event`) for display. This is the display-boundary
 * safety net for those old sessions: a corrupted/accreted contract from the
 * pre-fix goal loop (#5342) could otherwise surface as raw JSON "乱码" after
 * switching between conversations (#5361). Complete blocks are removed, then any
 * dangling (unclosed/truncated) block is cut from its opening tag onward so raw
 * contract JSON is never shown.
 */
export function stripMemoryCompilerExecution(text: string): string {
  return text.replace(COMPLETE_BLOCK_RE, "").replace(DANGLING_BLOCK_RE, "").trimStart();
}
