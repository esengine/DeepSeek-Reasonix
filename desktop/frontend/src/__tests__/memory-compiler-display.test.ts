import { describe, it, expect } from "vitest";

// Duplicate the regex and extract function inline for unit testing.
// In production this lives in Message.tsx; the test ensures behavior correctness.
const MEMORY_COMPILER_EXECUTION_RE = /<memory-compiler-execution>[\s\S]*?<\/memory-compiler-execution>\s*/g;

function extractMemoryCompilerExecution(text: string): { cleaned: string; block: string | null } {
  const match = MEMORY_COMPILER_EXECUTION_RE.exec(text);
  if (!match) return { cleaned: text, block: null };
  MEMORY_COMPILER_EXECUTION_RE.lastIndex = 0;
  return {
    cleaned: text.replace(MEMORY_COMPILER_EXECUTION_RE, "").trimStart(),
    block: match[0].trim(),
  };
}

describe("extractMemoryCompilerExecution", () => {
  it("returns null block for plain text", () => {
    const result = extractMemoryCompilerExecution("Hello world");
    expect(result.block).toBeNull();
    expect(result.cleaned).toBe("Hello world");
  });

  it("extracts memory compiler block and cleans text", () => {
    const input = '<memory-compiler-execution>\n{"type":"memory_v5_execution_contract"}\n</memory-compiler-execution>\nfix the bug';
    const result = extractMemoryCompilerExecution(input);
    expect(result.block).toContain("<memory-compiler-execution>");
    expect(result.block).toContain("memory_v5_execution_contract");
    expect(result.cleaned).toBe("fix the bug");
  });

  it("returns cleaned empty string when block is the only content", () => {
    const input = '<memory-compiler-execution>\n{"planner_ir":{}}\n</memory-compiler-execution>';
    const result = extractMemoryCompilerExecution(input);
    expect(result.block).toContain("<memory-compiler-execution>");
    expect(result.cleaned).toBe("");
  });

  it("handles text without compiler block unchanged", () => {
    const input = "今日分时交易数据分析";
    const result = extractMemoryCompilerExecution(input);
    expect(result.block).toBeNull();
    expect(result.cleaned).toBe(input);
  });
});
