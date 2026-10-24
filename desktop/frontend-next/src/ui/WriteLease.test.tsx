// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { WriteLease } from "./WriteLease";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

const toggle = () => screen.findByRole("switch", { name: "乐观模式（多会话并行）" });

describe("the write-lease switch", () => {
  it("ignores an old save response after switching to another chat", async () => {
    const old = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const original = await old.writeLease();
    let finish!: (value: typeof original) => void;
    old.saveWriteLease = () => new Promise((resolve) => { finish = resolve; });
    await next.saveWriteLease({ collaboration: "shared", autoIntegrate: false });
    const view = render(<WriteLease port={old} />);
    await userEvent.click(await screen.findByRole("switch", { name: "自动整合已验证成果" }));
    view.rerender(<WriteLease port={next} />);
    await waitFor(() => expect(screen.getByRole("combobox", { name: "协作工作方式" })).toHaveProperty("value", "shared"));
    finish({ ...original, collaboration: "isolated", autoIntegrate: true });
    await waitFor(() => expect(screen.getByRole("switch", { name: "自动整合已验证成果" }).getAttribute("aria-checked")).toBe("false"));
    expect(screen.getByRole("combobox", { name: "协作工作方式" })).toHaveProperty("value", "shared");
  });
  it("saves automatic integration without changing the current runtime", async () => {
    const p = new MockPort() as unknown as AgentPort;
    const save = vi.spyOn(p, "saveWriteLease");
    render(<WriteLease port={p} />);
    const sw = await screen.findByRole("switch", { name: "自动整合已验证成果" });
    expect(sw.getAttribute("aria-checked")).toBe("true");
    await userEvent.click(sw);
    expect(save).toHaveBeenLastCalledWith({ autoIntegrate: false });
    await waitFor(() => expect(sw.getAttribute("aria-checked")).toBe("false"));
    expect(screen.getByText("当前聊天自动整合：开。")).toBeTruthy();
    expect(await p.writeLease()).toMatchObject({ autoIntegrate: false, effective: { autoIntegrate: true } });
  });
  it("starts on, says what it costs and when it takes effect, and saves the opposite of what it shows", async () => {
    const p = new MockPort() as unknown as AgentPort;
    const save = vi.spyOn(p, "saveWriteLease");
    render(<WriteLease port={p} />);
    const sw = await toggle();
    expect(sw.getAttribute("aria-checked")).toBe("true");
    expect(screen.getByText(/没有事前的路径围栏，只在事后观察/)).toBeTruthy();
    expect(screen.getByText(/只对之后新开的会话生效/)).toBeTruthy();
    await userEvent.click(sw);
    expect(save).toHaveBeenCalledWith({ optimistic: false });
    await waitFor(() => expect(sw.getAttribute("aria-checked")).toBe("false"));
    await userEvent.click(sw);
    expect(save).toHaveBeenLastCalledWith({ optimistic: true });
  });

  it("shows the longest wait, saves a changed one on blur and shows what the kernel stored", async () => {
    const p = new MockPort() as unknown as AgentPort;
    const save = vi.spyOn(p, "saveWriteLease");
    render(<WriteLease port={p} />);
    const box = (await screen.findByRole("textbox", { name: "等待其他会话释放写入权的最长时间" })) as HTMLInputElement;
    expect(box.value).toBe("0");
    await userEvent.clear(box);
    await userEvent.type(box, "30");
    await userEvent.tab();
    expect(save).toHaveBeenCalledWith({ maxWaitMinutes: 30 });
    await waitFor(() => expect(box.value).toBe("30"));
    expect(screen.getByText(/workspace\.claim_timeout/)).toBeTruthy();
  });

  it("keeps the switch where it was when the save is refused", async () => {
    const p = new MockPort() as unknown as AgentPort;
    p.saveWriteLease = async () => {
      throw new Error("disk full");
    };
    render(<WriteLease port={p} />);
    const sw = await toggle();
    await userEvent.click(sw);
    await screen.findByText(/disk full/);
    expect(sw.getAttribute("aria-checked")).toBe("true");
  });

  it("says so when the setting cannot be read", async () => {
    const p = new MockPort() as unknown as AgentPort;
    p.writeLease = async () => {
      throw new Error("offline");
    };
    render(<WriteLease port={p} />);
    expect(await screen.findByText("无法读取多会话写入设置。")).toBeTruthy();
  });

  it("saves the strategy while continuing to show the current runtime and retains legacy choices", async () => {
    const p = new MockPort() as unknown as AgentPort;
    await p.saveWriteLease({ optimistic: false, maxWaitMinutes: 23 });
    const save = vi.spyOn(p, "saveWriteLease");
    render(<WriteLease port={p} />);
    const choice = await screen.findByRole("combobox", { name: "协作工作方式" });
    expect(screen.getByRole("status").textContent).toContain("当前聊天：兼容模式");
    await userEvent.selectOptions(choice, "auto");
    expect(save).toHaveBeenLastCalledWith({ collaboration: "auto" });
    expect(await screen.findByText("已保存：自动隔离。当前聊天尚未切换。")).toBeTruthy();
    expect(screen.getByRole("status").textContent).toContain("子代理并发 8，写入并发 4");
    expect(await p.writeLease()).toMatchObject({ optimistic: false, maxWaitMinutes: 23, effective: { collaboration: "legacy", optimistic: true } });
    await userEvent.selectOptions(choice, "legacy");
    expect((await toggle()).getAttribute("aria-checked")).toBe("false");
    expect((await screen.findByRole("textbox", { name: "等待其他会话释放写入权的最长时间" }) as HTMLInputElement).value).toBe("23");
  });

  it("keeps the saved strategy when persistence fails", async () => {
    const p = new MockPort() as unknown as AgentPort;
    p.saveWriteLease = async () => { throw new Error("disk full"); };
    render(<WriteLease port={p} />);
    const choice = await screen.findByRole("combobox", { name: "协作工作方式" });
    await userEvent.selectOptions(choice, "shared");
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "disk full");
    expect(choice).toHaveProperty("value", "legacy");
  });

  it("does not invent an effective policy or allow unsupported saves with an older server", async () => {
    const p = new MockPort() as unknown as AgentPort;
    p.writeLease = async () => ({ optimistic: false, maxWaitMinutes: 0, path: "/fixture/config.toml" });
    render(<WriteLease port={p} />);
    expect(await screen.findByRole("combobox", { name: "协作工作方式" })).toHaveProperty("disabled", true);
    expect(screen.getByRole("status").textContent).toBe("无法确认当前聊天的生效策略。");
  });

  it("shows the effective transaction timeout independently of the saved legacy wait", async () => {
    const p = new MockPort() as unknown as AgentPort;
    const initial = await p.writeLease();
    p.writeLease = async () => ({ ...initial, collaboration: "shared", maxWaitMinutes: 120,
      effective: { ...initial.effective!, collaboration: "shared", transactionWaitSeconds: 30 } });
    render(<WriteLease port={p} />);
    expect(await screen.findByText(/当前共享事务最多等待 30 秒/)).toBeTruthy();
    expect(screen.getByRole("status").textContent).toContain("当前聊天：共享工作区");
    expect(screen.getByRole("textbox", { name: "等待其他会话释放写入权的最长时间" }).closest("details")).toHaveProperty("open", false);
  });
  it("says when automatic isolation falls back to shared transactions on this host", async () => {
    const p = new MockPort() as unknown as AgentPort;
    const initial = await p.writeLease();
    p.writeLease = async () => ({ ...initial, collaboration: "auto",
      effective: { ...initial.effective!, collaboration: "auto", transactionWaitSeconds: 30, automaticIsolationFallback: true } });
    render(<WriteLease port={p} />);
    expect(await screen.findByText("本机无法为自动隔离限定完整的写入边界，当前聊天的可写子任务改用共享事务。")).toBeTruthy();
  });
});
