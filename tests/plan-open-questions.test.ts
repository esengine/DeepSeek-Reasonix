import { describe, expect, it } from "vitest";
import {
  extractOpenQuestionsSection,
  hasOpenQuestionsSection,
} from "../src/cli/ui/plan-open-questions.js";

describe("extractOpenQuestionsSection", () => {
  it("returns null when no matching heading exists", () => {
    const plan = "## Summary\nrefactor X\n## Steps\n1. do it";
    expect(extractOpenQuestionsSection(plan)).toBeNull();
    expect(hasOpenQuestionsSection(plan)).toBe(false);
  });

  it("extracts an Open Questions block from the heading to end of plan", () => {
    const plan = [
      "## Summary",
      "swap backend",
      "",
      "## Open Questions",
      "- which adapter wins on tie?",
      "- keep deprecated env var?",
    ].join("\n");
    const block = extractOpenQuestionsSection(plan);
    expect(block).toContain("Open Questions");
    expect(block).toContain("which adapter wins on tie?");
    expect(block).toContain("keep deprecated env var?");
  });

  it("stops at the next same-level heading", () => {
    const plan = [
      "## Summary",
      "swap backend",
      "",
      "## Risks",
      "- breaking config",
      "- migration runs hot",
      "",
      "## Steps",
      "1. ship it",
    ].join("\n");
    const block = extractOpenQuestionsSection(plan);
    expect(block).toContain("breaking config");
    expect(block).toContain("migration runs hot");
    expect(block).not.toContain("ship it");
  });

  it("includes deeper sub-headings nested under the questions block", () => {
    const plan = ["## Open Questions", "- top", "### sub", "- nested", "## Steps", "1. go"].join(
      "\n",
    );
    const block = extractOpenQuestionsSection(plan);
    expect(block).toContain("nested");
    expect(block).not.toContain("1. go");
  });

  it("matches Chinese headings", () => {
    const plan = ["## 待确认", "- 边界 A 还是 B?", "## 步骤", "1. 走起"].join("\n");
    const block = extractOpenQuestionsSection(plan);
    expect(block).toContain("边界 A 还是 B?");
    expect(block).not.toContain("走起");
  });

  it("matches case-insensitively and through plural variants", () => {
    expect(hasOpenQuestionsSection("# RISK\n- thing")).toBe(true);
    expect(hasOpenQuestionsSection("### unknowns\n- x")).toBe(true);
    expect(hasOpenQuestionsSection("## Assumption\n- y")).toBe(true);
  });

  it("does not match the literal word inside body text", () => {
    expect(hasOpenQuestionsSection("## Summary\nthis lists open questions later")).toBe(false);
  });
});
