// @vitest-environment jsdom
import "./testkit";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Item } from "../state/session";
import { UserCard } from "./cards/UserCard";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const row = (over: Partial<Extract<Item, { t: "user" }>> = {}) =>
  ({ t: "user", id: "row", text: "测试也要覆盖带 BOM 的文件", pending: true, queued: "steer", itemId: "i1", ...over }) as Extract<Item, { t: "user" }>;

describe("guidance said into a running turn, not read yet", () => {
  it("stands at the end of the conversation as a dashed bubble that says it is waiting", () => {
    render(<UserCard item={row()} onWithdraw={() => {}} />);
    expect(screen.getByText("插话 · 等待插入")).toBeTruthy();
    expect(screen.getByText("测试也要覆盖带 BOM 的文件").className).toContain("sw-txt");
    expect(document.querySelector(".steerwait")).toBeTruthy();
  });

  it("withdraws the entry the kernel named", async () => {
    const onWithdraw = vi.fn();
    render(<UserCard item={row()} onWithdraw={onWithdraw} />);
    await userEvent.click(screen.getByRole("button", { name: "撤回" }));
    expect(onWithdraw).toHaveBeenCalledWith("i1");
  });

  it("turns into an ordinary guidance row once the turn has read it", () => {
    render(<UserCard item={row({ pending: false, steer: true })} onWithdraw={() => {}} />);
    expect(document.querySelector(".steerwait")).toBeNull();
    expect(screen.getByText("插话")).toBeTruthy();
  });

  it("keeps a queued follow-up on the ordinary pending row", () => {
    render(<UserCard item={row({ queued: "followup" })} onWithdraw={() => {}} />);
    expect(document.querySelector(".steerwait")).toBeNull();
  });

  it("draws the ordinary row when nothing can withdraw it", () => {
    render(<UserCard item={row()} />);
    expect(document.querySelector(".steerwait")).toBeNull();
  });
});
