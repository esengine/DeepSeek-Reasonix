// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, MarketPlan, SessionStatus } from "../port/port";

afterEach(cleanup);

function draw(port: AgentPort, onClose = vi.fn(), onChanged = vi.fn()) {
  return <Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={onClose} onChanged={onChanged} onError={() => {}} at="ext:market"
    account={{ signedIn: true, user: { handle: "demo", email: "demo@example.com", label: "demo" } }}
    accountUnread="" reloadAccount={() => {}}
  />;
}

async function start() {
  await userEvent.click(screen.getByRole("radio", { name: "我的发布" }));
  const row = (await screen.findByText("ship-notes")).closest("li")!;
  await userEvent.click(within(row).getByRole("button", { name: "安装" }));
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
  return document.querySelector<HTMLElement>('.addpkg[data-stage="confirm"]')!;
}

it.each(["success", "failure", "refused"])("holds author installation through Settings navigation until %s", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const rows = await port.myMarket();
  const plan = await port.planOwnMarket({ slug: "demo/ship-notes" });
  const mine = vi.spyOn(port, "myMarket").mockResolvedValue(rows);
  const inventory = vi.spyOn(port, "plugins");
  let finish!: (plan: MarketPlan) => void;
  let fail!: (error: Error) => void;
  vi.spyOn(port, "installOwnMarket").mockImplementationOnce(() => new Promise((resolve, reject) => { finish = resolve; fail = reject; }));
  const onClose = vi.fn();
  const onChanged = vi.fn();
  render(draw(port, onClose, onChanged));
  const panel = await start();
  const reads = inventory.mock.calls.length;
  const close = document.querySelector<HTMLButtonElement>('[data-action="settings.close"]')!;
  const section = document.querySelector<HTMLButtonElement>('[data-action="settings.section"][data-value="session"]')!;
  const installed = screen.getByRole<HTMLButtonElement>("tab", { name: "已安装" });
  const browse = screen.getByRole<HTMLButtonElement>("radio", { name: "浏览" });
  const publish = screen.getByRole<HTMLButtonElement>("radio", { name: "发布" });
  const controls = [close, section, installed, browse, publish];
  for (const button of controls) {
    expect(button.disabled).toBe(true);
    await userEvent.click(button);
  }
  await userEvent.type(screen.getByRole("textbox", { name: "搜索设置" }), "模型");
  const found = screen.getAllByRole<HTMLButtonElement>("option");
  for (const button of found) {
    expect(button.disabled).toBe(true);
    await userEvent.click(button);
  }
  await userEvent.click(panel);
  await userEvent.keyboard("{Escape}");
  await userEvent.click(document.querySelector<HTMLElement>(".prefs")!);
  expect(onClose).not.toHaveBeenCalled();
  expect(document.querySelector('.addpkg[data-stage="confirm"]')).toBe(panel);
  expect(onChanged).not.toHaveBeenCalled();
  if (outcome === "success") mine.mockResolvedValue(rows.map((p) => p.slug === "demo/ship-notes" ? { ...p, installed: { version: p.latestVersion, contentHash: "installed-digest" } } : p));
  await act(async () => {
    if (outcome === "failure") fail(new Error("installation unavailable"));
    else finish({ ...plan, ok: outcome === "success", applied: outcome === "success", status: outcome === "success" ? "done" : "failed", error: outcome === "refused" ? "installation refused" : undefined });
  });
  for (const button of [...controls, ...found]) expect(button.disabled).toBe(false);
  expect(onChanged).toHaveBeenCalledTimes(outcome === "success" ? 1 : 0);
  if (outcome === "success") await waitFor(() => expect(inventory.mock.calls.length).toBeGreaterThan(reads));
  await userEvent.click(screen.getByRole("button", { name: outcome === "failure" ? "返回" : "返回我的发布" }));
  const row = (await screen.findByText("ship-notes")).closest("li")!;
  if (outcome === "success") {
    expect(mine.mock.calls.length).toBeGreaterThan(1);
    expect(within(row).getByText("已安装")).toBeTruthy();
    expect(within(row).queryByRole("button", { name: "安装" })).toBeNull();
  }
  await userEvent.click(browse);
  expect(screen.queryByText("ship-notes")).toBeNull();
  await userEvent.click(close);
  expect(onClose).toHaveBeenCalledTimes(1);
});

it("releases navigation on a port change without letting the old install unlock the new one", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const plan = await port.planOwnMarket({ slug: "demo/ship-notes" });
  let finishOld!: (plan: MarketPlan) => void;
  let finishNew!: (plan: MarketPlan) => void;
  vi.spyOn(port, "installOwnMarket").mockImplementationOnce(() => new Promise((resolve) => { finishOld = resolve; }));
  vi.spyOn(next, "installOwnMarket").mockImplementationOnce(() => new Promise((resolve) => { finishNew = resolve; }));
  const onChanged = vi.fn();
  const onClose = vi.fn();
  const view = render(draw(port, onClose, onChanged));
  await start();
  view.rerender(draw(next, onClose, onChanged));
  const close = document.querySelector<HTMLButtonElement>('[data-action="settings.close"]')!;
  expect(close.disabled).toBe(false);
  const row = (await screen.findByText("ship-notes")).closest("li")!;
  await userEvent.click(within(row).getByRole("button", { name: "安装" }));
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
  const panel = document.querySelector('.addpkg[data-stage="confirm"]');
  expect(close.disabled).toBe(true);
  await act(async () => finishOld({ ...plan, applied: true, status: "done" }));
  expect(close.disabled).toBe(true);
  expect(document.querySelector('.addpkg[data-stage="confirm"]')).toBe(panel);
  expect(onChanged).not.toHaveBeenCalled();
  await act(async () => finishNew({ ...plan, applied: true, status: "done" }));
  expect(close.disabled).toBe(false);
  expect(onChanged).toHaveBeenCalledTimes(1);
});
