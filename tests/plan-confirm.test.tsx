import { Box, Text } from "ink";
import { render } from "ink-testing-library";
import React from "react";
import stripAnsi from "strip-ansi";
import { describe, expect, it } from "vitest";
import { PlanConfirm } from "../src/cli/ui/PlanConfirm.js";
import { ViewportBudgetProvider, useReserveRows } from "../src/cli/ui/layout/viewport-budget.js";
import { makeFakeStdin, makeFakeStdout } from "./helpers/ink-stdio.js";

function bytesFor(plan: string, steps?: { id: string; title: string }[]): string {
  const { lastFrame, unmount } = render(
    <PlanConfirm plan={plan} steps={steps as never} onChoose={() => {}} />,
  );
  const out = lastFrame() ?? "";
  unmount();
  return out;
}

function ModalHost({ children }: { children: React.ReactNode }): React.ReactElement {
  useReserveRows("stream", { min: 0, max: 12 });
  return (
    <Box flexDirection="row" height={30}>
      <Box flexDirection="column" flexGrow={1}>
        <Box flexDirection="column" flexGrow={1}>
          <Text>top</Text>
        </Box>
        {children}
      </Box>
    </Box>
  );
}

async function nextFrame(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

function blankLinesBetween(out: string, before: string, after: string): number {
  const clean = stripAnsi(out);
  const lines = clean.split("\n");
  const start = lines.findIndex((line) => line.includes(before));
  const end = lines.findIndex((line, i) => i > start && line.includes(after));
  expect(start).toBeGreaterThanOrEqual(0);
  expect(end).toBeGreaterThan(start);
  return lines.slice(start + 1, end).filter((line) => line.trim().length === 0).length;
}

describe("PlanConfirm — issue #336 plan body must be visible", () => {
  it("renders only a summary by default and expands body on Ctrl+P", async () => {
    const plan = [
      "## Summary",
      "Refactor `web_search` to support multiple backends",
      "",
      "## Steps",
      "1. add adapter interface",
      "2. wire env-var dispatch",
    ].join("\n");
    const { lastFrame, stdin, unmount } = render(
      <PlanConfirm plan={plan} steps={[]} onChoose={() => {}} />,
    );
    expect(lastFrame() ?? "").toContain("Summary");
    expect(lastFrame() ?? "").not.toContain("adapter interface");
    stdin.write("\x10");
    await nextFrame();
    expect(lastFrame() ?? "").toContain("adapter interface");
    stdin.write("\x10");
    await nextFrame();
    expect(lastFrame() ?? "").not.toContain("adapter interface");
    unmount();
  });

  it("renders the markdown body when no steps are supplied after expand", async () => {
    const plan = [
      "## Summary",
      "Refactor `web_search` to support multiple backends",
      "",
      "## Steps",
      "1. add adapter interface",
      "2. wire env-var dispatch",
    ].join("\n");
    const { lastFrame, stdin, unmount } = render(
      <PlanConfirm plan={plan} steps={[]} onChoose={() => {}} />,
    );
    stdin.write("\x10");
    await nextFrame();
    const out = lastFrame() ?? "";
    expect(out).toContain("Refactor");
    expect(out).toContain("adapter interface");
    expect(out).toContain("Approve plan");
    unmount();
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

  it("bounds very long expanded plans and gives detail scrolling priority over the picker", async () => {
    const longPlan = Array.from({ length: 50 }, (_, i) => `line ${i + 1}`).join("\n");
    const choices: string[] = [];
    const { lastFrame, stdin, unmount } = render(
      <PlanConfirm plan={longPlan} steps={[]} onChoose={(choice) => choices.push(choice)} />,
    );
    expect(lastFrame() ?? "").toContain("line 1");
    expect(lastFrame() ?? "").not.toContain("line 20");
    stdin.write("\x10");
    await nextFrame();
    const expanded = lastFrame() ?? "";
    expect(expanded).toContain("showing lines 1-");
    expect(expanded).toContain("line 1");
    expect(expanded).not.toContain("line 50");
    stdin.write("\u001B[6~");
    await nextFrame();
    const scrolled = lastFrame() ?? "";
    expect(scrolled).toContain("showing lines");
    expect(scrolled).not.toContain("showing lines 1-");
    stdin.write("\r");
    await nextFrame();
    expect(choices).toEqual(["approve"]);
    unmount();
  });

  it("uses the allocated expanded detail height before any detail scroll", async () => {
    const longPlan = Array.from({ length: 50 }, (_, i) => `line ${i + 1}`).join("\n");
    const stdout = makeFakeStdout();
    const { lastFrame, stdin, unmount } = render(
      <PlanConfirm plan={longPlan} steps={[]} onChoose={() => {}} />,
      { stdout: stdout as never, stdin: makeFakeStdin() as never },
    );
    stdin.write("\x10");
    await nextFrame();
    await nextFrame();
    const expanded = lastFrame() ?? "";
    expect(expanded).toContain("showing lines 1-24 of 50");
    expect(expanded).toContain("line 24");
    expect(expanded).not.toContain("line 25");
    expect(
      blankLinesBetween(expanded, "PgUp/PgDn scroll details · Home/End jump", "▸ accept"),
    ).toBeLessThanOrEqual(1);
    unmount();
  });

  it("keeps expanded action options tight after detail scrolling", async () => {
    const longPlan = Array.from({ length: 50 }, (_, i) => `line ${i + 1}`).join("\n");
    const { lastFrame, stdin, unmount } = render(
      <PlanConfirm plan={longPlan} steps={[]} onChoose={() => {}} />,
    );
    stdin.write("\x10");
    await nextFrame();
    stdin.write("\u001B[6~");
    await nextFrame();
    const scrolled = lastFrame() ?? "";
    expect(scrolled).toContain("showing lines");
    expect(scrolled).not.toContain("showing lines 1-");
    expect(
      blankLinesBetween(scrolled, "PgUp/PgDn scroll details · Home/End jump", "▸ accept"),
    ).toBeLessThanOrEqual(1);
    unmount();
  });

  it("arrow keys scroll plan content, not options, when details are expanded", async () => {
    const longPlan = Array.from({ length: 50 }, (_, i) => `line ${i + 1}`).join("\n");
    const choices: string[] = [];
    const { lastFrame, stdin, unmount } = render(
      <PlanConfirm plan={longPlan} steps={[]} onChoose={(choice) => choices.push(choice)} />,
    );
    stdin.write("\x10");
    await nextFrame();
    const expanded = lastFrame() ?? "";
    expect(expanded).toContain("showing lines 1-");
    stdin.write("\u001B[B");
    await nextFrame();
    const scrolled = lastFrame() ?? "";
    expect(scrolled).not.toContain("showing lines 1-");
    expect(scrolled).toContain("showing lines");
    stdin.write("\r");
    await nextFrame();
    expect(choices).toEqual(["approve"]);
    unmount();
  });

  it("renders plan choice labels and descriptions on the same row", () => {
    const out = bytesFor("## Summary\nbackend swap", []);
    expect(out).toMatch(/accept\s+run it now, in order/);
    expect(out).toMatch(/refine\s+give the agent more guidance, draft a new plan/);
    expect(out).toMatch(/revise\s+edit the plan inline before running \(skip \/ reorder steps\)/);
    expect(out).toMatch(/reject\s+discard, agent will retry from scratch/);
  });

  it("handles Ctrl+P safely when no plan body exists", async () => {
    const { lastFrame, stdin, unmount } = render(
      <PlanConfirm plan="" steps={[]} onChoose={() => {}} />,
    );
    expect(lastFrame() ?? "").toContain("No plan body submitted yet.");
    stdin.write("\x10");
    await nextFrame();
    expect(lastFrame() ?? "").toContain("No plan body submitted yet.");
    unmount();
  });

  it("surfaces the open-questions block even when steps are present (issue #477)", () => {
    const plan = [
      "## Summary",
      "swap backend",
      "",
      "## Open Questions",
      "- which adapter wins on tie?",
      "- keep deprecated env var?",
    ].join("\n");
    const steps = [{ id: "s1", title: "do thing" }];
    const out = bytesFor(plan, steps);
    expect(out).toContain("which adapter wins on tie?");
    expect(out).toContain("keep deprecated env var?");
    expect(out).toContain("do thing");
  });

  it("does not jump when scrolling into a blank line at the top of the detail window", async () => {
    const planLines = Array.from({ length: 241 }, (_, i) => `line ${i + 1}`);
    planLines[96] = "detail preface";
    planLines[97] = "";
    planLines[98] = "status marker";
    planLines[99] = "";
    planLines[100] = "showing lines 97-126 of 241";
    planLines[101] = "";
    planLines[102] = "- route segment `[slug]` matches the file name";
    planLines[103] = "- **`generateStaticParams()`** builds static article pages";
    planLines[104] = "- **`generateMetadata()`** derives title and description";
    const plan = planLines.join("\n");
    const { lastFrame, stdin, unmount } = render(
      <PlanConfirm plan={plan} steps={[]} onChoose={() => {}} />,
    );
    stdin.write("\x10");
    await nextFrame();
    for (let i = 0; i < 97; i++) stdin.write("[B");
    await nextFrame();
    const before = lastFrame() ?? "";
    stdin.write("[B");
    await nextFrame();
    const after = lastFrame() ?? "";
    const cleanBefore = stripAnsi(before);
    const cleanAfter = stripAnsi(after);
    expect(cleanBefore).toContain("showing lines 99-");
    expect(cleanBefore).toContain("- route segment `[slug]` matches the file name");
    expect(cleanAfter).toContain("showing lines 99-");
    expect(cleanAfter).toContain("- route segment `[slug]` matches the file name");
    expect(cleanAfter).toBe(cleanBefore);
    unmount();
  });

  it("keeps the expanded detail header intact inside the app-style modal stack", async () => {
    const planLines = Array.from({ length: 277 }, (_, i) => `line ${i + 1}`);
    planLines[158] = "**`src/components/Pagination.tsx`**";
    planLines[159] = "- 上一页 / 下一页按钮 of 277";
    planLines[160] = "- 页码数字（显示前后各 2 页，中间用 ... 省略）";
    planLines[161] = "- 禁用状态（第一页/最后一页）";
    const plan = planLines.join("\n");
    const { lastFrame, stdin, unmount } = render(
      <ViewportBudgetProvider initialRows={30}>
        <ModalHost>
          <PlanConfirm plan={plan} steps={[]} onChoose={() => {}} />
        </ModalHost>
      </ViewportBudgetProvider>,
    );
    stdin.write("\x10");
    await nextFrame();
    for (let i = 0; i < 158; i++) stdin.write("[B");
    await nextFrame();
    const out = lastFrame() ?? "";
    const clean = stripAnsi(out);
    expect(
      clean.split("\n").some((line) => line.trimStart().startsWith("showing lines 159-")),
    ).toBe(true);
    expect(out).toContain("- 上一页 / 下一页按钮 of 277");
    unmount();
  });
});
