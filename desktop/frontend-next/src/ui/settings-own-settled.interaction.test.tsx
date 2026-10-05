// @vitest-environment jsdom
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AccountState, AgentPort, MarketPlan, SessionStatus } from "../port/port";

afterEach(cleanup);

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function AccountSettings({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [account, setAccount] = useState<AccountState>({
    signedIn: true, user: { handle: "demo", email: "demo@example.com", label: "demo" },
  });
  return <Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={onChanged} onError={() => {}} at="account"
    account={account} accountUnread="" reloadAccount={() => { void port.account().then(setAccount); }}
  />;
}

async function startOwnInstall() {
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="settings.section"][data-value="ext"]')!);
  await userEvent.click(screen.getByRole("tab", { name: "发现" }));
  await userEvent.click(screen.getByRole("radio", { name: "我的发布" }));
  const row = (await screen.findByText("ship-notes")).closest("li")!;
  await userEvent.click(within(row).getByRole("button", { name: "安装" }));
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
}

it.each([
  ["success", "stay"], ["refused", "stay"], ["failure", "stay"],
  ["success", "leave"], ["success", "replacement"],
])("settles author install %s after a pending sign-out completes (%s)", async (outcome, connection) => {
  const port = new MockPort() as unknown as AgentPort;
  const logout = deferred<void>();
  const installation = deferred<MarketPlan>();
  const plan = await port.planOwnMarket({ slug: "demo/ship-notes" });
  vi.spyOn(port, "accountLogout").mockImplementationOnce(() => logout.promise);
  const account = vi.spyOn(port, "account").mockResolvedValue({ signedIn: false });
  const install = vi.spyOn(port, "installOwnMarket").mockImplementationOnce(() => installation.promise);
  const inventory = vi.spyOn(port, "plugins");
  const onChanged = vi.fn();
  const view = render(<AccountSettings port={port} onChanged={onChanged} />);
  await userEvent.click(screen.getByRole("button", { name: "退出登录" }));
  await userEvent.click(screen.getByRole("button", { name: "确认退出" }));
  expect(account).not.toHaveBeenCalled();
  await startOwnInstall();
  expect(install).toHaveBeenCalledTimes(1);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: /关闭/ }).disabled).toBe(true);
  await act(async () => logout.resolve());
  await screen.findByRole("button", { name: "去登录" });
  expect(account).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole("button", { name: "安装中…" })).toBeNull();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: /关闭/ }).disabled).toBe(false);
  if (connection !== "stay") {
    await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="settings.section"][data-value="account"]')!);
    if (connection === "replacement") view.rerender(<AccountSettings port={new MockPort() as unknown as AgentPort} onChanged={onChanged} />);
  }
  const reads = inventory.mock.calls.length;
  await act(async () => {
    if (outcome === "failure") installation.reject(new Error("late installation failure"));
    else installation.resolve({ ...plan, applied: outcome === "success", status: outcome === "success" ? "done" : "blocked" });
  });
  const changed = outcome === "success" && connection !== "replacement";
  expect(onChanged).toHaveBeenCalledTimes(changed ? 1 : 0);
  if (changed) await waitFor(() => expect(inventory).toHaveBeenCalledTimes(reads + 1));
  else expect(inventory).toHaveBeenCalledTimes(reads);
  expect(screen.queryByText("late installation failure")).toBeNull();
  expect(screen.queryByText("装好了，下一轮就能用")).toBeNull();
  if (connection === "stay") expect(screen.getByRole("button", { name: "去登录" })).toBeTruthy();
});
