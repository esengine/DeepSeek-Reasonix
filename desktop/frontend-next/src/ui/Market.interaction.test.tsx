// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { StrictMode } from "react";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Market } from "./Market";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/http_error";
import type { AgentPort, MarketDetail, MarketList, MarketPackage, MarketQuery } from "../port/port";

afterEach(cleanup);

const row = (slug: string, pinned?: boolean): MarketPackage => ({
  kind: "skill", handle: slug.split("/")[0], name: slug.split("/")[1], slug, summary: "s", description: "", homepage: "",
  repoUrl: "", tags: [], latestVersion: "1.0.0", installCount: 3, starCount: 0, upCount: 0, downCount: 0, approvalRate: null, score: 0, verified: false, status: "active", updatedAt: "", pinned,
});

describe("market list", () => {
  it.each([
    ["more", "success"], ["more", "failure"], ["retry", "success"], ["retry", "failure"],
  ])("keeps focus in the market while a next-page %s ends in %s", async (trigger, outcome) => {
    const port = new MockPort() as unknown as AgentPort;
    let finish!: (value: MarketList) => void;
    let fail!: (error: Error) => void;
    const read = vi.fn().mockResolvedValueOnce({ packages: [row("a/one", true), row("b/two", true)], limit: 2, offset: 0 });
    if (trigger === "retry") read.mockRejectedValueOnce(new Error("offline"));
    read.mockImplementationOnce(() => new Promise<MarketList>((resolve, reject) => { finish = resolve; fail = reject; }));
    port.marketList = read;
    render(<Market port={port} onInstalled={() => {}} />);
    const more = await screen.findByRole("button", { name: "加载更多" });
    if (trigger === "retry") {
      await userEvent.click(more);
      await screen.findByRole("alert");
    }
    screen.getByRole("button", { name: trigger === "retry" ? "重试" : "加载更多" }).focus();
    await userEvent.keyboard("{Enter}");
    const market = screen.getByRole("region", { name: "社区市场" });
    expect(document.activeElement).toBe(market);
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "正在读取…" }).disabled).toBe(true);
    expect(read).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 2 }));
    await act(async () => {
      if (outcome === "success") finish({ packages: [], limit: 2, offset: 2 });
      else fail(new Error("still offline"));
    });
    expect(document.activeElement).toBe(market);
    expect(screen.getByText("one")).toBeTruthy();
    if (outcome === "success") expect(screen.queryByRole("button", { name: "加载更多" })).toBeNull();
    else expect(screen.getByRole("alert").textContent).toContain("still offline");
    await userEvent.tab();
    expect(document.activeElement).toBe(screen.getByRole("searchbox"));
  });

  it("retries a failed list request without changing its filters", async () => {
    const port = new MockPort() as unknown as AgentPort;
    let finish!: (value: MarketList) => void;
    port.marketList = vi.fn()
      .mockResolvedValueOnce({ packages: [], limit: 24, offset: 0 })
      .mockRejectedValueOnce(new Error("offline"))
      .mockImplementationOnce(() => new Promise<MarketList>((resolve) => { finish = resolve; }));
    render(<Market port={port} onInstalled={() => {}} />);

    await screen.findByText("没有找到已固定内容的包。可关闭筛选查看全部包。");
    await userEvent.type(screen.getByRole("searchbox"), "kit");
    await screen.findByText("无法读取社区市场");
    expect(screen.getByRole("alert").textContent).toContain("offline");
    expect(screen.queryByRole("button", { name: "查看全部包" })).toBeNull();
    screen.getByRole("button", { name: "重试" }).focus();
    await userEvent.keyboard("{Enter}");

    expect(screen.queryByText("无法读取社区市场")).toBeNull();
    expect(screen.queryByRole("button", { name: "重试" })).toBeNull();
    expect(screen.getByRole("status").textContent).toBe("正在读取…");
    const market = screen.getByRole("region", { name: "社区市场" });
    expect(document.activeElement).toBe(market);
    await act(async () => finish({ packages: [row("a/kit", true)], limit: 24, offset: 0 }));
    await screen.findByText("kit");
    expect(document.activeElement).toBe(market);
    await userEvent.tab();
    expect(document.activeElement).toBe(screen.getByRole("searchbox"));
    expect(port.marketList).toHaveBeenCalledTimes(3);
    expect(vi.mocked(port.marketList).mock.calls[2]).toEqual([{ ...vi.mocked(port.marketList).mock.calls[1][0], refresh: true }]);
    expect(vi.mocked(port.marketList).mock.calls[2]?.[0]).toMatchObject({ q: "kit", pinned: true, offset: 0 });
    expect(screen.queryByText("无法读取社区市场")).toBeNull();
  });

  it("retries a failed detail request before offering installation", async () => {
    const port = new MockPort() as unknown as AgentPort;
    let finish!: (value: MarketDetail) => void;
    port.marketList = async () => ({ packages: [row("a/kit", true)], limit: 24, offset: 0 });
    port.marketMyVote = async () => ({ signedIn: false, value: 0 });
    port.marketDetail = vi.fn()
      .mockRejectedValueOnce(new Error("offline"))
      .mockImplementationOnce(() => new Promise<MarketDetail>((resolve) => { finish = resolve; }));
    const plan = vi.spyOn(port, "planMarket");
    render(<Market port={port} onInstalled={() => {}} />);

    await userEvent.click(await screen.findByRole("button", { name: /kit/ }));
    await screen.findByText("无法读取 a/kit");
    screen.getByRole("button", { name: "重试" }).focus();
    await userEvent.keyboard("{Enter}");

    expect(screen.queryByText("无法读取 a/kit")).toBeNull();
    expect(screen.queryByRole("button", { name: "重试" })).toBeNull();
    expect(screen.getByRole("status").textContent).toBe("正在读取…");
    expect(screen.queryByRole("button", { name: "查看将安装的内容" })).toBeNull();
    const detail = screen.getByRole("region", { name: "a/kit" });
    expect(document.activeElement).toBe(detail);
    await act(async () => finish({ package: row("a/kit", true), pinned: true }));
    await screen.findByRole("button", { name: "查看将安装的内容" });
    expect(document.activeElement).toBe(detail);
    await userEvent.tab();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "返回列表" }));
    expect(port.marketDetail).toHaveBeenNthCalledWith(2, "a/kit", { refresh: true });
    expect(screen.queryByText("无法读取 a/kit")).toBeNull();
    expect(plan).not.toHaveBeenCalled();
  });

  it.each(["list", "detail"])("keeps keyboard focus in the %s after a retry fails again", async (target) => {
    const port = new MockPort() as unknown as AgentPort;
    let fail!: (error: Error) => void;
    const read = vi.fn()
      .mockRejectedValueOnce(new Error("offline"))
      .mockImplementationOnce(() => new Promise((_, reject) => { fail = reject; }));
    if (target === "list") port.marketList = read;
    else {
      port.marketList = async () => ({ packages: [row("a/kit", true)], limit: 24, offset: 0 });
      port.marketDetail = read;
    }
    render(<Market port={port} onInstalled={() => {}} />);
    if (target === "detail") await userEvent.click(await screen.findByRole("button", { name: /kit/ }));
    (await screen.findByRole("button", { name: "重试" })).focus();
    await userEvent.keyboard("{Enter}");
    const panel = screen.getByRole("region", { name: target === "list" ? "社区市场" : "a/kit" });
    expect(document.activeElement).toBe(panel);
    await act(async () => fail(new Error("still offline")));
    expect(screen.getByRole("alert").textContent).toContain("still offline");
    expect(document.activeElement).toBe(panel);
    await userEvent.tab();
    expect(document.activeElement).toBe(target === "list" ? screen.getByRole("searchbox") : screen.getByRole("button", { name: "返回列表" }));
    expect(read).toHaveBeenCalledTimes(2);
  });

  it.each(["success", "failure"])("ignores a stale detail %s after StrictMode replays the read", async (outcome) => {
    const port = new MockPort() as unknown as AgentPort;
    let finishOld!: (value: MarketDetail) => void;
    let failOld!: (error: Error) => void;
    let finishNew!: (value: MarketDetail) => void;
    port.marketList = async () => ({ packages: [row("a/kit", true)], limit: 24, offset: 0 });
    port.marketDetail = vi.fn()
      .mockImplementationOnce(() => new Promise<MarketDetail>((resolve, reject) => { finishOld = resolve; failOld = reject; }))
      .mockImplementationOnce(() => new Promise<MarketDetail>((resolve) => { finishNew = resolve; }));
    render(<StrictMode><Market port={port} onInstalled={() => {}} /></StrictMode>);

    await userEvent.click(await screen.findByRole("button", { name: /kit/ }));
    expect(port.marketDetail).toHaveBeenCalledTimes(2);
    await act(async () => finishNew({ package: { ...row("a/kit", true), description: "Current details" }, pinned: true }));
    await screen.findByText("Current details");
    await act(async () => {
      if (outcome === "success") finishOld({ package: { ...row("a/kit", true), description: "Old details" }, pinned: true });
      else failOld(new Error("old read failed"));
    });

    expect(screen.getByText("Current details")).toBeTruthy();
    expect(screen.queryByText("Old details")).toBeNull();
    expect(screen.queryByText("old read failed")).toBeNull();
  });

  it("clears the old detail while reading from a replacement port", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    let finish!: (value: MarketDetail) => void;
    port.marketList = async () => ({ packages: [row("a/kit", true)], limit: 24, offset: 0 });
    port.marketDetail = async () => ({ package: { ...row("a/kit", true), description: "Old details" }, pinned: true });
    next.marketDetail = vi.fn(() => new Promise<MarketDetail>((resolve) => { finish = resolve; }));
    const view = render(<Market port={port} onInstalled={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /kit/ }));
    await screen.findByText("Old details");

    view.rerender(<Market port={next} onInstalled={() => {}} />);
    expect(next.marketDetail).toHaveBeenCalledWith("a/kit");
    expect(screen.getByRole("status").textContent).toBe("正在读取…");
    expect(screen.queryByText("Old details")).toBeNull();
    expect(screen.queryByRole("button", { name: "查看将安装的内容" })).toBeNull();
    await act(async () => finish({ package: { ...row("a/kit", true), description: "Current details" }, pinned: true }));
    expect(screen.getByText("Current details")).toBeTruthy();
  });

  it("retries a failed next page without discarding loaded packages or advancing its offset", async () => {
    const port = new MockPort() as unknown as AgentPort;
    let finish!: (value: MarketList) => void;
    port.marketList = vi.fn()
      .mockResolvedValueOnce({ packages: [row("a/one", true), row("b/two", true)], limit: 2, offset: 0 })
      .mockRejectedValueOnce(new Error("offline"))
      .mockImplementationOnce(() => new Promise<MarketList>((resolve) => { finish = resolve; }));
    render(<Market port={port} onInstalled={() => {}} />);

    await userEvent.click(await screen.findByRole("button", { name: "加载更多" }));
    await screen.findByText("无法读取社区市场");
    await userEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(screen.getByText("one")).toBeTruthy();
    expect(screen.getByText("two")).toBeTruthy();
    expect(screen.queryByText("无法读取社区市场")).toBeNull();
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "正在读取…" }).disabled).toBe(true);
    expect(screen.getAllByText("正在读取…")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "正在读取…" }).getAttribute("aria-live")).toBe("polite");
    expect(vi.mocked(port.marketList).mock.calls[2]).toEqual(vi.mocked(port.marketList).mock.calls[1]);
    expect(vi.mocked(port.marketList).mock.calls[2]?.[0]).toMatchObject({ offset: 2 });
    await act(async () => finish({ packages: [row("b/two", true), row("c/three", true)], limit: 2, offset: 2 }));
    expect(screen.getByText("three")).toBeTruthy();
    expect(screen.getAllByText("two")).toHaveLength(1);
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "加载更多" }).disabled).toBe(false);
  });

  it("asks the registry for installable packages instead of filtering a page", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const asked: MarketQuery[] = [];
    port.marketList = vi.fn(async (q: MarketQuery) => {
      asked.push(q);
      return { packages: q.pinned ? [row("a/kit", true)] : [row("a/kit", true), row("b/raw", false), row("c/old")], limit: 24, offset: 0 };
    });
    render(<Market port={port} onInstalled={() => {}} />);

    await screen.findByText("kit");
    expect(asked[0]).toMatchObject({ pinned: true, offset: 0 });
    expect(screen.queryByText("raw")).toBeNull();
    await userEvent.click(screen.getByRole("checkbox", { name: "只看已固定" }));
    await screen.findByText("raw");
    expect(screen.getByText("已固定")).toBeTruthy();
    expect(screen.getByText("未固定")).toBeTruthy();
    expect(screen.getByText("固定状态未知")).toBeTruthy();
    expect(asked.at(-1)).toMatchObject({ pinned: false, offset: 0 });
  });

  it("offers all packages when the registry cannot filter", async () => {
    const port = new MockPort() as unknown as AgentPort;
    port.marketList = vi.fn(async (q: MarketQuery) => {
      if (q.pinned) throw new HttpError(502, "unsupported", { code: "market.filter_unsupported" });
      return { packages: [row("b/raw", false)], limit: 24, offset: 0 };
    });
    render(<Market port={port} onInstalled={() => {}} />);
    const showAll = await screen.findByRole("button", { name: "查看全部包" });
    expect(screen.getByRole("alert").textContent).toContain("社区市场暂不支持只列出已固定内容的包");
    expect(screen.queryByRole("button", { name: "重试" })).toBeNull();
    await userEvent.click(showAll);
    await screen.findByText("raw");
    expect(document.activeElement).toBe(screen.getByRole("region", { name: "社区市场" }));
    expect(screen.getByRole<HTMLInputElement>("checkbox", { name: "只看已固定" }).checked).toBe(false);
  });

  it("ignores a previous filtered page after switching to all packages", async () => {
    const port = new MockPort() as unknown as AgentPort;
    let finish!: (value: MarketList) => void;
    port.marketList = vi.fn((q: MarketQuery) => q.pinned
      ? new Promise<MarketList>((resolve) => { finish = resolve; })
      : Promise.resolve({ packages: [row("b/raw", false)], limit: 24, offset: 0 }));
    render(<Market port={port} onInstalled={() => {}} />);
    await waitFor(() => expect(finish).toBeDefined());
    await userEvent.click(screen.getByRole("checkbox", { name: "只看已固定" }));
    await screen.findByText("raw");
    finish({ packages: [row("a/kit", true)], limit: 24, offset: 0 });
    await waitFor(() => expect(screen.queryByText("kit")).toBeNull());
  });

  it("keeps pagination moving when adjacent registry pages overlap", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const asked: number[] = [];
    port.marketList = vi.fn(async (q: MarketQuery) => {
      const offset = q.offset ?? 0;
      asked.push(offset);
      const slugs = offset === 0 ? ["a/one", "b/two"] : offset === 2 ? ["b/two", "c/three"] : ["d/four"];
      return { packages: slugs.map((slug) => row(slug, true)), limit: 2, offset };
    });
    render(<Market port={port} onInstalled={() => {}} />);

    await screen.findByText("one");
    await userEvent.click(screen.getByRole("button", { name: "加载更多" }));
    await screen.findByText("three");
    expect(document.querySelectorAll('[data-action="market.open"][data-value="b/two"]')).toHaveLength(1);
    await userEvent.click(screen.getByRole("button", { name: "加载更多" }));
    await screen.findByText("four");
    expect(asked).toEqual([0, 2, 4]);
  });

  it("opens an entry from the keyboard", async () => {
    const port = new MockPort() as unknown as AgentPort;
    port.marketList = async () => ({ packages: [row("a/kit", true)], limit: 24, offset: 0 });
    port.marketDetail = vi.fn(async () => ({ package: row("a/kit", true), pinned: true }));
    render(<Market port={port} onInstalled={() => {}} />);

    (await screen.findByRole("button", { name: /kit/ })).focus();
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(port.marketDetail).toHaveBeenCalledWith("a/kit"));
  });

  it("lists each applied capability and opens its installed location", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const onViewInstalled = vi.fn();
    const install = port.installMarket.bind(port);
    port.installMarket = async (req) => {
      const out = await install(req);
      return { ...out, actions: [{ kind: "plugin", action: "install_plugin_package", status: "done", riskLevel: "high", name: "manifest-kit" }, ...(out.actions ?? [])] };
    };
    render(<Market port={port} onInstalled={() => {}} onViewInstalled={onViewInstalled} />);
    await userEvent.click(await screen.findByRole("button", { name: /review-kit/ }));
    await userEvent.click(await screen.findByRole("button", { name: "查看将安装的内容" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "我已看过这 3 个技能，全部安装" }));
    await userEvent.click(screen.getByRole("button", { name: "安装" }));
    await screen.findByRole("button", { name: "查看已安装能力" });
    expect(document.querySelector(".mkt-installed")?.textContent).toContain("review");
    expect(document.querySelector(".mkt-installed")?.textContent).toContain("pr-notes");
    await userEvent.click(screen.getByRole("button", { name: "查看已安装能力" }));
    expect(onViewInstalled).toHaveBeenCalledWith("plugin", "manifest-kit");
  });

  it("opens an installed theme package from the market outcome", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const onViewInstalled = vi.fn();
    render(<Market port={port} onInstalled={() => {}} onViewInstalled={onViewInstalled} />);

    await userEvent.click(await screen.findByRole("button", { name: /dusk-harbor/ }));
    await userEvent.click(await screen.findByRole("button", { name: "查看将安装的内容" }));
    await userEvent.click(screen.getByRole("button", { name: "安装" }));
    await userEvent.click(await screen.findByRole("button", { name: "查看已安装能力" }));

    expect(onViewInstalled).toHaveBeenCalledWith("plugin", "dusk-harbor");
  });

  it("installs an unpinned package only on trust, through the same preview", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const plan = vi.spyOn(port, "planMarket");
    const install = vi.spyOn(port, "installMarket");
    render(<Market port={port} onInstalled={() => {}} />);
    await userEvent.click(await screen.findByRole("checkbox", { name: "只看已固定" }));
    await userEvent.click(await screen.findByRole("button", { name: /lm-studio-vision-bridge/ }));

    await screen.findByText("未固定——审核时没有记录内容摘要");
    expect(screen.getByText("内容未经审核固定")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "查看将安装的内容" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "信任并安装" }));

    await screen.findByText("你选择了信任这个发布者");
    expect(plan).toHaveBeenCalledWith(expect.objectContaining({ slug: "1574022644/lm-studio-vision-bridge", trust: true }));
    expect(install).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "安装" }));

    await waitFor(() => expect(install).toHaveBeenCalledTimes(1));
    const req = install.mock.calls[0]![0];
    const shown = await plan.mock.results[0]!.value;
    expect(req).toMatchObject({ trust: true, digest: shown.contentDigest, planId: shown.planId, version: "2.0.0" });
  });

  it("does not ask for trust on a pinned package", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const plan = vi.spyOn(port, "planMarket");
    render(<Market port={port} onInstalled={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /make-ui-not-ai/ }));
    expect(screen.queryByRole("button", { name: "信任并安装" })).toBeNull();
    await userEvent.click(await screen.findByRole("button", { name: "查看将安装的内容" }));
    await screen.findByText("已按内容摘要核对：与审核时固定的版本一致。");
    expect(plan.mock.calls[0]![0].trust).toBeUndefined();
  });
});
