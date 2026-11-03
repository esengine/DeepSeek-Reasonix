// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import "../testkit";
import { RememberChoice, RememberWhere } from "./RememberChoice";
import { ApprovalCard } from "./ApprovalCard";
import { boot, STORAGE, t } from "../../i18n";
import type { Item } from "../../state/session";

afterEach(() => {
  cleanup();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

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

describe.each(["zh", "en"] as const)("remembered authorization scope in %s", (lang) => {
  it.each([
    ["bash", "同类操作不再询问", "future matching operations will no longer prompt", "权限规则", "permission rule"],
    ["paid_search", "使用这个付费服务不再询问", "future use of this paid service will no longer prompt", "[tools]", "[tools]"],
    ["network_egress", "访问这个域名不再询问", "future access to this domain will no longer prompt", "allowed_domains", "allowed_domains"],
    ["mcp_signin", "同一服务器登录它不再询问", "the same server is not asked about it again", "登录状态", "sign-in state"],
  ])("explains %s scope, lifetime and storage before the user grants access", (tool, zhScope, enScope, zhPlace, enPlace) => {
    localStorage.setItem(STORAGE, lang);
    boot();
    const box = draw({ id: "localized", tool, subject: "target", allowsSession: true, allowsPersist: true });
    const note = box.querySelector(".apv-note")!;
    const text = note.textContent ?? "";
    expect(text).toContain(lang === "zh" ? "这个对话里同类操作不再询问" : "matching operations will no longer prompt in this conversation");
    expect(text).toContain(lang === "zh" ? "随对话保存" : "saved with the conversation");
    expect(text).toContain(lang === "zh" ? "恢复这个对话后仍然有效" : "remains effective when you resume it");
    expect(text).toContain(lang === "zh" ? zhScope : enScope);
    expect(text).toContain(lang === "zh" ? zhPlace : enPlace);
    for (const choice of ["session", "always"]) {
      expect(box.querySelector(`[data-value="${choice}"]`)?.getAttribute("aria-describedby")).toBe(note.id);
    }
    expect([...box.querySelectorAll(".apv-ft button")].map((b) => b.getAttribute("data-value"))).toEqual(["session", "always", "deny", "once"]);
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
    expect(note).toContain(t("“本会话允许”：这个对话里同类操作不再询问，随对话保存，恢复这个对话后仍然有效。"));
    expect(note).toContain(t("“始终允许”：之后同类操作不再询问，写入用户配置的权限规则，可在设置 → 工具与权限里撤销。"));
    expect(both.querySelector(".apv-note")!.compareDocumentPosition(both.querySelector(".apv-ft")!)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  });

  it("answers with the remembered verdict", () => {
    const approve = vi.fn(async () => {});
    const box = draw({ id: "a3", tool: "bash", subject: "ls", allowsSession: true, allowsPersist: true }, approve);
    fireEvent.click(box.querySelector('[data-value="session"]')!);
    expect(approve).toHaveBeenCalledWith("i1", "a3", "session");
  });

  it.each([
    ["paid_search", "[tools]"],
    ["network_egress", "allowed_domains"],
    ["mcp_signin", "登录状态"],
  ])("keeps %s grant storage guidance in the compact card", (tool, place) => {
    const box = draw({ id: "scoped", tool, subject: "target", allowsSession: true, allowsPersist: true });
    const note = box.querySelector(".apv-note")?.textContent ?? "";
    expect(note).toContain("随对话保存");
    expect(note).toContain("恢复这个对话后仍然有效");
    expect(note).toContain(place);
    expect(note).not.toContain("权限规则");
    expect(box.querySelectorAll(".apv-ft button")).toHaveLength(4);
  });

  it.each(["session", "always", "deny", "once"] as const)("keeps the %s decision's original callback", (verdict) => {
    const approve = vi.fn(async () => {});
    const box = draw({ id: "a4", tool: "bash", subject: "ls", allowsSession: true, allowsPersist: true }, approve);
    expect([...box.querySelectorAll(".apv-ft button")].map((b) => b.getAttribute("data-value"))).toEqual(["session", "always", "deny", "once"]);
    fireEvent.click(box.querySelector(`[data-value="${verdict}"]`)!);
    expect(approve).toHaveBeenCalledExactlyOnceWith("i1", "a4", verdict);
  });
});
