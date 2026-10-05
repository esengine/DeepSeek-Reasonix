// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { OwnInstall } from "./MarketOwn";
import { MyPackages } from "./MarketPublish";
import { MarketGroup } from "./Market";
import { MockPort } from "../port/mock";
import type { AccountState, AgentPort, MarketPlan } from "../port/port";

afterEach(cleanup);

const signedIn: AccountState = { signedIn: true, user: { handle: "demo", email: "demo@example.com", label: "demo" } };

async function pendingInstall(port: AgentPort) {
  const pkg = (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
  const plan = await port.planOwnMarket({ slug: pkg.slug });
  let finish!: (out: MarketPlan) => void;
  vi.spyOn(port, "installOwnMarket").mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  return { pkg, finish: () => finish({ ...plan, applied: true, status: "done" }) };
}

async function installFromList() {
  const row = (await screen.findByText("ship-notes")).closest("li")!;
  await userEvent.click(within(row).getByRole("button", { name: "安装" }));
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
}

it("notifies a successful install after the confirmation unmounts", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const pending = await pendingInstall(port);
  const onInstalled = vi.fn();
  const view = render(<OwnInstall port={port} pkg={pending.pkg} onBack={() => {}} onInstalled={onInstalled} />);
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
  view.unmount();
  await act(async () => pending.finish());
  expect(onInstalled).toHaveBeenCalledTimes(1);
});

it("does not notify an install from an earlier lifetime of the same port", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const pending = await pendingInstall(port);
  const onInstalled = vi.fn();
  const draw = (active: AgentPort) => <OwnInstall port={active} pkg={pending.pkg} onBack={() => {}} onInstalled={onInstalled} />;
  const view = render(draw(port));
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
  view.rerender(draw(next));
  await screen.findByRole("button", { name: "安装" });
  view.rerender(draw(port));
  await screen.findByRole("button", { name: "安装" });
  await act(async () => pending.finish());
  expect(onInstalled).not.toHaveBeenCalled();
  expect(screen.queryByText("装好了，下一轮就能用")).toBeNull();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "安装" }).disabled).toBe(false);
});

it.each([false, true])("ignores the old keyed install after MyPackages changes port (return=%s)", async (returnToPort) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const pending = await pendingInstall(port);
  const onInstalled = vi.fn();
  const draw = (active: AgentPort) => <MyPackages port={active} onInstalled={onInstalled} />;
  const view = render(draw(port));
  await installFromList();
  view.rerender(draw(next));
  await screen.findByText("ship-notes");
  if (returnToPort) {
    view.rerender(draw(port));
    await screen.findByText("ship-notes");
  }
  await act(async () => pending.finish());
  expect(onInstalled).not.toHaveBeenCalled();
  expect(screen.queryByText("装好了，下一轮就能用")).toBeNull();
});

it.each([false, true])("ignores the signed-out install after MarketGroup changes port (return=%s)", async (returnToPort) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const pending = await pendingInstall(port);
  const onInstalled = vi.fn();
  const draw = (active: AgentPort, account: AccountState) => <MarketGroup port={active} account={account} onInstalled={onInstalled} onSignIn={() => {}} />;
  const view = render(draw(port, signedIn));
  await userEvent.click(screen.getByRole("radio", { name: "我的发布" }));
  await installFromList();
  view.rerender(draw(port, { signedIn: false }));
  await screen.findByRole("button", { name: "去登录" });
  view.rerender(draw(next, { signedIn: false }));
  if (returnToPort) view.rerender(draw(port, { signedIn: false }));
  await act(async () => pending.finish());
  expect(onInstalled).not.toHaveBeenCalled();
  expect(screen.queryByText("装好了，下一轮就能用")).toBeNull();
  expect(screen.getByRole("button", { name: "去登录" })).toBeTruthy();
});
