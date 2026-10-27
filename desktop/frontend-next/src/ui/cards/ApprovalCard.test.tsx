// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
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

describe("a setting the model asks to change", () => {
  const card = (tool: string, allowsSession: boolean) => (
    <ApprovalCard
      item={{ t: "approval", id: "i1", a: { id: "a1", tool, subject: "peers.dispatch", reason: "助手请求把设置 peers.dispatch 从 false 改为 true。", kind: "tool", fresh: true, allowsSession, allowsPersist: false } } as never}
      onApprove={async () => {}} onFullAccess={async () => {}} onPlan={async () => {}}
    />
  );

  it("is titled as a settings change and names the setting, not the internal tool", () => {
    render(card("setting_change", true));
    expect(screen.getByText("请求修改设置")).toBeTruthy();
    expect(screen.queryByText("请求执行权限")).toBeNull();
    expect(screen.queryByText("setting_change")).toBeNull();
    expect(screen.getByText("peers.dispatch")).toBeTruthy();
  });

  it("an elevated one says it is asked every time and offers no session grant", () => {
    render(card("setting_change_elevated", false));
    expect(screen.getByText("每次都会询问")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /本会话/ })).toBeNull();
  });

  it("a preference one does not claim to be asked every time", () => {
    render(card("setting_change", true));
    expect(screen.queryByText("每次都会询问")).toBeNull();
  });
});

describe("a call that repeats text from outside", () => {
  const origin = { id: "o1", tool: "origin_check", subject: "text from web:evil.example reached this call: bash touch x", reasonCode: "origin_text", reason: "this call repeats text from web:evil.example" };

  it("names the class in the reader's words, not the tool id, and shows the call", () => {
    const box = draw(origin);
    const text = box.querySelector(".apv")?.textContent ?? "";
    expect(text).toContain(t("来自外部的文字"));
    expect(text).not.toContain("origin_check");
    expect(text).toContain("web:evil.example");
    expect(text).toContain(t("这条调用里的文字，和本次任务从外部读到的内容（网页或工具结果）逐字相同。自动放行、全部放行和记住的答案都不能代你回答；请只批准这一次调用，或拒绝。"));
  });

  it("does not offer full access, which cannot answer it", () => {
    expect(draw(origin).querySelector('[data-action="decision.full-access"]')).toBeNull();
    expect(draw({ id: "d1", tool: "bash", subject: "x", reasonCode: "dynamic_bash" }).querySelector('[data-action="decision.full-access"]')).not.toBeNull();
  });

  it("offers only a one-time answer", () => {
    const box = draw({ ...origin, allowsSession: false, allowsPersist: false });
    expect(box.querySelector('[data-action="approval.always"], [data-action="approval.session"]')).toBeNull();
  });
});

describe("an approval that opens folders as projects", () => {
  const paths = ["D:\\workspce\\1", "D:\\workspce\\2"];
  const card = () => draw({ id: "o1", tool: "open_projects", subject: "D:\\workspce\\1 (+1)", scope: "open_projects", input: { paths }, fresh: true });

  it("lists every folder it will add, and says it also marks them trusted", () => {
    const box = card();
    const scope = box.querySelector('[data-scope="open_projects"]');
    expect([...(scope?.querySelectorAll("li code") ?? [])].map((c) => c.textContent)).toEqual(paths);
    expect(scope?.textContent).toContain(t("把下面 {n} 个文件夹加入你的项目列表并标记为可信；加入后，会话才能把任务派发到这些项目。没有出现在这里的文件夹不会被加入。", { n: 2 }));
  });

  it("offers no session or always grant, so every batch is its own click", () => {
    const box = card();
    expect(box.querySelector('[data-action="decision.tool"][data-value="once"]')).not.toBeNull();
    expect(box.querySelector('[data-value="session"], [data-value="always"]')).toBeNull();
  });

  it("lists nothing it was not given", () => {
    const box = draw({ id: "o2", tool: "open_projects", subject: "", scope: "open_projects", input: { paths: [3, "/only"] } });
    expect([...box.querySelectorAll("li code")].map((c) => c.textContent)).toEqual(["/only"]);
  });
});
