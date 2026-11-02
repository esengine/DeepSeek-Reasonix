// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { Compaction } from "./Compaction";
import { MockPort } from "../port/mock";
import type { AgentPort, CompactionSettings } from "../port/port";

afterEach(cleanup);

const port = (over: Partial<CompactionSettings> = {}) => {
  const p = new MockPort() as unknown as AgentPort;
  const base = {
    ratio: 0.85, context_window: 1_000_000, trigger: 850000, path: "~/.reasonix/config.toml",
    ...over,
  };
  p.compaction = async () => ({ ...base });
  return p;
};

describe("context maintenance follows the window share and nothing else", () => {
  it("shows where the session folds and the share it comes from", async () => {
    render(<Compaction port={port()} />);
    await screen.findByText("下次整理");
    expect(screen.getAllByText("850k").length).toBeGreaterThan(0);
    expect(screen.getByText(/本会话预计在 850k tokens 左右自动整理上下文/)).toBeTruthy();
    expect(screen.getByText(/模型窗口的 85% 自动整理/)).toBeTruthy();
  });

  it("offers no threshold of its own to set", async () => {
    render(<Compaction port={port()} />);
    await screen.findByText("下次整理");
    expect(screen.queryByRole("button", { name: "自定义" })).toBeNull();
    expect(screen.queryByText("整理策略")).toBeNull();
  });

  it("says nothing folds when no window was declared", async () => {
    render(<Compaction port={port({ context_window: 0, trigger: 0 })} />);
    await screen.findByText(/未声明上下文窗口/);
  });

  it("reports an unreadable configuration", async () => {
    const p = port();
    p.compaction = async () => {
      throw new Error("down");
    };
    render(<Compaction port={p} />);
    await screen.findByText("无法读取压缩配置。");
  });
});
