// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockPort } from "../port/mock";
import type { AccountState, AgentPort, MarketPackage, MarketPlan } from "../port/port";
import { MarketGroup } from "./Market";

afterEach(cleanup);

const account = (handle: string): AccountState => ({
  signedIn: true, user: { handle, email: `${handle}@example.com`, label: handle },
});
const draw = (port: AgentPort, handle: string, onInstalled = () => {}) => (
  <MarketGroup port={port} account={account(handle)} onInstalled={onInstalled} onSignIn={() => {}} />
);

it("keeps an open author preview when the same account state is refreshed", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const read = vi.spyOn(port, "myMarket");
  const preview = vi.spyOn(port, "planOwnMarket");
  const view = render(draw(port, "demo"));
  await userEvent.click(screen.getByRole("radio", { name: "我的发布" }));
  const row = (await screen.findByText("ship-notes")).closest("li")!;
  await userEvent.click(within(row).getByRole("button", { name: "安装" }));
  await screen.findByText(/demo\/ship-notes .* 将安装以下内容/);
  view.rerender(draw(port, "demo"));
  expect(screen.getByText(/demo\/ship-notes .* 将安装以下内容/)).toBeTruthy();
  expect(read).toHaveBeenCalledTimes(1);
  expect(preview).toHaveBeenCalledTimes(1);
});

it("refreshes installed capabilities when the old author's install completes on the same host", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const pkg = (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
  const plan = await port.planOwnMarket({ slug: pkg.slug });
  const fresh = { ...pkg, handle: "other", slug: "other/new-package", name: "new-package" };
  vi.spyOn(port, "myMarket").mockResolvedValueOnce([pkg]).mockResolvedValue([fresh]);
  let finish!: (out: MarketPlan) => void;
  vi.spyOn(port, "installOwnMarket").mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  const onInstalled = vi.fn();
  const view = render(draw(port, "demo", onInstalled));
  await userEvent.click(screen.getByRole("radio", { name: "我的发布" }));
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
  view.rerender(draw(port, "other", onInstalled));
  await screen.findByText("new-package");
  await act(async () => finish({ ...plan, applied: true, status: "done" }));
  expect(onInstalled).toHaveBeenCalledTimes(1);
  expect(screen.getByText("new-package")).toBeTruthy();
  expect(screen.queryByText("装好了，下一轮就能用")).toBeNull();
});

it("closes the old author's preview when the account changes on the same connection", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const pkg = (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
  const fresh = { ...pkg, handle: "other", slug: "other/new-package", name: "new-package" };
  const read = vi.spyOn(port, "myMarket").mockResolvedValueOnce([pkg]).mockResolvedValue([fresh]);
  const preview = vi.spyOn(port, "planOwnMarket");
  const install = vi.spyOn(port, "installOwnMarket");
  const view = render(draw(port, "demo"));
  await userEvent.click(screen.getByRole("radio", { name: "我的发布" }));
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
  await screen.findByText(/demo\/ship-notes .* 将安装以下内容/);
  view.rerender(draw(port, "other"));
  expect(await screen.findByText("new-package")).toBeTruthy();
  expect(screen.queryByText(/demo\/ship-notes .* 将安装以下内容/)).toBeNull();
  expect(read).toHaveBeenCalledTimes(2);
  expect(preview).toHaveBeenCalledTimes(1);
  expect(install).not.toHaveBeenCalled();
});

it.each([
  ["success", false], ["failure", false], ["success", true], ["failure", true],
])("ignores the earlier account's list %s on the same connection (return=%s)", async (outcome, returnToAccount) => {
  const port = new MockPort() as unknown as AgentPort;
  const pkg = (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
  const other = { ...pkg, handle: "other", slug: "other/current-package", name: "current-package" };
  const fresh = { ...pkg, slug: "demo/fresh-package", name: "fresh-package" };
  let finish!: (rows: MarketPackage[]) => void;
  let fail!: (error: Error) => void;
  const read = vi.spyOn(port, "myMarket")
    .mockImplementationOnce(() => new Promise((resolve, reject) => { finish = resolve; fail = reject; }))
    .mockResolvedValueOnce([other]).mockResolvedValue([fresh]);
  const view = render(draw(port, "demo"));
  await userEvent.click(screen.getByRole("radio", { name: "我的发布" }));
  view.rerender(draw(port, "other"));
  await screen.findByText("current-package");
  if (returnToAccount) {
    view.rerender(draw(port, "demo"));
    await screen.findByText("fresh-package");
  }
  await act(async () => {
    if (outcome === "success") finish([pkg]);
    else fail(new Error("old account inventory failed"));
  });
  expect(screen.getByText(returnToAccount ? "fresh-package" : "current-package")).toBeTruthy();
  expect(screen.queryByText("ship-notes")).toBeNull();
  expect(screen.queryByText("old account inventory failed")).toBeNull();
  expect(read).toHaveBeenCalledTimes(returnToAccount ? 3 : 2);
});
