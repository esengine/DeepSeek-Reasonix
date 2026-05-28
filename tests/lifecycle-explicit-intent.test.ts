import { describe, expect, it } from "vitest";
import {
  detectExplicitPlanFirstIntent,
  shouldEnterPlanModeForExplicitIntent,
} from "../src/cli/ui/lifecycle-explicit-intent.js";

describe("explicit lifecycle plan intent", () => {
  it.each([
    "请先规划再修改代码",
    "先给我一个实现方案，不要直接改代码",
    "先不要动代码，先做计划",
    "Use strict lifecycle for this refactor",
    "Enable plan mode before editing files",
    "Please draft an implementation plan before changing code",
  ])("detects explicit plan-first prompts: %s", (text) => {
    expect(detectExplicitPlanFirstIntent(text)).toBe(true);
  });

  it.each([
    "解释一下 tsconfig.json 是怎么工作的",
    "修复一个拼写错误",
    "I came across a bug in the API route",
    "Please address this issue",
    "Add address field to the API response",
    "What is the plan for understanding package.json?",
    "Use lifecycle hooks to track mounted state",
  ])("does not treat ambiguous wording as explicit plan-first intent: %s", (text) => {
    expect(detectExplicitPlanFirstIntent(text)).toBe(false);
  });

  it("only asks the host to enter plan mode in code mode when plan mode is off", () => {
    expect(
      shouldEnterPlanModeForExplicitIntent({
        text: "先给我一个计划，不要直接改代码",
        codeMode: true,
        planMode: false,
      }),
    ).toBe(true);

    expect(
      shouldEnterPlanModeForExplicitIntent({
        text: "先给我一个计划，不要直接改代码",
        codeMode: false,
        planMode: false,
      }),
    ).toBe(false);

    expect(
      shouldEnterPlanModeForExplicitIntent({
        text: "先给我一个计划，不要直接改代码",
        codeMode: true,
        planMode: true,
      }),
    ).toBe(false);
  });
});
