// @vitest-environment jsdom
import { StrictMode, useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import { SsePort } from "../port/sse";
import type { AccountState, AgentPort, SessionStatus, MarketVote } from "../port/port";

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => { resolve = yes; });
  return { promise, resolve };
}

const signedIn: AccountState = { signedIn: true, user: { handle: "demo", email: "demo@example.test", label: "demo" } };
const signedOut: AccountState = { signedIn: false };
const known: MarketVote = { signedIn: true, value: 1, upCount: 8, downCount: 2, approvalRate: 0.8, canVote: true };
const out: MarketVote = { signedIn: false, value: 0 };
const up = () => screen.getByRole<HTMLButtonElement>("button", { name: /赞/ });

function AccountSettings({ port, hub, initial }: { port: AgentPort; hub: MockHub; initial: AccountState }) {
  const [account, setAccount] = useState(initial);
  return <Settings
    hub={hub as never} port={port} status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={() => {}} onError={() => {}} at="account"
    account={account} accountUnread="" reloadAccount={() => { void port.account().then(setAccount); }}
  />;
}

async function fixture(strict: boolean, initial: AccountState, target: AccountState) {
  const port = new MockPort() as unknown as AgentPort;
  const pkg = (await port.marketList({ pinned: true })).packages[0]!;
  let account = initial;
  const transition = deferred<void>();
  const votePath = `/kernel/market/packages/${pkg.slug}/vote`;
  const readReplies: Promise<Response>[] = [];
  const writeReplies: Promise<Response>[] = [];
  const fetcher = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    if (url === "/kernel/account/login") return Response.json({
      deviceCode: "test-device", userCode: "TEST-CODE", verificationUri: "https://fixture.test/device",
      interval: 1, expiresIn: 60,
    });
    if (url === "/kernel/account/poll" || url === "/kernel/account/logout") {
      await transition.promise;
      account = target;
      return url.endsWith("/poll") ? Response.json({ status: "complete" }) : new Response(null, { status: 204 });
    }
    if (url === "/kernel/account") return Response.json(account);
    if (url === votePath && init?.method === "POST") {
      const pending = writeReplies.shift();
      const value = JSON.parse(init.body as string).value;
      return pending ?? Response.json({ ...known, value });
    }
    if (url === votePath) return readReplies.shift() ?? Response.json(account.signedIn ? known : out);
    throw new Error(`Unexpected request: ${url}`);
  });
  vi.stubGlobal("fetch", fetcher);
  const http = new SsePort("/kernel");
  for (const method of ["account", "accountLogin", "accountLogout", "accountPoll", "marketMyVote", "voteMarket"] as const) {
    vi.spyOn(port, method).mockImplementation(http[method].bind(http) as never);
  }
  const browser = vi.spyOn(port, "openExternal").mockResolvedValue(undefined);
  const list = vi.spyOn(port, "marketList");
  const detail = vi.spyOn(port, "marketDetail");
  const install = vi.spyOn(port, "installMarket");
  const form = <AccountSettings port={port} hub={new MockHub()} initial={initial} />;
  render(strict ? <StrictMode>{form}</StrictMode> : form);
  const user = userEvent.setup();
  const requests = (path: string, method?: string) => fetcher.mock.calls.filter(([url, init]) => url === path && (!method || init?.method === method));
  if (!initial.signedIn) {
    await user.click(screen.getByRole("button", { name: "登录" }));
    await waitFor(() => expect(requests("/kernel/account/poll")).toHaveLength(1), { timeout: 2500 });
    expect(JSON.parse(requests("/kernel/account/poll")[0][1]!.body as string)).toEqual({ deviceCode: "test-device" });
    expect(browser).toHaveBeenCalledWith("https://fixture.test/device");
  } else {
    await user.click(screen.getByRole("button", { name: "退出登录" }));
    await user.click(screen.getByRole("button", { name: "确认退出" }));
    expect(requests("/kernel/account/logout")).toHaveLength(1);
  }
  expect(requests("/kernel/account")).toHaveLength(0);
  const openMarket = async () => {
    await user.click(document.querySelector<HTMLButtonElement>('[data-action="settings.section"][data-value="ext"]')!);
    await user.click(screen.getByRole("tab", { name: "发现" }));
    await user.click(await screen.findByRole("button", { name: new RegExp(pkg.name) }));
    await waitFor(() => expect(requests(votePath).length).toBeGreaterThan(0));
  };
  const settle = async () => {
    await act(async () => transition.resolve());
    await waitFor(() => expect(requests("/kernel/account").length).toBeGreaterThan(0));
    expect(document.querySelector('[data-action="settings.section"][data-value="account"]')?.textContent)
      .toContain(target.signedIn ? target.user?.label ?? "已登录" : "未登录");
  };
  return { user, port, pkg, requests, votePath, readReplies, writeReplies, openMarket, settle, list, detail, install };
}

for (const strict of [false, true]) describe(`account vote ownership with StrictMode=${strict}`, () => {
  it.each([
    { change: "login", initial: signedOut, target: signedIn },
    { change: "login without identity service", initial: signedOut, target: { signedIn: true, error: "Identity unavailable" } },
    { change: "logout", initial: signedIn, target: signedOut },
  ])("refreshes the same package detail after pending $change completes", async ({ initial, target }) => {
    const f = await fixture(strict, initial, target);
    await f.openMarket();
    if (initial.signedIn) await waitFor(() => expect(up().disabled).toBe(false));
    else await screen.findByRole("button", { name: "登录后评价" });
    const panel = document.querySelector(".mkt-entry");
    const listReads = f.list.mock.calls.length;
    const detailReads = f.detail.mock.calls.length;
    const voteReads = f.requests(f.votePath).length;
    await f.settle();
    expect(document.querySelector(".mkt-entry")).toBe(panel);
    await waitFor(() => expect(f.requests(f.votePath).length).toBeGreaterThan(voteReads));
    if (target.signedIn) {
      await waitFor(() => expect(up().disabled).toBe(false));
      expect(up().getAttribute("aria-pressed")).toBe("true");
      expect(screen.queryByRole("button", { name: "登录后评价" })).toBeNull();
      await f.user.click(up());
      expect(f.requests(f.votePath, "POST")[0][1]).toMatchObject({
        method: "POST", credentials: "same-origin", body: JSON.stringify({ value: 0 }),
      });
      await waitFor(() => expect(up().getAttribute("aria-pressed")).toBe("false"));
    } else {
      await screen.findByRole("button", { name: "登录后评价" });
      expect(up().disabled).toBe(true);
      expect(up().getAttribute("aria-pressed")).toBe("false");
      await f.user.click(up());
      expect(f.requests(f.votePath, "POST")).toHaveLength(0);
    }
    expect(f.list).toHaveBeenCalledTimes(listReads);
    expect(f.detail).toHaveBeenCalledTimes(detailReads);
    expect(f.install).not.toHaveBeenCalled();
  });

  it.each([
    ["read", "success"], ["read", "failure"], ["write", "success"], ["write", "failure"],
  ])("discards old-account %s %s after logout", async (operation, outcome) => {
    const f = await fixture(strict, signedIn, signedOut);
    const old = deferred<Response>();
    if (operation === "read") f.readReplies.push(old.promise);
    await f.openMarket();
    if (operation === "write") {
      await waitFor(() => expect(up().disabled).toBe(false));
      f.writeReplies.push(old.promise);
      await f.user.click(up());
      expect(f.requests(f.votePath, "POST")).toHaveLength(1);
    }
    expect(up().disabled).toBe(true);
    const panel = document.querySelector(".mkt-entry");
    await f.settle();
    await act(async () => old.resolve(outcome === "success"
      ? Response.json({ ...known, upCount: 91, downCount: 9, approvalRate: 0.91 })
      : Response.json({ code: "market.rate_limited", error: "old-account refusal" }, { status: 429 })));
    await screen.findByRole("button", { name: "登录后评价" });
    expect(up().disabled).toBe(true);
    expect(up().getAttribute("aria-pressed")).toBe("false");
    expect(document.querySelector(".mkt-entry")).toBe(panel);
    expect(document.querySelector(".mkt-vote-rate")?.textContent).not.toContain("100");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByText("old-account refusal")).toBeNull();
    expect(document.querySelector('[data-action="market.vote-retry"]')).toBeNull();
    expect(f.install).not.toHaveBeenCalled();
  });
});
