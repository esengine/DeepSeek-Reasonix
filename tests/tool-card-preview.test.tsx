/** ToolCard preview rendering for elided command output and pinned failure lines. */

import React from "react";
import { describe, expect, it } from "vitest";
import { ToolCard } from "../src/cli/ui/cards/ToolCard.js";
import type { ToolCard as ToolCardData } from "../src/cli/ui/state/cards.js";
import { render } from "./helpers/ink-test.js";

function shellCard(output: string): ToolCardData {
  return {
    id: "tool-1",
    ts: 0,
    kind: "tool",
    name: "run_command",
    args: "npm test",
    output,
    done: true,
    exitCode: 1,
    elapsedMs: 510,
  };
}

describe("ToolCard failure preview", () => {
  it("shows the full-output shortcut only once across multiple hidden gaps", () => {
    const output = [
      "$ npm test",
      "[exit 1]",
      "> node test.mjs",
      "AssertionError [ERR_ASSERTION]: VIP25 should reduce cart",
      "    at file:///repo/test.mjs:5:8",
      "    at ModuleJob.run (node:internal/modules/esm/module_job:343:25)",
      "actual: 10200",
      "expected: 9000",
      "operator: strictEqual",
      "}",
      "Node.js v22.22.0",
    ].join("\n");

    const { lastFrame, unmount } = render(<ToolCard card={shellCard(output)} />);
    const frame = lastFrame() ?? "";

    expect(frame).toContain("AssertionError [ERR_ASSERTION]");
    expect(frame).toContain("actual: 10200");
    expect(frame).toContain("expected: 9000");
    expect(frame.match(/Ctrl\+R for full output/g)?.length).toBe(1);
    unmount();
  });
});
