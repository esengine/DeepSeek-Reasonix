/** Locks cache-prefix byte budget — every byte ships in every request. PRs that
 *  grow it must compress elsewhere or raise the constant with a commit-message reason. */

import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { CODE_SYSTEM_PROMPT, codeSystemPrompt } from "../src/code/prompt.js";
import { ToolRegistry } from "../src/tools.js";
import { registerChoiceTool } from "../src/tools/choice.js";
import { registerFilesystemTools } from "../src/tools/filesystem.js";
import { JobRegistry } from "../src/tools/jobs.js";
import { registerMemoryTools } from "../src/tools/memory.js";
import { registerPlanTool } from "../src/tools/plan.js";
import { registerScaffoldTools } from "../src/tools/scaffold.js";
import { registerShellTools } from "../src/tools/shell.js";
import { registerSkillTools } from "../src/tools/skills.js";
import { registerTodoTool } from "../src/tools/todo.js";
import { registerWebTools } from "../src/tools/web.js";

/** Base system prompt — no memory, no .gitignore, no built-in skills index. */
const SYSTEM_PROMPT_BUDGET = 24_500;
/** Adds the built-in skills index `applySkillsIndex` injects on top of the base. */
const SYSTEM_PROMPT_BUDGET_WITH_SKILLS = 26_500;
/** Full code-mode tool spec list — descriptions + JSON-schema parameters, all 35 tools. */
const TOOL_LIST_BUDGET = 40_000;

function buildCodeToolset(root: string): ToolRegistry {
  const tools = new ToolRegistry();
  const jobs = new JobRegistry();
  registerFilesystemTools(tools, { rootDir: root });
  registerShellTools(tools, { rootDir: root, jobs });
  registerMemoryTools(tools, { projectRoot: root });
  registerPlanTool(tools);
  registerChoiceTool(tools);
  registerTodoTool(tools);
  registerScaffoldTools(tools, { projectRoot: root });
  registerWebTools(tools);
  registerSkillTools(tools, { projectRoot: root, disableBuiltins: true });
  return tools;
}

function totalToolBytes(tools: ToolRegistry): {
  total: number;
  perTool: Array<{ name: string; descBytes: number; schemaBytes: number; total: number }>;
} {
  const specs = tools.specs();
  const perTool = specs.map((s) => {
    const descBytes = (s.function?.description ?? "").length;
    const schemaBytes = JSON.stringify(s.function?.parameters ?? {}).length;
    return {
      name: s.function?.name ?? "?",
      descBytes,
      schemaBytes,
      total: descBytes + schemaBytes,
    };
  });
  const total = perTool.reduce((sum, t) => sum + t.total, 0);
  return { total, perTool };
}

describe("prompt budget — cache prefix size regression net", () => {
  it("system prompt stays under the byte budget", () => {
    const actual = CODE_SYSTEM_PROMPT.length;
    if (actual > SYSTEM_PROMPT_BUDGET) {
      throw new Error(
        `CODE_SYSTEM_PROMPT is ${actual} bytes — exceeds budget ${SYSTEM_PROMPT_BUDGET}. Either compress an equivalent section in src/code/prompt.ts OR raise SYSTEM_PROMPT_BUDGET in tests/prompt-budget.test.ts with a justification in the commit message.`,
      );
    }
    expect(actual).toBeLessThanOrEqual(SYSTEM_PROMPT_BUDGET);
  });

  it("code-mode tool list stays under the byte budget", () => {
    const root = mkdtempSync(join(tmpdir(), "reasonix-budget-"));
    try {
      const tools = buildCodeToolset(root);
      const { total, perTool } = totalToolBytes(tools);
      if (total > TOOL_LIST_BUDGET) {
        const top = perTool
          .sort((a, b) => b.total - a.total)
          .slice(0, 5)
          .map((t) => `  ${t.name}: ${t.total} (${t.descBytes} desc + ${t.schemaBytes} schema)`)
          .join("\n");
        throw new Error(
          `Tool spec list is ${total} bytes — exceeds budget ${TOOL_LIST_BUDGET}.\nTop 5 by size:\n${top}\nEither compress a description in src/tools/*.ts OR raise TOOL_LIST_BUDGET in tests/prompt-budget.test.ts with a justification.`,
        );
      }
      expect(total).toBeLessThanOrEqual(TOOL_LIST_BUDGET);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("no single tool description exceeds 8 KiB on its own", () => {
    const root = mkdtempSync(join(tmpdir(), "reasonix-budget-"));
    try {
      const tools = buildCodeToolset(root);
      const { perTool } = totalToolBytes(tools);
      const oversized = perTool.filter((t) => t.descBytes > 8 * 1024);
      if (oversized.length > 0) {
        const lines = oversized.map((t) => `  ${t.name}: ${t.descBytes} bytes`).join("\n");
        throw new Error(
          `Tools with descriptions over 8 KiB (every byte ships in every request):\n${lines}\nLong descriptions belong in tool error messages (shown on overrun) or the system prompt (where one copy serves the whole tool list), not in the tool spec.`,
        );
      }
      expect(oversized).toEqual([]);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("codeSystemPrompt with no memory stays under the budget too (defense in depth)", () => {
    const root = mkdtempSync(join(tmpdir(), "reasonix-budget-"));
    try {
      const built = codeSystemPrompt(root);
      expect(built.length).toBeLessThanOrEqual(SYSTEM_PROMPT_BUDGET_WITH_SKILLS);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});
