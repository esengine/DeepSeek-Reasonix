// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Compaction } from "./Compaction";
import { MockPort } from "../port/mock";
import type { AgentPort, CompactionSettings } from "../port/port";

afterEach(cleanup);

const port = (over: Partial<CompactionSettings> = {}) => {
  const p = new MockPort() as unknown as AgentPort;
  const base = {
    soft_limit_tokens: 0, default_soft_limit: 0, ratio: 0.85,
    context_window: 1_000_000, trigger: 850000, path: "~/.reasonix/config.toml",
    ...over,
  };
  p.compaction = async () => ({ ...base });
  return p;
};

const open = async (p: AgentPort) => {
  render(<Compaction port={p} onChanged={() => {}} />);
  await screen.findByText("下次整理");
};

describe("capacity-based context maintenance", () => {
  it("shows the capacity boundary as the default", async () => {
    await open(port());
    expect(screen.getAllByText("850k").length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: "按模型容量（默认）" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByText(/容量保护会先到/)).toBeTruthy();
  });

  it("keeps the policy visible without an advanced disclosure", async () => {
    await open(port());
    expect(screen.queryByRole("button", { name: "高级设置" })).toBeNull();
    expect(screen.getByRole("button", { name: "自定义" })).toBeTruthy();
    expect(screen.getByText(/中转站未提供容量时使用 160k/)).toBeTruthy();
  });

  it("stores the capacity default as zero", async () => {
    const p = port({ soft_limit_tokens: 90000, trigger: 90000 });
    const save = vi.fn(async (n: number) => ({ ...(await p.compaction()), soft_limit_tokens: n, trigger: 850000 }));
    p.saveCompaction = save;
    await open(p);
    await userEvent.click(screen.getByRole("button", { name: "按模型容量（默认）" }));
    expect(save).toHaveBeenCalledWith(0);
  });

  it("treats a negative legacy value as capacity-based", async () => {
    await open(port({ soft_limit_tokens: -1 }));
    expect(screen.getByRole("button", { name: "按模型容量（默认）" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.queryByText("-1")).toBeNull();
  });

  it("opens and saves a custom fixed threshold", async () => {
    const p = port();
    const save = vi.fn(async (n: number) => ({ ...(await p.compaction()), soft_limit_tokens: n, trigger: n }));
    p.saveCompaction = save;
    await open(p);
    await userEvent.click(screen.getByRole("button", { name: "自定义" }));
    const box = screen.getByRole("textbox");
    await userEvent.clear(box);
    await userEvent.type(box, "90000{Enter}");
    await waitFor(() => expect(save).toHaveBeenCalledWith(90000));
    expect(save).toHaveBeenCalledTimes(1);
  });

  it("shows an existing custom threshold immediately", async () => {
    await open(port({ soft_limit_tokens: 90000, trigger: 90000 }));
    expect(screen.getByRole("button", { name: "自定义" }).getAttribute("aria-pressed")).toBe("true");
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("90000");
    expect(screen.getByText(/经济维护阈值会先到/)).toBeTruthy();
  });
});
