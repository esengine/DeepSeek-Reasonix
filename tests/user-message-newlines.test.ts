import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

describe("user message newline rendering", () => {
  it("preserves whitespace in desktop and dashboard user bubbles", () => {
    const desktopCss = readFileSync("desktop/src/styles.css", "utf8");
    const dashboardCss = readFileSync("dashboard/src/styles.css", "utf8");

    expect(desktopCss).toContain(".msg.user .msg-text");
    expect(desktopCss).toContain("white-space: pre-wrap");
    expect(dashboardCss).toContain(".msg.user .msg-text");
    expect(dashboardCss).toContain("white-space: pre-wrap");
  });
});
