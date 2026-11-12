// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

// These tests describe the feedback channel while it exists; the shipped build has it retired.
vi.mock("../feedbackchannel", () => ({ FEEDBACK_RETIRED: false }));
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import { dropDraft, heldDraft } from "./feedbackdraft";
import { clearToasts, pushToast } from "../state/toasts";
import { ToastStack } from "./ToastStack";

afterEach(() => {
  cleanup();
  clearToasts();
  dropDraft();
  vi.useRealTimers();
  sessionStorage.clear();
});

describe("the corner notices", () => {
  it("stay until dismissed, however long they wait", () => {
    vi.useFakeTimers();
    render(<ToastStack onFeedback={vi.fn()} />);
    act(() => pushToast("模型切换失败"));
    act(() => { vi.advanceTimersByTime(10 * 60_000); });
    expect(screen.getByText("模型切换失败")).toBeTruthy();
  });

  it("show at most three, each dismissed by itself", async () => {
    render(<ToastStack onFeedback={vi.fn()} />);
    act(() => { for (const text of ["一", "二", "三", "四"]) pushToast(text); });
    expect(screen.queryByText("一")).toBeNull();
    expect(screen.getAllByRole("alert")).toHaveLength(3);
    await userEvent.click(screen.getAllByRole("button", { name: "知道了" })[1]);
    expect(screen.queryByText("三")).toBeNull();
    expect(screen.getByText("二")).toBeTruthy();
    expect(screen.getByText("四")).toBeTruthy();
  });

  it("report the problem from where it appeared, and are gone once reported", async () => {
    const onFeedback = vi.fn();
    render(<ToastStack onFeedback={onFeedback} />);
    act(() => pushToast("分支切换失败"));
    await userEvent.click(screen.getByRole("button", { name: "报告此问题" }));
    expect(onFeedback).toHaveBeenCalledOnce();
    expect(heldDraft().body).toBe("分支切换失败");
    expect(screen.queryByText("分支切换失败")).toBeNull();
  });

  it("open the feedback form with what failed already written in it", async () => {
    const hub = new MockHub();
    const build = hub.portFor.bind(hub);
    hub.portFor = (rt) => {
      const port = build(rt);
      port.providerSetup = async () => null;
      port.welcomeSeen = async () => true;
      return port;
    };
    render(<App hub={hub} />);
    await waitFor(() => expect(document.querySelector(".crumb b")?.textContent).toBeTruthy());
    act(() => pushToast("无法打开链接"));
    await userEvent.click(await screen.findByRole("button", { name: "报告此问题" }));
    const box = await screen.findByRole<HTMLTextAreaElement>("textbox", { name: /发生了什么/ });
    expect(box.value).toContain("无法打开链接");
  });
});
