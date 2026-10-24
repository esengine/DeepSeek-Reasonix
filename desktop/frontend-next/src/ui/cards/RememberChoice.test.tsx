// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import "../testkit";
import { RememberChoice, RememberWhere } from "./RememberChoice";
import { ApprovalCard } from "./ApprovalCard";
import { t } from "../../i18n";
import type { Item } from "../../state/session";

afterEach(cleanup);

describe("RememberChoice", () => {
  it("draws only what the kernel will honour and reports the choice", () => {
    const choose = vi.fn();
    const box = render(<RememberChoice options={["always"]} target="a1" busy="" disabled={false} onChoose={choose} />).container;
    expect(box.querySelectorAll("button")).toHaveLength(1);
    const button = box.querySelector("button")!;
    expect(button.dataset.value).toBe("always");
    expect(button.dataset.target).toBe("a1");
    fireEvent.click(button);
    expect(choose).toHaveBeenCalledWith("always");
  });

  it("draws nothing when there is nothing to remember", () => {
    expect(render(<RememberChoice options={[]} target="a1" busy="" disabled={false} onChoose={() => {}} />).container.innerHTML).toBe("");
    expect(render(<RememberWhere options={[]} target="a1" />).container.innerHTML).toBe("");
  });

  it("says where each remembered choice is kept", () => {
    const both = render(<RememberWhere options={["session", "always"]} target="a1" />).container.textContent ?? "";
    expect(both).toContain("随对话保存");
    expect(both).toContain("权限规则");
    expect(render(<RememberWhere options={["session"]} target="a2" />).container.textContent).not.toContain("权限规则");
  });
});

describe("RememberWhere for the questions that keep their answer somewhere other than a permission rule", () => {
  const where = (tool: string, options: ("session" | "always")[] = ["session", "always"]) =>
    render(<RememberWhere options={options} target="a1" tool={tool} />).container.textContent ?? "";

  it.each([
    ["paid_search", "[tools]"],
    ["network_egress", "allowed_domains"],
    ["mcp_signin", "登录状态"],
  ])("%s says where an always answer is kept, and does not call it a permission rule", (tool, place) => {
    const note = where(tool);
    expect(note).toContain(place);
    expect(note).not.toContain("权限规则");
    expect(note).toContain("随对话保存");
  });

  it("keeps the permission-rule sentence for an ordinary tool", () => {
    expect(where("bash")).toContain("权限规则");
  });

  it("says nothing about a place when only the session choice is offered", () => {
    expect(where("paid_search", ["session"])).not.toContain("[tools]");
  });
});

type Approval = Extract<Item, { t: "approval" }>;
const draw = (a: Approval["a"], onApprove = vi.fn(async () => {})) =>
  render(<ApprovalCard item={{ t: "approval", id: "i1", a }} onApprove={onApprove} onFullAccess={async () => {}} onPlan={async () => {}} />).container;

describe("the approval card's footer", () => {
  it("offers the session and always choices only where the kernel grants them", () => {
    const none = draw({ id: "a1", tool: "bash", subject: "ls" });
    expect(none.querySelector('[data-value="session"]')).toBeNull();
    expect(none.querySelector('[data-value="always"]')).toBeNull();
    expect(none.querySelector(".apv-note")).toBeNull();
    cleanup();
    const both = draw({ id: "a2", tool: "bash", subject: "ls", allowsSession: true, allowsPersist: true });
    expect(both.querySelector('[data-value="session"]')).not.toBeNull();
    expect(both.querySelector('[data-value="always"]')).not.toBeNull();
    const note = both.querySelector(".apv-note")?.textContent ?? "";
    expect(note).toContain(t("“本会话允许”只对这个对话有效，随对话保存，恢复这个对话后仍然有效。"));
    expect(note).toContain(t("“始终允许”写入用户配置的权限规则，可在设置 → 工具与权限里撤销。"));
  });

  it("answers with the remembered verdict", () => {
    const approve = vi.fn(async () => {});
    const box = draw({ id: "a3", tool: "bash", subject: "ls", allowsSession: true, allowsPersist: true }, approve);
    fireEvent.click(box.querySelector('[data-value="session"]')!);
    expect(approve).toHaveBeenCalledWith("i1", "a3", "session");
  });
});
