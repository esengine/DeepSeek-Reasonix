// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Market, MarketGroup } from "./Market";
import { MockPort } from "../port/mock";
import type { AgentPort, MarketDetail, MarketList, MarketPlan } from "../port/port";

afterEach(cleanup);

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

async function fixture() {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const page = await port.marketList({ pinned: true });
  const pkg = page.packages[0]!;
  const detail = await port.marketDetail(pkg.slug);
  const plan = await port.planMarket({ slug: pkg.slug });
  const oldPage = { ...page, packages: [{ ...pkg, name: "old-kit" }, { ...pkg, name: "old-second", slug: "old/second" }], limit: 2 };
  const newPage = { ...page, packages: [{ ...pkg, name: "new-kit" }, { ...pkg, name: "new-second", slug: "new/second" }], limit: 2 };
  const list = vi.spyOn(port, "marketList").mockResolvedValue(oldPage);
  const newList = vi.spyOn(next, "marketList").mockResolvedValue(newPage);
  const oldDetail = vi.spyOn(port, "marketDetail").mockResolvedValue({ ...detail, package: oldPage.packages[0]! });
  const newDetail = vi.spyOn(next, "marketDetail").mockResolvedValue({ ...detail, package: newPage.packages[0]! });
  const newPlan = { ...plan, planId: "new-plan", version: "2.0.0" };
  const newPreview = vi.spyOn(next, "planMarket").mockResolvedValue(newPlan);
  const newInstall = vi.spyOn(next, "installMarket");
  return { port, next, pkg, detail, plan, oldPage, newPage, list, newList, oldDetail, newDetail, newPlan, newPreview, newInstall };
}

const installed = (plan: MarketPlan, name: string): MarketPlan => ({
  ...plan, applied: true, status: "done", actions: plan.actions?.map((a) => ({ ...a, name, status: "done" })),
});

it.each(["success", "failure"])("reloads the selected filters on a new connection and ignores the old page %s", async (outcome) => {
  const f = await fixture();
  const onInstalled = vi.fn();
  const view = render(<Market port={f.port} onInstalled={onInstalled} />);
  await screen.findByText("old-kit");
  await userEvent.click(screen.getByRole("radio", { name: "插件" }));
  await userEvent.click(screen.getByRole("radio", { name: "最新" }));
  await userEvent.click(screen.getByRole("checkbox", { name: "只看已固定" }));
  await userEvent.type(screen.getByRole("searchbox"), "kit");
  const query = { kind: "plugin", sort: "new", pinned: false, q: "kit", offset: 0 };
  await waitFor(() => expect(f.list).toHaveBeenLastCalledWith(query));
  const old = deferred<MarketList>();
  const fresh = deferred<MarketList>();
  f.list.mockImplementationOnce(() => old.promise);
  f.newList.mockImplementationOnce(() => fresh.promise);
  await userEvent.click(screen.getByRole("button", { name: "加载更多" }));
  view.rerender(<Market port={f.next} onInstalled={onInstalled} />);
  expect(screen.queryByText("old-kit")).toBeNull();
  expect(screen.getByRole<HTMLInputElement>("searchbox").value).toBe("kit");
  expect(screen.getByRole("radio", { name: "插件" }).getAttribute("aria-checked")).toBe("true");
  expect(screen.getByRole("radio", { name: "最新" }).getAttribute("aria-checked")).toBe("true");
  expect(screen.getByRole<HTMLInputElement>("checkbox", { name: "只看已固定" }).checked).toBe(false);
  await waitFor(() => expect(f.newList).toHaveBeenLastCalledWith(query));
  await act(async () => {
    if (outcome === "success") old.resolve({ ...f.oldPage, offset: 2 });
    else old.reject(new Error("old page unavailable"));
  });
  expect(screen.queryByText("old-kit")).toBeNull();
  expect(screen.queryByText("old page unavailable")).toBeNull();
  expect(screen.getByRole("status").textContent).toBe("正在读取…");
  await act(async () => fresh.resolve(f.newPage));
  await screen.findByText("new-kit");
  await userEvent.click(screen.getByRole("button", { name: "加载更多" }));
  expect(f.newList).toHaveBeenLastCalledWith({ ...query, offset: 2 });
});

it.each(["success", "failure"])("keeps the public selection but ignores old detail %s while the new connection reads it", async (outcome) => {
  const f = await fixture();
  const old = deferred<MarketDetail>();
  const fresh = deferred<MarketDetail>();
  f.oldDetail.mockImplementationOnce(() => old.promise);
  f.newDetail.mockImplementationOnce(() => fresh.promise);
  const view = render(<Market port={f.port} onInstalled={() => {}} />);
  await userEvent.click(await screen.findByRole("button", { name: /old-kit/ }));
  await waitFor(() => expect(f.oldDetail).toHaveBeenCalledTimes(1));
  view.rerender(<Market port={f.next} onInstalled={() => {}} />);
  expect(f.newDetail).toHaveBeenCalledWith(f.pkg.slug);
  expect(screen.getByRole("status").textContent).toBe("正在读取…");
  await act(async () => {
    if (outcome === "success") old.resolve(f.detail);
    else old.reject(new Error("old detail unavailable"));
  });
  expect(screen.queryByText("old detail unavailable")).toBeNull();
  expect(screen.queryByRole("button", { name: "查看将安装的内容" })).toBeNull();
  await act(async () => fresh.resolve({ ...f.detail, package: f.newPage.packages[0]! }));
  await screen.findByRole("button", { name: "查看将安装的内容" });
  expect(f.newDetail).toHaveBeenCalledTimes(1);
});

it.each(["success", "failure"])("discards an old preview %s while the new connection applies its own confirmation", async (outcome) => {
  const f = await fixture();
  const old = deferred<MarketPlan>();
  const fresh = deferred<MarketPlan>();
  vi.spyOn(f.port, "planMarket").mockImplementationOnce(() => old.promise);
  f.newInstall.mockImplementationOnce(() => fresh.promise);
  const onInstalled = vi.fn();
  const view = render(<Market port={f.port} onInstalled={onInstalled} />);
  await userEvent.click(await screen.findByRole("button", { name: /old-kit/ }));
  await userEvent.click(await screen.findByRole("button", { name: "查看将安装的内容" }));
  view.rerender(<Market port={f.next} onInstalled={onInstalled} />);
  expect(f.newDetail).toHaveBeenCalledWith(f.pkg.slug);
  await userEvent.click(await screen.findByRole("button", { name: "查看将安装的内容" }));
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
  const applying = screen.getByRole<HTMLButtonElement>("button", { name: "安装中…" });
  await act(async () => {
    if (outcome === "success") old.resolve(f.plan);
    else old.reject(new Error("old preview unavailable"));
  });
  expect(screen.getByText(`${f.pkg.slug} 2.0.0 将安装以下内容`)).toBeTruthy();
  expect(screen.queryByText("old preview unavailable")).toBeNull();
  expect(applying.disabled).toBe(true);
  expect(f.newInstall).toHaveBeenCalledWith({ slug: f.pkg.slug, version: "2.0.0", planId: "new-plan", replace: false });
  await act(async () => fresh.resolve(installed(f.newPlan, "new-capability")));
  expect(onInstalled).toHaveBeenCalledTimes(1);
});

it.each(["confirm", "done"])("clears an old %s view and requires a fresh preview before installation on the new connection", async (stage) => {
  const f = await fixture();
  vi.spyOn(f.port, "installMarket").mockResolvedValue(installed(f.plan, "old-capability"));
  const onInstalled = vi.fn();
  const view = render(<Market port={f.port} onInstalled={onInstalled} />);
  await userEvent.click(await screen.findByRole("button", { name: /old-kit/ }));
  await userEvent.click(await screen.findByRole("button", { name: "查看将安装的内容" }));
  await screen.findByRole("button", { name: "安装" });
  if (stage === "done") {
    await userEvent.click(screen.getByRole("button", { name: "安装" }));
    await screen.findByText("装好了，下一轮就能用");
  }
  view.rerender(<Market port={f.next} onInstalled={onInstalled} />);
  expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
  expect(screen.queryByRole("button", { name: "查看已安装能力" })).toBeNull();
  expect(f.newDetail).toHaveBeenCalledWith(f.pkg.slug);
  expect(f.newPreview).not.toHaveBeenCalled();
  expect(f.newInstall).not.toHaveBeenCalled();
  await userEvent.click(await screen.findByRole("button", { name: "查看将安装的内容" }));
  await screen.findByText(`${f.pkg.slug} 2.0.0 将安装以下内容`);
  expect(f.newPreview).toHaveBeenCalledTimes(1);
  expect(f.newInstall).not.toHaveBeenCalled();
});

it.each(["success", "failure"])("ignores an old installation %s while the new connection is applying", async (outcome) => {
  const f = await fixture();
  const old = deferred<MarketPlan>();
  const fresh = deferred<MarketPlan>();
  vi.spyOn(f.port, "installMarket").mockImplementationOnce(() => old.promise);
  f.newInstall.mockImplementationOnce(() => fresh.promise);
  const onInstalled = vi.fn();
  const view = render(<Market port={f.port} onInstalled={onInstalled} />);
  await userEvent.click(await screen.findByRole("button", { name: /old-kit/ }));
  await userEvent.click(await screen.findByRole("button", { name: "查看将安装的内容" }));
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
  view.rerender(<Market port={f.next} onInstalled={onInstalled} />);
  expect(f.newDetail).toHaveBeenCalledWith(f.pkg.slug);
  await userEvent.click(await screen.findByRole("button", { name: "查看将安装的内容" }));
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
  const applying = screen.getByRole<HTMLButtonElement>("button", { name: "安装中…" });
  const reads = f.list.mock.calls.length;
  await act(async () => {
    if (outcome === "success") old.resolve(installed(f.plan, "old-capability"));
    else old.reject(new Error("old install unavailable"));
  });
  expect(screen.queryAllByText("old-capability")).toHaveLength(0);
  expect(screen.queryByText("old install unavailable")).toBeNull();
  expect(screen.getByText(`${f.pkg.slug} 2.0.0 将安装以下内容`)).toBeTruthy();
  expect(applying.disabled).toBe(true);
  expect(f.list).toHaveBeenCalledTimes(reads);
  expect(onInstalled).not.toHaveBeenCalled();
  await act(async () => fresh.resolve(installed(f.newPlan, "new-capability")));
  expect(document.querySelector(".mkt-installed")?.textContent).toContain("new-capability");
  expect(onInstalled).toHaveBeenCalledTimes(1);
});

it("still notifies the same connection after leaving the applying market view", async () => {
  const f = await fixture();
  const finish = deferred<MarketPlan>();
  vi.spyOn(f.port, "installMarket").mockImplementationOnce(() => finish.promise);
  const onInstalled = vi.fn();
  render(<MarketGroup port={f.port} onInstalled={onInstalled} onSignIn={() => {}}
    account={{ signedIn: true, user: { handle: "demo", email: "demo@example.com", label: "demo" } }} />);
  await userEvent.click(await screen.findByRole("button", { name: /old-kit/ }));
  await userEvent.click(await screen.findByRole("button", { name: "查看将安装的内容" }));
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
  await userEvent.click(screen.getByRole("radio", { name: "发布" }));
  const reads = f.list.mock.calls.length;
  await act(async () => finish.resolve(installed(f.plan, "same-connection-capability")));
  expect(onInstalled).toHaveBeenCalledTimes(1);
  expect(document.querySelector(".mkt-pub")).toBeTruthy();
  expect(screen.queryByText("same-connection-capability")).toBeNull();
  await userEvent.click(screen.getByRole("radio", { name: "浏览" }));
  await screen.findByText("old-kit");
  expect(f.list.mock.calls.length).toBeGreaterThan(reads);
});
