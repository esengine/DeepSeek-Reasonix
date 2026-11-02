// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { ApprovalCard } from "../cards/ApprovalCard";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(cleanup);

// A folder a model chose for an agent is a reach only the person grants; the
// card says which agent and which folder, and offers no standing answer.
it("a team folder card names the agent and the folder, and only answers once", async () => {
  const approve = vi.fn(async () => {});
  render(
    <ApprovalCard
      item={{ t: "approval", id: "i1", a: { id: "a1", tool: "team_change", subject: "Ben", input: { agent: "Ben", field: "folder", to: "/work/launch" } } }}
      onApprove={approve} onFullAccess={vi.fn(async () => {})} onPlan={vi.fn(async () => {})}
    />,
  );
  expect(screen.getByText("请求给 Agent 指定文件夹")).toBeTruthy();
  expect(screen.getByText("允许 Ben 在 /work/launch 中读写文件？这会扩大它能接触的范围，所以每次都要你亲自确认。")).toBeTruthy();
  expect(screen.queryByRole("button", { name: /本会话/ })).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "拒绝" }));
  expect(approve).toHaveBeenCalledWith("i1", "a1", "deny");
});
