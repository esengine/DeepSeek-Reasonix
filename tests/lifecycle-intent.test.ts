import { describe, expect, it } from "vitest";
import { evaluateEngineeringLifecycleIntent } from "../src/code/lifecycle-intent.js";

describe("engineering lifecycle intent evaluator", () => {
  it.each([
    {
      name: "read-only explanation",
      input: "Explain what the package.json scripts do. Do not edit files.",
      recommendation: "stay-light",
      signals: ["read-only"],
    },
    {
      name: "small single-file fix",
      input: "Fix a typo in README.md.",
      recommendation: "stay-light",
      signals: ["small-task", "single-file"],
    },
    {
      name: "multi-file API rename",
      input: "Rename the createClient API across src, update imports, and run tests.",
      recommendation: "strict-candidate",
      signals: ["multi-file", "refactor"],
    },
    {
      name: "module move with references",
      input: "Move the parser module, update all references, and refresh the tests.",
      recommendation: "strict-candidate",
      signals: ["multi-file", "refactor"],
    },
    {
      name: "dependency migration",
      input: "Upgrade zod, update package.json and pnpm-lock.yaml, then run npm install.",
      recommendation: "strict-candidate",
      signals: ["dependency-change", "package-or-config"],
    },
    {
      name: "configuration migration",
      input: "Migrate tsconfig and the GitHub Actions workflow for the new build.",
      recommendation: "strict-candidate",
      signals: ["migration", "package-or-config"],
    },
    {
      name: "failed verification revise flow",
      input: "Tests failed after step 2; revise_plan should replace the remaining steps.",
      recommendation: "strict-candidate",
      signals: ["failed-verification", "plan-revision"],
    },
    {
      name: "explicit strict lifecycle",
      input: "/plan strict refactor the shell gate and update the lifecycle tests",
      recommendation: "strict-candidate",
      signals: ["explicit-strict", "refactor"],
    },
    {
      name: "broad cleanup asks for plan but does not auto-enforce",
      input: "Clean up the routing layer across modules.",
      recommendation: "suggest-plan",
      signals: ["multi-file"],
    },
    {
      name: "cancel intent stays lightweight",
      input: "Stop the current lifecycle task and cancel the plan.",
      recommendation: "stay-light",
      signals: ["cancel"],
    },
    {
      name: "Chinese multi-file refactor",
      input: "把 API 模块移动到新的目录，更新所有引用并跑测试。",
      recommendation: "strict-candidate",
      signals: ["multi-file", "refactor"],
    },
    {
      name: "Chinese small fix",
      input: "修一下 README 里的一个拼写错误。",
      recommendation: "stay-light",
      signals: ["small-task", "single-file"],
    },
  ])("classifies $name", ({ input, recommendation, signals }) => {
    const result = evaluateEngineeringLifecycleIntent(input);

    expect(result.recommendation).toBe(recommendation);
    expect(result.signals).toEqual(expect.arrayContaining(signals));
  });

  it("keeps lifecycle intent evaluation advisory-only", () => {
    const result = evaluateEngineeringLifecycleIntent("Refactor the parser across modules.");

    expect(result.modelVisible).toBe(false);
    expect(result.enforced).toBe(false);
  });
});
