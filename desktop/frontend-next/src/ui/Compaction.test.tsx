// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Compaction } from "./Compaction";
import { MockPort } from "../port/mock";
import type { AgentPort, CompactionSettings } from "../port/port";
import { HttpError } from "../port/http_error";

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

  it("says on the threshold field itself when the capacity guard caps it", async () => {
    await open(port({ soft_limit_tokens: 850000, ratio: 0.5, trigger: 500000 }));
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("850000");
    const row = screen.getByRole("textbox").closest(".threshold-row")!;
    expect(row.textContent).toContain("500k");
    expect(row.textContent).toContain("50%");
    expect(row.textContent).not.toContain("达到这个用量时开始整理");
  });

  it("keeps the plain hint when the threshold is the one that applies", async () => {
    await open(port({ soft_limit_tokens: 90000, trigger: 90000 }));
    const row = screen.getByRole("textbox").closest(".threshold-row")!;
    expect(row.textContent).toContain("达到这个用量时开始整理");
    expect(row.textContent).not.toContain("容量保护");
  });

  it.each(["runtime.saved_while_running", "runtime.rebuild_failed"])("announces %s as saved but not applied", async (code) => {
    const p = port();
    p.saveCompaction = vi.fn().mockRejectedValue(new HttpError(409, "rebuild refused", { code, params: { detail: "boom" } }));
    const changed = vi.fn();
    render(<Compaction port={p} onChanged={changed} />);
    await screen.findByText("下次整理");
    await userEvent.click(screen.getByRole("button", { name: "自定义" }));
    await userEvent.clear(screen.getByRole("textbox"));
    await userEvent.type(screen.getByRole("textbox"), "90000{Enter}");
    const notice = await screen.findByRole("status");
    expect(notice.getAttribute("data-lvl")).toBe("warn");
    expect(notice.textContent).toContain("已保存，尚未生效");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getAllByText("850k").length).toBeGreaterThan(0);
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("90000");
    expect(changed).not.toHaveBeenCalled();
    expect(p.saveCompaction).toHaveBeenCalledTimes(1);
  });

  it("announces a rejected write as an error and keeps the threshold for retry", async () => {
    const p = port();
    p.saveCompaction = vi.fn().mockRejectedValue(new HttpError(400, "write refused", { code: "compaction.rejected", params: { detail: "disk full" } }));
    await open(p);
    await userEvent.click(screen.getByRole("button", { name: "自定义" }));
    await userEvent.clear(screen.getByRole("textbox"));
    await userEvent.type(screen.getByRole("textbox"), "90000{Enter}");
    const notice = await screen.findByRole("alert");
    expect(notice.getAttribute("data-lvl")).toBe("err");
    expect(notice.textContent).toContain("操作未完成");
    expect(notice.textContent).toContain("disk full");
    expect(screen.queryByRole("status")).toBeNull();
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("90000");
  });

  it("retries the same threshold after a failed write without duplicating Enter and blur", async () => {
    const p = port();
    const save = vi.fn()
      .mockRejectedValueOnce(new Error("disk full"))
      .mockImplementation(async (n: number) => ({ ...(await p.compaction()), soft_limit_tokens: n, trigger: n }));
    p.saveCompaction = save;
    const changed = vi.fn();
    render(<Compaction port={p} onChanged={changed} />);
    await screen.findByText("下次整理");
    await userEvent.click(screen.getByRole("button", { name: "自定义" }));
    const input = screen.getByRole("textbox") as HTMLInputElement;
    await userEvent.clear(input);
    await userEvent.type(input, "90000{Enter}");
    await screen.findByText("disk full");
    expect(input.disabled).toBe(false);
    expect(save).toHaveBeenCalledTimes(1);
    expect(changed).not.toHaveBeenCalled();
    await userEvent.click(input);
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(changed).toHaveBeenCalledTimes(1));
    expect(save).toHaveBeenCalledTimes(2);
    expect(save).toHaveBeenLastCalledWith(90000);
    expect(screen.queryByText("disk full")).toBeNull();
    expect(screen.getAllByText("90k").length).toBeGreaterThan(0);
  });

  it("rejects an invalid threshold before sending a write and clears the notice after correction", async () => {
    const p = port();
    const save = vi.fn(async (n: number) => ({ ...(await p.compaction()), soft_limit_tokens: n, trigger: n }));
    p.saveCompaction = save;
    await open(p);
    await userEvent.click(screen.getByRole("button", { name: "自定义" }));
    await userEvent.clear(screen.getByRole("textbox"));
    await userEvent.type(screen.getByRole("textbox"), "999{Enter}");
    expect((await screen.findByRole("alert")).textContent).toContain("阈值需至少为 1,000");
    expect(save).not.toHaveBeenCalled();
    await userEvent.clear(screen.getByRole("textbox"));
    await userEvent.type(screen.getByRole("textbox"), "90000{Enter}");
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("does not re-save a successfully applied threshold on a later blur", async () => {
    const p = port();
    const save = vi.fn(async (n: number) => ({ ...(await p.compaction()), soft_limit_tokens: n, trigger: n }));
    p.saveCompaction = save;
    await open(p);
    await userEvent.click(screen.getByRole("button", { name: "自定义" }));
    await userEvent.clear(screen.getByRole("textbox"));
    await userEvent.type(screen.getByRole("textbox"), "90000{Enter}");
    await screen.findByText(/经济维护阈值会先到/);
    await userEvent.click(screen.getByRole("textbox"));
    await userEvent.tab();
    expect(save).toHaveBeenCalledTimes(1);
  });
});
