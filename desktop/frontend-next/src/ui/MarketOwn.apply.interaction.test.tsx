// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { OwnInstall } from "./MarketOwn";
import { MockPort } from "../port/mock";
import type { AgentPort, MarketPlan } from "../port/port";

afterEach(cleanup);

const applied = (plan: MarketPlan, name: string): MarketPlan => ({
  ...plan, applied: true, status: "done",
  actions: plan.actions?.map((a) => ({ ...a, name, status: "done" })),
});

describe("author installation across connection changes", () => {
  it.each(["success", "failure"])("ignores an old preview %s while the new connection is applying", async (outcome) => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const pkg = (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
    const oldPlan = await port.planOwnMarket({ slug: pkg.slug });
    const newPlan = { ...oldPlan, planId: "new-plan", version: "2.0.0", contentDigest: "new-digest" };
    let finishOld!: (plan: MarketPlan) => void;
    let failOld!: (error: Error) => void;
    let finishNew!: (plan: MarketPlan) => void;
    vi.spyOn(port, "planOwnMarket").mockImplementation(() => new Promise((resolve, reject) => { finishOld = resolve; failOld = reject; }));
    vi.spyOn(next, "planOwnMarket").mockResolvedValue(newPlan);
    const install = vi.spyOn(next, "installOwnMarket").mockImplementation(() => new Promise((resolve) => { finishNew = resolve; }));
    const onInstalled = vi.fn();
    const view = render(<OwnInstall port={port} pkg={pkg} onBack={() => {}} onInstalled={onInstalled} />);
    expect(screen.getByText("正在预览将安装的内容…")).toBeTruthy();
    view.rerender(<OwnInstall port={next} pkg={pkg} onBack={() => {}} onInstalled={onInstalled} />);
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    await act(async () => {
      if (outcome === "success") finishOld(oldPlan);
      else failOld(new Error("old preview failed"));
    });
    expect(screen.getByText("demo/ship-notes 2.0.0 将安装以下内容")).toBeTruthy();
    expect(screen.queryByText("old preview failed")).toBeNull();
    const applying = screen.getByRole<HTMLButtonElement>("button", { name: "安装中…" });
    expect(applying.disabled).toBe(true);
    await userEvent.click(applying);
    expect(install).toHaveBeenCalledTimes(1);
    expect(install).toHaveBeenCalledWith({ slug: pkg.slug, version: "2.0.0", planId: "new-plan", digest: "new-digest", replace: false });
    await act(async () => finishNew(applied(newPlan, "new-capability")));
    expect(document.querySelector(".mkt-installed")?.textContent).toContain("new-capability");
    expect(onInstalled).toHaveBeenCalledTimes(1);
  });

  it.each(["success", "failure"])("ignores an old install %s while the new connection is applying its own plan", async (outcome) => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const pkg = (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
    const oldPlan = await port.planOwnMarket({ slug: pkg.slug });
    const newPlan = { ...oldPlan, planId: "new-plan", version: "2.0.0", contentDigest: "new-digest" };
    vi.spyOn(next, "planOwnMarket").mockResolvedValue(newPlan);
    let finishOld!: (plan: MarketPlan) => void;
    let failOld!: (error: Error) => void;
    let finishNew!: (plan: MarketPlan) => void;
    const oldInstall = vi.spyOn(port, "installOwnMarket").mockImplementation(() => new Promise((resolve, reject) => { finishOld = resolve; failOld = reject; }));
    const newInstall = vi.spyOn(next, "installOwnMarket").mockImplementation(() => new Promise((resolve) => { finishNew = resolve; }));
    const onInstalled = vi.fn();
    const view = render(<OwnInstall port={port} pkg={pkg} onBack={() => {}} onInstalled={onInstalled} />);
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    expect(oldInstall).toHaveBeenCalledTimes(1);

    view.rerender(<OwnInstall port={next} pkg={pkg} onBack={() => {}} onInstalled={onInstalled} />);
    await screen.findByText("demo/ship-notes 2.0.0 将安装以下内容");
    await userEvent.click(screen.getByRole("button", { name: "安装" }));
    expect(newInstall).toHaveBeenCalledWith({ slug: pkg.slug, version: "2.0.0", planId: "new-plan", digest: "new-digest", replace: false });
    const button = screen.getByRole<HTMLButtonElement>("button", { name: "安装中…" });
    await act(async () => {
      if (outcome === "success") finishOld(applied(oldPlan, "old-capability"));
      else failOld(new Error("old installation failed"));
    });

    expect(screen.queryAllByText("old-capability")).toHaveLength(0);
    expect(screen.queryByText("old installation failed")).toBeNull();
    expect(screen.getByText("demo/ship-notes 2.0.0 将安装以下内容")).toBeTruthy();
    expect(button.disabled).toBe(true);
    await userEvent.click(button);
    expect(newInstall).toHaveBeenCalledTimes(1);
    expect(onInstalled).not.toHaveBeenCalled();
    await act(async () => finishNew(applied(newPlan, "new-capability")));
    expect(document.querySelector(".mkt-installed")?.textContent).toContain("new-capability");
    expect(onInstalled).toHaveBeenCalledTimes(1);
  });

  it("clears a completed install before previewing on a new connection", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const pkg = (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
    const plan = await port.planOwnMarket({ slug: pkg.slug });
    vi.spyOn(port, "installOwnMarket").mockResolvedValue(applied(plan, "old-capability"));
    let finish!: (plan: MarketPlan) => void;
    vi.spyOn(next, "planOwnMarket").mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    const view = render(<OwnInstall port={port} pkg={pkg} onBack={() => {}} onInstalled={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    await screen.findByText("装好了，下一轮就能用");
    expect(document.querySelector(".mkt-installed")?.textContent).toContain("old-capability");

    view.rerender(<OwnInstall port={next} pkg={pkg} onBack={() => {}} onInstalled={() => {}} />);
    expect(screen.queryAllByText("old-capability")).toHaveLength(0);
    expect(screen.getByText("正在预览将安装的内容…")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "查看已安装能力" })).toBeNull();
    await act(async () => finish({ ...plan, planId: "new-plan", version: "2.0.0" }));
    expect(screen.getByText("demo/ship-notes 2.0.0 将安装以下内容")).toBeTruthy();
  });

  it.each(["success", "failure"])("keeps the confirmation mounted until install %s finishes", async (outcome) => {
    const port = new MockPort() as unknown as AgentPort;
    const pkg = (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
    const plan = await port.planOwnMarket({ slug: pkg.slug });
    let finish!: (plan: MarketPlan) => void;
    let fail!: (error: Error) => void;
    vi.spyOn(port, "installOwnMarket").mockImplementation(() => new Promise((resolve, reject) => { finish = resolve; fail = reject; }));
    const onInstalled = vi.fn();
    const onBack = vi.fn();
    const view = render(<OwnInstall port={port} pkg={pkg} onBack={() => { onBack(); view.unmount(); }} onInstalled={onInstalled} />);
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    const back = screen.getByRole<HTMLButtonElement>("button", { name: "返回" });
    expect(back.disabled).toBe(true);
    back.focus();
    await userEvent.keyboard("{Enter}");
    await userEvent.click(back);
    expect(onBack).not.toHaveBeenCalled();
    await act(async () => {
      if (outcome === "success") finish(applied(plan, "old-capability"));
      else fail(new Error("old installation failed"));
    });
    expect(document.querySelector(".mkt")).toBeTruthy();
    expect(onInstalled).toHaveBeenCalledTimes(outcome === "success" ? 1 : 0);
    if (outcome === "failure") expect(screen.getByText("old installation failed")).toBeTruthy();
    const leave = screen.getByRole<HTMLButtonElement>("button", { name: outcome === "success" ? "返回我的发布" : "返回" });
    expect(leave.disabled).toBe(false);
    await userEvent.click(leave);
    expect(onBack).toHaveBeenCalledTimes(1);
    expect(document.querySelector(".mkt")).toBeNull();
  });
});
