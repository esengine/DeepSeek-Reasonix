import React from "react";
import { describe, expect, it } from "vitest";
import { PlanConfirm } from "../src/cli/ui/PlanConfirm.js";
import { CharPool } from "../src/renderer/pools/char-pool.js";
import { HyperlinkPool } from "../src/renderer/pools/hyperlink-pool.js";
import { StylePool } from "../src/renderer/pools/style-pool.js";
import { mount } from "../src/renderer/reconciler/mount.js";

async function bytesFor(plan: string, steps?: { id: string; title: string }[]): Promise<string> {
  const chunks: string[] = [];
  const handle = mount(<PlanConfirm plan={plan} steps={steps as never} onChoose={() => {}} />, {
    viewportWidth: 80,
    viewportHeight: 30,
    pools: { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() },
    write: (s) => {
      chunks.push(s);
    },
  });
  await new Promise((r) => setTimeout(r, 100));
  const out = chunks.join("");
  handle.destroy();
  return out;
}

describe("PlanConfirm — issue #336 plan body must be visible", () => {
  it("renders the markdown body when no steps are supplied", async () => {
    const plan = [
      "## Summary",
      "Refactor `web_search` to support multiple backends",
      "",
      "## Steps",
      "1. add adapter interface",
      "2. wire env-var dispatch",
    ].join("\n");
    const out = await bytesFor(plan);
    expect(out).toContain("Refactor");
    expect(out).toContain("adapter interface");
    expect(out).toContain("Approve plan");
  });

  it("falls back to step list when steps are present", async () => {
    const plan = "## Summary\nbackend swap";
    const steps = [
      { id: "s1", title: "step one" },
      { id: "s2", title: "step two" },
    ];
    const out = await bytesFor(plan, steps);
    expect(out).toContain("step one");
    expect(out).toContain("step two");
  });

  it("truncates very long plans and surfaces the overflow hint", async () => {
    const longPlan = Array.from({ length: 50 }, (_, i) => `line ${i + 1}`).join("\n");
    const out = await bytesFor(longPlan);
    expect(out).toContain("line 1");
    expect(out).toMatch(/more line.*scrollback/);
  });
});
