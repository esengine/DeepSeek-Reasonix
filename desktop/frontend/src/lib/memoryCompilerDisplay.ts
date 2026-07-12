const HIDDEN_BLOCK_NAMES = "memory-compiler-execution|autoresearch-evidence|autoresearch-runtime|active-goal";
const COMPLETE_BLOCK_RE = new RegExp(`<(${HIDDEN_BLOCK_NAMES})>[\\s\\S]*?<\\/\\1>\\s*`, "g");
const DANGLING_BLOCK_RE = new RegExp(`<(?:${HIDDEN_BLOCK_NAMES})>[\\s\\S]*$`);
const GOAL_STATUS_RE = /\[goal:(?:complete|continue|blocked(?::[^\]\r\n]*)?)\]\s*/g;

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
  return text
    .replace(COMPLETE_BLOCK_RE, "")
    .replace(DANGLING_BLOCK_RE, "")
    .replace(GOAL_STATUS_RE, "")
    .trimStart();
}
