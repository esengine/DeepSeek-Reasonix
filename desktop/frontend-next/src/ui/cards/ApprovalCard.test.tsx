// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "../testkit";
import { ApprovalCard } from "./ApprovalCard";
import { t } from "../../i18n";
import type { Item } from "../../state/session";

afterEach(cleanup);

type Approval = Extract<Item, { t: "approval" }>;
const noop = async () => {};

const draw = (a: Approval["a"]) =>
  render(<ApprovalCard item={{ t: "approval", id: "i1", a }} onApprove={noop} onFullAccess={noop} onPlan={noop} />).container;

// Approving best_of_n lets its attempts run unattended; the person clicking has
// to see that, and the task, before the click rather than after.
describe("an approval that covers more than its call", () => {
  it("says what else approving starts, how many, and on what task", () => {
    const box = draw({ id: "a1", tool: "best_of_n", subject: "", scope: "unattended_attempts", input: { prompt: "add Median to calc.go", n: 2 } });
    const text = box.querySelector(".apv")?.textContent ?? "";
    expect(box.querySelector('[data-scope="unattended_attempts"]')).not.toBeNull();
    expect(text).toContain(t("并行启动 {n} 个候选", { n: 2 }));
    expect(text).toContain("add Median to calc.go");
  });

  it("still admits to a scope it cannot describe", () => {
    const box = draw({ id: "a2", tool: "x", subject: "", scope: "something_new" });
    expect(box.querySelector('[data-scope="something_new"]')?.textContent).toContain(t("这次批准的效力超出这一次调用本身。"));
  });

  it("adds nothing to an ordinary approval", () => {
    expect(draw({ id: "a3", tool: "bash", subject: "go test ./..." }).querySelector("[data-scope]")).toBeNull();
  });
});
