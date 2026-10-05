// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, MarketPlan, SessionStatus } from "../port/port";

afterEach(cleanup);

function draw(port: AgentPort, onChanged: () => void) {
  return <Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={onChanged} onError={() => {}} at="ext:market"
    account={{ signedIn: true, user: { handle: "demo", email: "demo@example.com", label: "demo" } }}
    accountUnread="" reloadAccount={() => {}}
  />;
}

it.each(["same", "replacement"])("settles an install after leaving Browse with a %s Settings connection", async (connection) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const pkg = (await port.marketList({ pinned: true })).packages[0]!;
  const plan = await port.planMarket({ slug: pkg.slug });
  let finish!: (plan: MarketPlan) => void;
  vi.spyOn(port, "installMarket").mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  const inventory = vi.spyOn(port, "plugins");
  const onChanged = vi.fn();
  const view = render(draw(port, onChanged));
  await userEvent.click(await screen.findByRole("button", { name: new RegExp(pkg.name) }));
  await userEvent.click(await screen.findByRole("button", { name: "查看将安装的内容" }));
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
  await userEvent.click(screen.getByRole("radio", { name: "发布" }));
  if (connection === "replacement") view.rerender(draw(next, onChanged));
  const reads = inventory.mock.calls.length;
  await act(async () => finish({ ...plan, applied: true, status: "done" }));
  expect(onChanged).toHaveBeenCalledTimes(connection === "same" ? 1 : 0);
  expect(inventory.mock.calls.length).toBe(connection === "same" ? reads + 1 : reads);
  expect(document.querySelector(".mkt-pub")).toBeTruthy();
  expect(screen.queryByText("装好了，下一轮就能用")).toBeNull();
});
