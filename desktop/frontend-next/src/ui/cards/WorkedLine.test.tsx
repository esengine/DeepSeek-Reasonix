// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "../testkit";
import { WorkedLine } from "./WorkedLine";

afterEach(cleanup);

describe("the worked line", () => {
  it("says the turn is done and how long it ran, in the compact form", () => {
    const box = render(<WorkedLine ms={65_000} />).container;
    expect(box.querySelector(".rc-t")?.textContent).toBe("已完成 · 1m 5s");
    expect(box.querySelector(".rc-tick")).toBeTruthy();
  });

  it("keeps when the turn began for the hover, when the kernel knows", () => {
    const at = new Date(2026, 9, 13, 14, 5).getTime();
    const box = render(<WorkedLine ms={65_000} startedAt={at} />).container;
    expect(box.querySelector(".rc-t")?.textContent).not.toContain("14:05");
    expect(box.querySelector(".rc-ok")?.getAttribute("title")).toContain("14:05 开始");
  });
});
