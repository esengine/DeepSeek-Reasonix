// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PostureNote, postureNote } from "./PostureNote";
import { HttpError } from "../port/port";
import type { AgentPort, ApprovalDefault, SessionStatus } from "../port/port";

afterEach(cleanup);
beforeEach(() => localStorage.clear());

const DEF: ApprovalDefault = { defaulted: true, writesConfined: true, trust: "", trustable: true };
const status = (d: Partial<ApprovalDefault> | undefined, mode: SessionStatus["toolApprovalMode"] = "ask") =>
  ({ toolApprovalMode: mode, approvalDefault: d && { ...DEF, ...d } }) as SessionStatus;

function draw(s: SessionStatus, calls: Partial<AgentPort> = {}) {
  const onChanged = vi.fn();
  const port = {
    setApprovalMode: vi.fn(async () => {}),
    decideWorkspaceTrust: vi.fn(async () => {}),
    ...calls,
  } as unknown as AgentPort;
  const view = render(<PostureNote port={port} status={s} onChanged={onChanged} />);
  return { ...view, port, onChanged };
}

describe("which note a session owes", () => {
  it.each([
    ["an older kernel says nothing", undefined, "ask", null],
    ["a named posture is left alone", { defaulted: false, writesConfined: false }, "ask", null],
    ["a defaulted auto owes nothing", { trust: "trusted" }, "auto", null],
    ["an undecided folder under a sandbox is asked", {}, "ask", "trust"],
    ["a declined folder is not asked again", { trust: "declined" }, "ask", null],
    ["a home directory is not asked", { trustable: false }, "ask", null],
    ["no sandbox explains the ask", { writesConfined: false }, "ask", "unconfined"],
  ] as const)("%s", (_, d, mode, want) => {
    expect(postureNote(status(d as Partial<ApprovalDefault> | undefined, mode))).toBe(want);
  });
});

describe("the folder-trust question", () => {
  it("records the answer and refreshes the posture", async () => {
    const { port, onChanged } = draw(status({}));
    expect(screen.getByRole("group", { name: "是否信任此文件夹" })).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "信任此文件夹" }));
    expect(port.decideWorkspaceTrust).toHaveBeenCalledWith("trusted");
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
  });

  it("records a decline", async () => {
    const { port } = draw(status({}));
    await userEvent.click(screen.getByRole("button", { name: "暂不信任" }));
    expect(port.decideWorkspaceTrust).toHaveBeenCalledWith("declined");
  });

  it("does not send a second answer while the first is pending", async () => {
    let settle!: () => void;
    const decideWorkspaceTrust = vi.fn(() => new Promise<void>((r) => { settle = r; }));
    draw(status({}), { decideWorkspaceTrust });
    await userEvent.click(screen.getByRole("button", { name: "信任此文件夹" }));
    await userEvent.click(screen.getByRole("button", { name: "暂不信任" }));
    expect(decideWorkspaceTrust).toHaveBeenCalledTimes(1);
    settle();
  });

  it("says the kernel's refusal in place of the explanation", async () => {
    const decideWorkspaceTrust = vi.fn(async () => {
      throw new HttpError(409, "home", { code: "workspace.untrustable" });
    });
    const { onChanged } = draw(status({}), { decideWorkspaceTrust });
    await userEvent.click(screen.getByRole("button", { name: "信任此文件夹" }));
    await waitFor(() => expect(screen.getByText(/主目录或磁盘根目录不能整体信任/)).toBeTruthy());
    expect(onChanged).not.toHaveBeenCalled();
  });
});

describe("the no-sandbox note", () => {
  it("switches to auto in one click", async () => {
    const { port, onChanged } = draw(status({ writesConfined: false }));
    await userEvent.click(screen.getByRole("button", { name: "切换到自动批准" }));
    expect(port.setApprovalMode).toHaveBeenCalledWith("auto");
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
  });

  it("stays dismissed once read", async () => {
    const { unmount } = draw(status({ writesConfined: false }));
    await userEvent.click(screen.getByRole("button", { name: "知道了" }));
    expect(screen.queryByRole("button", { name: "切换到自动批准" })).toBeNull();
    unmount();
    draw(status({ writesConfined: false }));
    expect(screen.queryByRole("button", { name: "切换到自动批准" })).toBeNull();
  });

  it("still draws when storage is unavailable", () => {
    const get = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked"); });
    draw(status({ writesConfined: false }));
    expect(screen.getByRole("button", { name: "切换到自动批准" })).toBeTruthy();
    get.mockRestore();
  });
});
