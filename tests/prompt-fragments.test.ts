/** escalationContract — model-aware contract so the system prompt names the actual tier (#582). */

import { describe, expect, it } from "vitest";
import { ESCALATION_CONTRACT, escalationContract } from "../src/prompt-fragments.js";

describe("escalationContract (#582)", () => {
  it("interpolates the actual model id for non-pro tiers", () => {
    const out = escalationContract("deepseek-v4-flash");
    expect(out).toContain("`deepseek-v4-flash`");
    expect(out).toContain("If asked which model you are, answer `deepseek-v4-flash`");
    expect(out).toContain("<<<NEEDS_PRO");
  });

  it("returns the no-escalation note for the pro tier instead of the full ladder", () => {
    const out = escalationContract("deepseek-v4-pro");
    expect(out).toContain("`deepseek-v4-pro`");
    expect(out).toContain("escalation tier");
    expect(out).toContain("If asked which model you are, answer `deepseek-v4-pro`");
    expect(out).not.toContain("<<<NEEDS_PRO: <one-sentence reason>>>>");
  });

  it("never tells a pro session it is running on flash (regression for #582)", () => {
    const out = escalationContract("deepseek-v4-pro");
    expect(out).not.toMatch(/running on `?deepseek-v4-flash`?/);
  });

  it("backward-compat const matches the historical flash phrasing", () => {
    expect(ESCALATION_CONTRACT).toBe(escalationContract("deepseek-v4-flash"));
  });

  it("treats unknown future tiers as non-pro (full contract, name themselves)", () => {
    const out = escalationContract("deepseek-v5-experimental");
    expect(out).toContain("`deepseek-v5-experimental`");
    expect(out).toContain("<<<NEEDS_PRO");
  });
});
