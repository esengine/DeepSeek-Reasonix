import { render } from "ink-testing-library";
import React from "react";
import { describe, expect, it } from "vitest";
import { PlanConfirm } from "../src/cli/ui/PlanConfirm.js";

function bytesFor(plan: string, steps?: { id: string; title: string }[]): string {
  const { lastFrame, unmount } = render(
    <PlanConfirm plan={plan} steps={steps as never} onChoose={() => {}} />,
  );
  const out = lastFrame() ?? "";
  unmount();
  return out;
}

describe("PlanConfirm — issue #336 plan body must be visible", () => {
  it("renders the markdown body when no steps are supplied", () => {
    const plan = [
      "## Summary",
      "Refactor `web_search` to support multiple backends",
      "",
      "## Steps",
      "1. add adapter interface",
      "2. wire env-var dispatch",
    ].join("\n");
    const out = bytesFor(plan);
    expect(out).toContain("Refactor");
    expect(out).toContain("adapter interface");
    expect(out).toContain("Approve plan");
  });

  it("falls back to step list when steps are present", () => {
    const plan = "## Summary\nbackend swap";
    const steps = [
      { id: "s1", title: "step one" },
      { id: "s2", title: "step two" },
    ];
    const out = bytesFor(plan, steps);
    expect(out).toContain("step one");
    expect(out).toContain("step two");
  });

  it("truncates very long plans and surfaces the overflow hint", () => {
    const longPlan = Array.from({ length: 50 }, (_, i) => `line ${i + 1}`).join("\n");
    const out = bytesFor(longPlan);
    expect(out).toContain("line 1");
    expect(out).toMatch(/more line.*scrollback/);
  });
});
