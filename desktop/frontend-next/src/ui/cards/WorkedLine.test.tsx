// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "../testkit";
import { WorkedLine } from "./WorkedLine";
import { t } from "../../i18n";

afterEach(cleanup);

describe("the worked line", () => {
  it("says how long the turn ran, in the compact form", () => {
    const box = render(<WorkedLine ms={65_000} />).container;
    expect(box.querySelector(".rc-t")?.textContent).toBe(t("chat.worked_for_t", { t: "1m 5s" }));
    expect(box.textContent).toContain("1m 5s");
  });

  it("says when the turn began when the kernel knows", () => {
    const at = new Date(2026, 9, 13, 14, 5).getTime();
    const box = render(<WorkedLine ms={65_000} startedAt={at} />).container;
    expect(box.querySelector(".rc-t")?.textContent).toContain("1m 5s");
    expect(box.querySelector(".rc-t")?.textContent).toContain("14:05");
  });
});
