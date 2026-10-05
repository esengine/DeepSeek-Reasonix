// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MarketGroup } from "./Market";
import { MyPackages } from "./MarketPublish";
import { OwnInstall } from "./MarketOwn";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/port";
import type { AgentPort, MarketPackage, MarketPlan } from "../port/port";

afterEach(cleanup);

const signedIn = { signedIn: true, user: { handle: "demo", email: "demo@example.com", label: "demo" } };
const pinned = "https://github.com/demo/themes/tree/" + "a".repeat(40) + "/dusk";

async function openMine(port: AgentPort, onInstalled = () => {}, onViewInstalled?: (kind: string, name: string) => void) {
  render(<MarketGroup port={port} onInstalled={onInstalled} onViewInstalled={onViewInstalled} account={signedIn} onSignIn={() => {}} />);
  await userEvent.click(screen.getByRole("radio", { name: "我的发布" }));
}

describe("the account's own packages", () => {
  it.each(["success", "failure"])("ignores a stale list %s after replacing the port", async (outcome) => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const rows = await port.myMarket();
    let finishOld!: (value: MarketPackage[]) => void;
    let failOld!: (error: Error) => void;
    let finishNew!: (value: MarketPackage[]) => void;
    vi.spyOn(port, "myMarket").mockImplementation(() => new Promise((resolve, reject) => { finishOld = resolve; failOld = reject; }));
    vi.spyOn(next, "myMarket").mockImplementation(() => new Promise((resolve) => { finishNew = resolve; }));
    const view = render(<MyPackages port={port} onInstalled={() => {}} />);
    view.rerender(<MyPackages port={next} onInstalled={() => {}} />);
    await act(async () => finishNew([{ ...rows[0]!, name: "current-package", slug: "demo/current-package" }]));
    await screen.findByText("current-package");
    await act(async () => {
      if (outcome === "success") finishOld(rows);
      else failOld(new Error("old list failed"));
    });

    expect(screen.getByText("current-package")).toBeTruthy();
    expect(screen.queryByText(rows[0]!.name)).toBeNull();
    expect(screen.queryByText("old list failed")).toBeNull();
  });

  it.each(["success", "failure"])("keeps a replacement preview pending after a stale %s", async (outcome) => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const pkg = (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
    const shown = await port.planOwnMarket({ slug: pkg.slug });
    let finishOld!: (value: MarketPlan) => void;
    let failOld!: (error: Error) => void;
    let finishNew!: (value: MarketPlan) => void;
    vi.spyOn(port, "planOwnMarket").mockImplementation(() => new Promise((resolve, reject) => { finishOld = resolve; failOld = reject; }));
    vi.spyOn(next, "planOwnMarket").mockImplementation(() => new Promise((resolve) => { finishNew = resolve; }));
    const install = vi.spyOn(next, "installOwnMarket");
    const view = render(<OwnInstall port={port} pkg={pkg} onBack={() => {}} onInstalled={() => {}} />);
    view.rerender(<OwnInstall port={next} pkg={pkg} onBack={() => {}} onInstalled={() => {}} />);
    await act(async () => {
      if (outcome === "success") finishOld(shown);
      else failOld(new Error("old preview failed"));
    });

    expect(screen.getByText("正在预览将安装的内容…").closest(".mkt")?.getAttribute("aria-busy")).toBe("true");
    expect(screen.queryByText("old preview failed")).toBeNull();
    expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
    expect(screen.queryByRole("button", { name: "重试" })).toBeNull();
    expect(install).not.toHaveBeenCalled();
    await act(async () => finishNew(shown));
    expect(await screen.findByRole("button", { name: "安装" })).toBeTruthy();
    expect(install).not.toHaveBeenCalled();
  });

  it("clears a previous confirmation and installs only the replacement preview", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const pkg = (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
    const shown = await port.planOwnMarket({ slug: pkg.slug });
    const fresh = { ...shown, planId: "replacement-preview", contentDigest: "b".repeat(64) };
    vi.spyOn(port, "planOwnMarket").mockResolvedValue(shown);
    let finish!: (value: MarketPlan) => void;
    vi.spyOn(next, "planOwnMarket").mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    const install = vi.spyOn(next, "installOwnMarket").mockResolvedValue({ ...fresh, applied: true, status: "done" });
    const view = render(<OwnInstall port={port} pkg={pkg} onBack={() => {}} onInstalled={() => {}} />);
    await screen.findByRole("button", { name: "安装" });
    view.rerender(<OwnInstall port={next} pkg={pkg} onBack={() => {}} onInstalled={() => {}} />);

    expect(screen.getByText("正在预览将安装的内容…")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
    expect(install).not.toHaveBeenCalled();
    await act(async () => finish(fresh));
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    expect(install).toHaveBeenCalledWith({ slug: pkg.slug, version: fresh.version, planId: fresh.planId, replace: false, digest: fresh.contentDigest });
  });

  it("retries a failed list read without leaving My Packages", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const rows = await port.myMarket();
    let finish!: (rows: MarketPackage[]) => void;
    const read = vi.spyOn(port, "myMarket")
      .mockRejectedValueOnce(new Error("list unavailable"))
      .mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    await openMine(port);
    await screen.findByText("list unavailable");
    await userEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(screen.queryByText("list unavailable")).toBeNull();
    expect(screen.getByText("正在读取…")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "重试" })).toBeNull();

    await act(async () => finish(rows));
    expect(await screen.findByText("ship-notes")).toBeTruthy();
    expect(read).toHaveBeenCalledTimes(2);
    expect(screen.queryByText("无法读取我的发布")).toBeNull();
  });

  it("shows an empty account after a successful retry", async () => {
    const port = new MockPort() as unknown as AgentPort;
    vi.spyOn(port, "myMarket").mockRejectedValueOnce(new Error("list unavailable")).mockResolvedValueOnce([]);
    await openMine(port);
    await screen.findByText("list unavailable");
    await userEvent.click(screen.getByRole("button", { name: "重试" }));

    expect(await screen.findByText("还没有发布过。")).toBeTruthy();
    expect(screen.queryByText("无法读取我的发布")).toBeNull();
  });

  it("retries an own-package preview and requires confirmation of the recovered plan", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const shown = await port.planOwnMarket({ slug: "demo/ship-notes" });
    let finish!: (plan: MarketPlan) => void;
    const preview = vi.spyOn(port, "planOwnMarket")
      .mockRejectedValueOnce(new Error("preview unavailable"))
      .mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    const install = vi.spyOn(port, "installOwnMarket");
    await openMine(port);
    const row = (await screen.findByText("ship-notes")).closest("li")!;
    await userEvent.click(row.querySelector<HTMLButtonElement>('[data-action="market.own-inspect"]')!);
    await screen.findByText("preview unavailable");
    await userEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(screen.queryByText("preview unavailable")).toBeNull();
    expect(screen.getByText("正在预览将安装的内容…")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
    expect(preview.mock.calls).toEqual([[{ slug: "demo/ship-notes", replace: false }], [{ slug: "demo/ship-notes", replace: false }]]);

    await act(async () => finish(shown));
    const confirm = await screen.findByRole("button", { name: "安装" });
    expect(install).not.toHaveBeenCalled();
    await userEvent.click(confirm);
    expect(install.mock.calls[0]![0]).toEqual({
      slug: "demo/ship-notes", version: shown.version, planId: shown.planId, replace: false, digest: shown.contentDigest,
    });
  });

  it.each([["ship-notes", "skill"], ["night-desk", "plugin"], ["lint-kit", "plugin"]])(
    "opens the installed capability after installing %s",
    async (name, kind) => {
      const port = new MockPort() as unknown as AgentPort;
      const onViewInstalled = vi.fn();
      const install = port.installOwnMarket.bind(port);
      port.installOwnMarket = async (req) => {
        const out = await install(req);
        const actions = out.actions?.map((a) => ({ ...a, name: `${name}-local` })) ?? [];
        return { ...out, actions: name === "lint-kit"
          ? [{ kind: "skill", action: "copy_skill", status: "done", riskLevel: "low", name: "bundled-notes" }, ...actions] : actions };
      };
      await openMine(port, () => {}, onViewInstalled);
      const row = (await screen.findByText(name)).closest("li")!;
      await userEvent.click(row.querySelector<HTMLButtonElement>('[data-action="market.own-inspect"]')!);
      await userEvent.click(await screen.findByRole("button", { name: "安装" }));
      await userEvent.click(await screen.findByRole("button", { name: "查看已安装能力" }));

      expect(onViewInstalled).toHaveBeenCalledWith(kind, `${name}-local`);
    },
  );

  it("offers no installed location when the install was not applied", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const onViewInstalled = vi.fn();
    port.installOwnMarket = async (req) => ({ ...await port.planOwnMarket(req), ok: false, status: "blocked", applied: false, error: "refused" });
    await openMine(port, () => {}, onViewInstalled);
    const row = (await screen.findByText("ship-notes")).closest("li")!;
    await userEvent.click(row.querySelector<HTMLButtonElement>('[data-action="market.own-inspect"]')!);
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    await screen.findByText("refused");

    expect(screen.queryByRole("button", { name: "查看已安装能力" })).toBeNull();
    expect(onViewInstalled).not.toHaveBeenCalled();
  });

  it("locates only the successful server after a partial author install", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const pkg = (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
    const shown = await port.planOwnMarket({ slug: pkg.slug });
    const planned: MarketPlan = { ...shown, actions: [
      { kind: "mcp", action: "install_mcp_server", name: "unavailable-server", status: "planned", riskLevel: "high" },
      { kind: "mcp", action: "install_mcp_server", name: "working-server", status: "planned", riskLevel: "high" },
    ] };
    vi.spyOn(port, "myMarket").mockResolvedValue([{ ...pkg, kind: "mcp" }]);
    vi.spyOn(port, "planOwnMarket").mockResolvedValue(planned);
    vi.spyOn(port, "installOwnMarket").mockResolvedValue({ ...planned, ok: false, applied: true, status: "partial", next: "Some actions failed", actions: [
      { ...planned.actions![0]!, status: "failed", error: "server unavailable" },
      { ...planned.actions![1]!, status: "done" },
    ] });
    const onViewInstalled = vi.fn();
    const onInstalled = vi.fn();
    await openMine(port, onInstalled, onViewInstalled);
    const row = (await screen.findByText(pkg.name)).closest("li")!;
    await userEvent.click(row.querySelector<HTMLButtonElement>('[data-action="market.own-inspect"]')!);
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    await screen.findByText("有项目未安装成功，原因见下方。");
    expect(screen.getByRole("alert").textContent).toContain("unavailable-server: server unavailable");

    const installed = document.querySelector(".mkt-installed")!;
    expect(installed.textContent).toContain("working-server");
    expect(installed.textContent).not.toContain("unavailable-server");
    expect(onInstalled).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole("button", { name: "查看已安装能力" }));
    expect(onViewInstalled).toHaveBeenCalledWith("mcp", "working-server");
  });

  it("saves a private package out of review and shows it as private", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const publish = vi.spyOn(port, "publishMarket");
    render(<MarketGroup port={port} onInstalled={() => {}} account={signedIn} onSignIn={() => {}} />);
    await userEvent.click(screen.getByRole("radio", { name: "发布" }));
    await userEvent.click(screen.getByRole("radio", { name: "主题" }));
    fireEvent.change(document.querySelector('[data-value="name"]')!, { target: { value: "dusk" } });
    fireEvent.change(document.querySelector('[data-value="source"]')!, { target: { value: pinned } });
    await userEvent.click(screen.getByRole("checkbox", { name: /仅自己可见/ }));
    await userEvent.click(screen.getByRole("button", { name: "保存为私有" }));

    await waitFor(() => expect(publish).toHaveBeenCalled());
    expect(publish.mock.calls[0]![0]).toMatchObject({ name: "dusk", visibility: "private" });
    expect(await screen.findByText("已保存 demo/dusk 0.1.0，仅自己可见")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "查看我的发布" }));
    const row = (await screen.findByText("dusk")).closest("li")!;
    expect(row.textContent).toContain("私有");
    expect(row.textContent).toContain("提交审核");
  });

  it("sends a private package to review only on request", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const submit = vi.spyOn(port, "submitMarket");
    await openMine(port);
    const row = (await screen.findByText("night-desk")).closest("li")!;
    expect(row.textContent).toContain("私有");
    expect(submit).not.toHaveBeenCalled();
    await userEvent.click(row.querySelector<HTMLButtonElement>('[data-action="market.submit"]')!);
    await waitFor(() => expect(submit).toHaveBeenCalledWith("demo/night-desk"));
    await waitFor(() => expect(row.textContent).toContain("审核中"));
    expect(row.querySelector('[data-action="market.submit"]')).toBeNull();
  });

  it("installs an unreviewed package against the digest its preview showed", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const plan = vi.spyOn(port, "planOwnMarket");
    const install = vi.spyOn(port, "installOwnMarket");
    const onInstalled = vi.fn();
    await openMine(port, onInstalled);
    const row = (await screen.findByText("ship-notes")).closest("li")!;
    await userEvent.click(row.querySelector<HTMLButtonElement>('[data-action="market.own-inspect"]')!);

    expect(await screen.findByText("未审核 · 仅你可见")).toBeTruthy();
    expect(plan).toHaveBeenCalledWith({ slug: "demo/ship-notes", replace: false });
    expect(install).not.toHaveBeenCalled();
    const shown = await plan.mock.results[0]!.value;
    await userEvent.click(screen.getByRole("button", { name: "安装" }));

    await waitFor(() => expect(install).toHaveBeenCalled());
    expect(install.mock.calls[0]![0]).toEqual({
      slug: "demo/ship-notes", version: shown.version, planId: shown.planId, replace: false, digest: shown.contentDigest,
    });
    await waitFor(() => expect(onInstalled).toHaveBeenCalled());
  });

  it("offers nothing to install when the kernel cannot pin the preview", async () => {
    const port = new MockPort() as unknown as AgentPort;
    vi.spyOn(port, "planOwnMarket").mockRejectedValue(
      new HttpError(409, "not pinnable", { code: "market.not_pinnable", error: "not pinnable" }),
    );
    const install = vi.spyOn(port, "installOwnMarket");
    await openMine(port);
    const row = (await screen.findByText("lint-kit")).closest("li")!;
    await userEvent.click(row.querySelector<HTMLButtonElement>('[data-action="market.own-inspect"]')!);
    expect(await screen.findByText(/该来源的内容无法固定/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
    expect(install).not.toHaveBeenCalled();
  });
});
