// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MyPackages } from "./MarketPublish";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, MarketPackage, MarketPlan } from "../port/port";

afterEach(() => { cleanup(); localStorage.setItem(STORAGE, "zh"); boot(); });

describe("my packages connection lifetime", () => {
  it.each(["zh", "en"])("announces review failures on their own row and clears them for retry and host changes (%s)", async (lang) => {
    localStorage.setItem(STORAGE, lang); boot();
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const first = { ...(await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!, status: "private" };
    const second = { ...first, slug: "demo/other-notes", name: "other-notes" };
    vi.spyOn(port, "myMarket").mockResolvedValue([first, second]);
    vi.spyOn(next, "myMarket").mockResolvedValue([first, second]);
    let fail!: (error: Error) => void;
    const submit = vi.spyOn(port, "submitMarket").mockRejectedValueOnce(new Error("review unavailable"))
      .mockImplementationOnce(() => new Promise((_, reject) => { fail = reject; }));
    const view = render(<MyPackages port={port} onInstalled={() => {}} />);
    const row = (await screen.findByText(first.name)).closest("li")!;
    const other = screen.getByText(second.name).closest("li")!;
    await userEvent.click(within(row).getByRole("button", { name: t("提交审核") }));
    expect((await within(row).findByRole("alert")).textContent).toBe("review unavailable");
    expect(within(other).queryByRole("alert")).toBeNull();
    await userEvent.click(within(row).getByRole("button", { name: t("提交审核") }));
    expect(screen.queryByRole("alert")).toBeNull();
    expect(within(row).getByRole<HTMLButtonElement>("button", { name: t("提交中…") }).disabled).toBe(true);
    await act(async () => fail(new Error("retry unavailable")));
    expect(within(row).getByRole("alert").textContent).toBe("retry unavailable");
    view.rerender(<MyPackages port={next} onInstalled={() => {}} />);
    await screen.findByText(first.name);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(submit).toHaveBeenNthCalledWith(1, first.slug);
    expect(submit).toHaveBeenNthCalledWith(2, first.slug);
  });

  it("refreshes the parent's inventory after a blocked Back during installation", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const pkg = (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
    const plan = await port.planOwnMarket({ slug: pkg.slug });
    const read = vi.spyOn(port, "myMarket").mockResolvedValueOnce([pkg])
      .mockResolvedValueOnce([{ ...pkg, installed: { version: pkg.latestVersion, contentHash: "installed-digest" } }]);
    let finish!: (plan: MarketPlan) => void;
    vi.spyOn(port, "installOwnMarket").mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    const onInstalled = vi.fn();
    render(<MyPackages port={port} onInstalled={onInstalled} />);
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    await userEvent.click(screen.getByRole("button", { name: "返回" }));
    await act(async () => finish({ ...plan, applied: true, status: "done", actions: plan.actions?.map((a) => ({ ...a, status: "done" })) }));
    expect(onInstalled).toHaveBeenCalledTimes(1);
    expect(read).toHaveBeenCalledTimes(2);
    await userEvent.click(screen.getByRole("button", { name: "返回我的发布" }));
    expect(await screen.findByText("已安装")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
  });

  it("closes the old account's package before the new connection can preview it", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const pkg = (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
    vi.spyOn(port, "myMarket").mockResolvedValue([pkg]);
    vi.spyOn(next, "myMarket").mockResolvedValue([{ ...pkg, slug: "other/new-package", name: "new-package" }]);
    const preview = vi.spyOn(next, "planOwnMarket");
    const install = vi.spyOn(next, "installOwnMarket");
    const view = render(<MyPackages port={port} onInstalled={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    await screen.findByText(/demo\/ship-notes .* 将安装以下内容/);
    view.rerender(<MyPackages port={next} onInstalled={() => {}} />);
    await screen.findByText("new-package");
    expect(screen.queryByText(/demo\/ship-notes .* 将安装以下内容/)).toBeNull();
    expect(preview).not.toHaveBeenCalled();
    expect(install).not.toHaveBeenCalled();
  });

  it.each(["success", "failure"])("isolates an old submit %s from a new submission with the same slug", async (outcome) => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const pkg = { ...(await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!, status: "private" };
    vi.spyOn(port, "myMarket").mockResolvedValue([pkg]);
    const nextRows = vi.spyOn(next, "myMarket").mockResolvedValue([pkg]);
    let finishOld!: (pkg: MarketPackage) => void;
    let failOld!: (error: Error) => void;
    let finishNew!: (pkg: MarketPackage) => void;
    vi.spyOn(port, "submitMarket").mockImplementation(() => new Promise((resolve, reject) => { finishOld = resolve; failOld = reject; }));
    const submit = vi.spyOn(next, "submitMarket").mockImplementation(() => new Promise((resolve) => { finishNew = resolve; }));
    const view = render(<MyPackages port={port} onInstalled={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: "提交审核" }));
    view.rerender(<MyPackages port={next} onInstalled={() => {}} />);
    const send = await screen.findByRole<HTMLButtonElement>("button", { name: "提交审核" });
    expect(send.disabled).toBe(false);
    await userEvent.click(send);
    await act(async () => {
      if (outcome === "success") finishOld({ ...pkg, name: "old-response", status: "pending" });
      else failOld(new Error("old submission failed"));
    });
    expect(screen.queryByText("old-response")).toBeNull();
    expect(screen.queryByText("old submission failed")).toBeNull();
    expect(screen.getByText("ship-notes")).toBeTruthy();
    const sending = screen.getByRole<HTMLButtonElement>("button", { name: "提交中…" });
    expect(sending.disabled).toBe(true);
    await userEvent.click(sending);
    expect(submit).toHaveBeenCalledTimes(1);
    await act(async () => {
      nextRows.mockResolvedValue([{ ...pkg, status: "pending" }]);
      finishNew({ ...pkg, status: "pending" });
    });
    expect(screen.getByText("审核中")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "提交中…" })).toBeNull();
  });
});
