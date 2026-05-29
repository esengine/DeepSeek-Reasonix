import { describe, expect, it } from "vitest";
import {
  detectLifecyclePlanSuggestion,
  shouldSuggestPlanForEngineeringIntent,
} from "../src/cli/ui/lifecycle-plan-suggestion.js";

describe("lifecycle plan suggestion intent", () => {
  it.each([
    "Refactor multiple files to extract the auth module",
    "Rename the user API module and update imports across the app",
    "Move the billing module into packages/core and update references",
    "Migrate package.json, pnpm-lock.yaml, and tsconfig for the new dependency",
    "重构多个文件，把认证逻辑抽到新模块",
    "移动用户模块并更新所有引用",
    "迁移依赖和配置，更新 package.json 与 pnpm-lock.yaml",
  ])("suggests plan-first rails for high-risk engineering prompts: %s", (text) => {
    expect(detectLifecyclePlanSuggestion(text)).toBe(true);
  });

  it.each([
    "Explain how tsconfig.json works",
    "I came across a bug in the API route",
    "Please address this issue",
    "Add address field to the API response",
    "Fix a typo in README.md",
    "Use lifecycle hooks to track mounted state",
    "修复一个拼写错误",
    "这个装修流程怎么优化",
    "这门课是必修吗",
    "维修登录页面的文案",
  ])("does not suggest rails for ambiguous or lightweight prompts: %s", (text) => {
    expect(detectLifecyclePlanSuggestion(text)).toBe(false);
  });

  it("only suggests in code mode while plan/lifecycle rails are inactive", () => {
    expect(
      shouldSuggestPlanForEngineeringIntent({
        text: "Refactor multiple files to extract the auth module",
        codeMode: true,
        planMode: false,
        lifecycleMode: "off",
      }),
    ).toBe(true);

    expect(
      shouldSuggestPlanForEngineeringIntent({
        text: "Refactor multiple files to extract the auth module",
        codeMode: false,
        planMode: false,
        lifecycleMode: "off",
      }),
    ).toBe(false);

    expect(
      shouldSuggestPlanForEngineeringIntent({
        text: "Refactor multiple files to extract the auth module",
        codeMode: true,
        planMode: true,
        lifecycleMode: "strict",
      }),
    ).toBe(false);

    expect(
      shouldSuggestPlanForEngineeringIntent({
        text: "Refactor multiple files to extract the auth module",
        codeMode: true,
        planMode: false,
        lifecycleMode: "strict",
      }),
    ).toBe(false);
  });

  it("does not repeat the suggestion after it was already shown in the session", () => {
    const request = {
      text: "Refactor multiple files to extract the auth module",
      codeMode: true,
      planMode: false,
      lifecycleMode: "off",
      alreadySuggested: true,
    } as const;

    expect(shouldSuggestPlanForEngineeringIntent(request)).toBe(false);
  });
});
