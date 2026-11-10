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
    expect(text).toContain(t("chat.starts_n_attempts_in_parallel", { n: 2 }));
    expect(text).toContain("add Median to calc.go");
  });

  it("still admits to a scope it cannot describe", () => {
    const box = draw({ id: "a2", tool: "x", subject: "", scope: "something_new" });
    expect(box.querySelector('[data-scope="something_new"]')?.textContent).toContain(t("chat.this_approval_covers_more_than"));
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
    expect(text).toContain(t("chat.text_from_outside"));
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
    expect(scope?.textContent).toContain(t("chat.adds_these_n_folders_to", { n: 2 }));
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

describe("a write outside the folders the agent may change", () => {
  const card = (over: Partial<Approval["a"]> = {}) =>
    draw({ id: "w1", tool: "write_scope", subject: "/srv/app/wsl.conf", reason: "这个文件在 agent 可写的文件夹之外。「本会话允许」和「始终允许」只放开 /srv/app 及其下面的内容，不是整个磁盘。", fresh: true, allowsSession: true, allowsPersist: true, input: { path: "/srv/app/wsl.conf", dir: "/srv/app" }, ...over });

  it("is titled for what it is, and names the file rather than the internal tool", () => {
    const box = card();
    expect(box.querySelector(".nm")?.textContent).toBe(t("chat.asking_to_write_outside_the_workspace"));
    expect(box.querySelector(".tool")?.textContent).toBe(t("chat.a_file_outside_the_workspace"));
    expect(box.querySelector(".tool")?.textContent).not.toContain("write_scope");
    expect(box.querySelector(".sub")?.textContent).toBe("/srv/app/wsl.conf");
  });

  it("says which folder a wider answer covers", () => {
    expect(card().querySelector(".apv")?.textContent).toContain("/srv/app 及其下面的内容");
  });

  it("offers this once, this folder for the session, and always, then refuse", () => {
    const box = card();
    expect(box.querySelector('[data-value="once"]')).not.toBeNull();
    expect(box.querySelector('[data-value="session"]')).not.toBeNull();
    expect(box.querySelector('[data-value="always"]')).not.toBeNull();
    expect(box.querySelector('[data-value="deny"]')).not.toBeNull();
  });

  it("offers neither wider answer for a folder the kernel will not grant", () => {
    const box = card({ allowsSession: false, allowsPersist: false });
    expect(box.querySelector('[data-value="session"]')).toBeNull();
    expect(box.querySelector('[data-value="always"]')).toBeNull();
    expect(box.querySelector('[data-value="once"]')).not.toBeNull();
  });

  it("does not offer to switch to full access, which cannot answer it", () => {
    expect(card().querySelector('[data-action="decision.full-access"]')).toBeNull();
  });
});

describe("a recursive delete outside the workspace", () => {
  const card = (over: Partial<Approval["a"]> = {}) =>
    draw({ id: "d1", tool: "delete_scope", subject: "/tmp/nas, /tmp/askpass.sh", reason: "这条命令会递归删除工作区和你已授权的文件夹之外的路径：/tmp/nas, /tmp/askpass.sh。只有你允许才会执行，这次答复只对这一条命令有效。", fresh: true, allowsSession: false, allowsPersist: false, input: { paths: ["/tmp/nas", "/tmp/askpass.sh"] }, ...over });

  it("is titled for what it is, and names the targets rather than the internal tool", () => {
    const box = card();
    expect(box.querySelector(".nm")?.textContent).toBe(t("chat.asking_to_delete_outside_the_workspace"));
    expect(box.querySelector(".tool")?.textContent).toBe(t("chat.paths_outside_the_workspace"));
    expect(box.querySelector(".tool")?.textContent).not.toContain("delete_scope");
    expect(box.querySelector(".sub")?.textContent).toBe("/tmp/nas, /tmp/askpass.sh");
  });

  it("covers one command: once or refuse, never a remembered answer or full access", () => {
    const box = card();
    expect(box.querySelector('[data-value="once"]')).not.toBeNull();
    expect(box.querySelector('[data-value="deny"]')).not.toBeNull();
    expect(box.querySelector('[data-value="session"]')).toBeNull();
    expect(box.querySelector('[data-value="always"]')).toBeNull();
    expect(box.querySelector('[data-action="decision.full-access"]')).toBeNull();
  });
});
