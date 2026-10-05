// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { RuntimeBar } from "./RuntimeBar";
import type { Stall } from "../state/session_types";

afterEach(cleanup);

const stall = (over: Partial<Stall> = {}): Stall => ({
  cause: "rounds", idleRounds: 20, roundLimit: 20, promptTokens: 0, tokenMultiple: 8, paused: false, ...over,
});

describe("the stall strip beside the composer", () => {
  it("says the count and offers stop or quiet while the run goes on", async () => {
    const onStall = vi.fn();
    render(<RuntimeBar notices={[]} onSeen={() => {}} watch={{ stall: stall() }} onStall={onStall} />);
    expect(screen.getByRole("status").textContent).toContain("已连续 20 轮没有可观察的进展");
    await userEvent.click(screen.getByRole("button", { name: "停止" }));
    await userEvent.click(screen.getByRole("button", { name: "不再提示（本会话）" }));
    expect(onStall.mock.calls.map((c) => c[0])).toEqual(["stop", "mute"]);
  });

  it("offers to continue once the user's setting paused the run", async () => {
    const onStall = vi.fn();
    render(<RuntimeBar notices={[]} onSeen={() => {}} watch={{ stall: stall({ paused: true }), stallMuted: true }} onStall={onStall} />);
    expect(screen.getByRole("status").textContent).toContain("任务已暂停");
    await userEvent.click(screen.getByRole("button", { name: "继续（计数清零）" }));
    expect(onStall).toHaveBeenCalledWith("continue");
  });

  it("says the backstop in tokens", () => {
    render(<RuntimeBar notices={[]} onSeen={() => {}} watch={{ stall: stall({ cause: "tokens", promptTokens: 1200000 }) }} onStall={() => {}} />);
    expect(screen.getByRole("status").textContent).toContain("8 倍上下文");
  });

  it("says a perseveration stall as perseveration", () => {
    render(<RuntimeBar notices={[]} onSeen={() => {}} watch={{ stall: stall({ cause: "perseveration" }) }} onStall={() => {}} />);
    expect(screen.getByRole("status").textContent).toContain("模型在重复输出同一段文字");
  });

  it("stays quiet once muted", () => {
    render(<RuntimeBar notices={[]} onSeen={() => {}} watch={{ stall: stall(), stallMuted: true }} onStall={() => {}} />);
    expect(screen.queryByRole("status")).toBeNull();
  });
});
