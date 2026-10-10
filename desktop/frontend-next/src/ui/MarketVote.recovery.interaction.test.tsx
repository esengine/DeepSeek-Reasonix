// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { boot, STORAGE, t } from "../i18n";
import { reason } from "../i18n/kernel";
import { Market } from "./Market";
import { MockPort } from "../port/mock";
import { HttpError, type AgentPort, type MarketVote as Vote } from "../port/port";
import { SsePort } from "../port/sse";

afterEach(() => { cleanup(); vi.unstubAllGlobals(); localStorage.setItem(STORAGE, "zh"); boot(); });

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

const known: Vote = { signedIn: true, value: 1, upCount: 8, downCount: 2, approvalRate: 0.8, canVote: true };
const up = () => screen.getByRole<HTMLButtonElement>("button", { name: new RegExp(t("赞")) });

async function fixture(failure: Error = new Error("registry unavailable")) {
  const port = new MockPort() as unknown as AgentPort;
  const page = await port.marketList({ pinned: true });
  const pkg = { ...page.packages[0]!, upCount: 3, downCount: 1, approvalRate: 0.75 };
  const detail = await port.marketDetail(pkg.slug);
  vi.spyOn(port, "marketList").mockResolvedValue({ ...page, packages: [pkg] });
  vi.spyOn(port, "marketDetail").mockResolvedValue({ ...detail, package: pkg });
  const read = vi.spyOn(port, "marketMyVote").mockRejectedValueOnce(failure);
  const cast = vi.spyOn(port, "voteMarket").mockResolvedValue({ ...known, value: 0 });
  const onSignIn = vi.fn();
  const view = render(<Market port={port} onInstalled={() => {}} onSignIn={onSignIn} />);
  await userEvent.click(await screen.findByRole("button", { name: new RegExp(pkg.name) }));
  await screen.findByText(reason(failure));
  return { port, pkg, detail, read, cast, onSignIn, view };
}

it.each(["zh", "en"])("keeps a failed vote lookup unknown and retryable in %s", async (lang) => {
  localStorage.setItem(STORAGE, lang); boot();
  const f = await fixture();
  expect(screen.queryByRole("button", { name: t("登录后评价") })).toBeNull();
  expect(screen.getByRole("alert").textContent).toBe("registry unavailable");
  expect(screen.getByText(t("好评 {pct}%（{n} 票）", { pct: 75, n: 4 }))).toBeTruthy();
  expect(up().disabled).toBe(true);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: t("重试") }).disabled).toBe(false);
  expect(f.cast).not.toHaveBeenCalled();
  expect(f.onSignIn).not.toHaveBeenCalled();
});

it("retries by keyboard, waits for the fresh vote, then withdraws its actual value", async () => {
  const f = await fixture();
  const fresh = deferred<Vote>();
  f.read.mockImplementationOnce(() => fresh.promise);
  const retry = screen.getByRole<HTMLButtonElement>("button", { name: "重试" });
  retry.focus();
  await userEvent.keyboard("{Enter}");
  const group = screen.getByRole("group", { name: "评价" });
  expect(document.activeElement).toBe(group);
  expect(screen.queryByRole("alert")).toBeNull();
  const pending = screen.getByRole<HTMLButtonElement>("button", { name: "正在读取…" });
  expect(pending.disabled).toBe(true);
  expect(up().disabled).toBe(true);
  expect(f.read).toHaveBeenCalledTimes(2);
  expect(f.cast).not.toHaveBeenCalled();
  await act(async () => fresh.resolve(known));
  expect(screen.queryByRole("button", { name: "重试" })).toBeNull();
  expect(up().getAttribute("aria-pressed")).toBe("true");
  expect(screen.getByText("好评 80%（10 票）")).toBeTruthy();
  expect(document.activeElement).toBe(group);
  await userEvent.tab();
  expect(document.activeElement).toBe(up());
  await userEvent.keyboard("{Enter}");
  expect(f.cast).toHaveBeenLastCalledWith(f.pkg.slug, 0);
});

it("ignores clicking the disabled vote retry while its fresh read is pending", async () => {
  const f = await fixture();
  const fresh = deferred<Vote>();
  f.read.mockImplementationOnce(() => fresh.promise);
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  const pending = screen.getByRole<HTMLButtonElement>("button", { name: "正在读取…" });
  expect(pending.disabled).toBe(true);
  await userEvent.click(pending);
  expect(f.read).toHaveBeenCalledTimes(2);
  expect(f.cast).not.toHaveBeenCalled();
  await act(async () => fresh.resolve(known));
  expect(up().disabled).toBe(false);
  expect(screen.queryByRole("button", { name: "重试" })).toBeNull();
});

it("offers sign-in only when a successful retry answers signed out", async () => {
  const f = await fixture();
  f.read.mockResolvedValueOnce({ signedIn: false, value: 0 });
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  await userEvent.click(await screen.findByRole("button", { name: "登录后评价" }));
  expect(f.onSignIn).toHaveBeenCalledTimes(1);
  expect(up().disabled).toBe(true);
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.queryByRole("button", { name: "重试" })).toBeNull();
  expect(f.cast).not.toHaveBeenCalled();
});

it("preserves sign-in recovery for the host's typed signed-out refusal", async () => {
  const f = await fixture(new HttpError(401, "expired session", { code: "market.signed_out" }));
  await userEvent.click(screen.getByRole("button", { name: "登录后评价" }));
  expect(f.onSignIn).toHaveBeenCalledTimes(1);
  expect(up().disabled).toBe(true);
  expect(screen.queryByRole("button", { name: "重试" })).toBeNull();
  expect(f.cast).not.toHaveBeenCalled();
});

it("does not infer signed-out state from an HTTP status without its domain identity", async () => {
  const f = await fixture(new HttpError(401, "unclassified upstream refusal"));
  expect(screen.queryByRole("button", { name: "登录后评价" })).toBeNull();
  expect(screen.getByRole("button", { name: "重试" })).toBeTruthy();
  expect(up().disabled).toBe(true);
  expect(f.cast).not.toHaveBeenCalled();
});

it("keeps the registry's own-package restriction after recovery", async () => {
  const f = await fixture();
  f.read.mockResolvedValueOnce({ ...known, value: 0, canVote: false, own: true });
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  await screen.findByText("不能评价自己发布的包");
  expect(up().disabled).toBe(true);
  expect(screen.queryByRole("button", { name: "登录后评价" })).toBeNull();
  expect(f.cast).not.toHaveBeenCalled();
});

it("leaves repeated lookup failures retryable without inventing account state", async () => {
  const f = await fixture();
  f.read.mockRejectedValueOnce(new HttpError(503, "registry still unavailable"));
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("registry still unavailable"));
  expect(screen.queryByRole("button", { name: "登录后评价" })).toBeNull();
  f.read.mockResolvedValueOnce(known);
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() => expect(up().disabled).toBe(false));
  expect(f.read).toHaveBeenCalledTimes(3);
  expect(f.read).toHaveBeenLastCalledWith(f.pkg.slug);
  expect(screen.queryByRole("alert")).toBeNull();
});

it("keeps a known vote and allows another cast after its write fails", async () => {
  const f = await fixture();
  f.read.mockResolvedValueOnce(known);
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() => expect(up().disabled).toBe(false));
  f.cast.mockRejectedValueOnce(new Error("vote write unavailable"));
  await userEvent.click(up());
  await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("vote write unavailable"));
  expect(up().getAttribute("aria-pressed")).toBe("true");
  expect(up().disabled).toBe(false);
  expect(screen.queryByRole("button", { name: "重试" })).toBeNull();
  await userEvent.click(up());
  expect(f.cast).toHaveBeenLastCalledWith(f.pkg.slug, 0);
  expect(f.read).toHaveBeenCalledTimes(2);
});

it.each(["zh", "en"])("offers sign-in after a typed signed-out vote write in %s", async (lang) => {
  localStorage.setItem(STORAGE, lang); boot();
  const f = await fixture();
  f.read.mockResolvedValueOnce(known);
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  await waitFor(() => expect(up().disabled).toBe(false));
  const write = deferred<Vote>();
  f.cast.mockImplementationOnce(() => write.promise);
  await userEvent.click(up());
  expect(up().disabled).toBe(true);
  expect(screen.queryByRole("button", { name: t("登录后评价") })).toBeNull();
  const failure = new HttpError(401, "expired session", { code: "market.signed_out" });
  await act(async () => write.reject(failure));
  expect(screen.getByRole("alert").textContent).toBe(reason(failure));
  expect(up().disabled).toBe(true);
  expect(up().getAttribute("aria-pressed")).toBe("false");
  expect(screen.getByRole<HTMLButtonElement>("button", { name: new RegExp(t("踩")) }).disabled).toBe(true);
  expect(screen.getByText(t("好评 {pct}%（{n} 票）", { pct: 80, n: 10 }))).toBeTruthy();
  expect(screen.queryByRole("button", { name: t("重试") })).toBeNull();
  await userEvent.click(up());
  expect(f.cast).toHaveBeenCalledTimes(1);
  const signIn = screen.getByRole("button", { name: t("登录后评价") });
  signIn.focus();
  await userEvent.keyboard("{Enter}");
  expect(f.onSignIn).toHaveBeenCalledTimes(1);
  expect(f.read).toHaveBeenCalledTimes(2);
});

it.each([
  new HttpError(401, "unclassified upstream refusal"),
  new HttpError(429, "slow down", { code: "market.rate_limited" }),
  new Error("401 unauthorized: signed out"),
])("retains the known vote after an unrelated write failure: %s", async (failure) => {
  const f = await fixture();
  f.read.mockResolvedValueOnce(known);
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() => expect(up().disabled).toBe(false));
  f.cast.mockRejectedValueOnce(failure);
  await userEvent.click(up());
  expect(screen.getByRole("alert").textContent).toBe(reason(failure));
  expect(up().disabled).toBe(false);
  expect(up().getAttribute("aria-pressed")).toBe("true");
  expect(screen.queryByRole("button", { name: "登录后评价" })).toBeNull();
  expect(screen.getByText("好评 80%（10 票）")).toBeTruthy();
  await userEvent.click(up());
  expect(f.cast).toHaveBeenLastCalledWith(f.pkg.slug, 0);
  expect(f.cast).toHaveBeenCalledTimes(2);
  expect(f.onSignIn).not.toHaveBeenCalled();
});

it("recovers from the signed-out refusal carried by the real HTTP port", async () => {
  const f = await fixture();
  f.read.mockResolvedValueOnce(known);
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() => expect(up().disabled).toBe(false));
  const fetcher = vi.fn().mockResolvedValue(Response.json({ code: "market.signed_out", error: "not signed in" }, { status: 401 }));
  vi.stubGlobal("fetch", fetcher);
  const http = new SsePort("http://fixture.test");
  f.cast.mockImplementation((slug, value) => http.voteMarket(slug, value));
  await userEvent.click(up());
  expect(await screen.findByRole("button", { name: "登录后评价" })).toBeTruthy();
  expect(up().disabled).toBe(true);
  expect(up().getAttribute("aria-pressed")).toBe("false");
  expect(screen.getByText("好评 80%（10 票）")).toBeTruthy();
  expect(fetcher).toHaveBeenCalledOnce();
  expect(fetcher).toHaveBeenCalledWith(`http://fixture.test/market/packages/${f.pkg.slug}/vote`, {
    method: "POST", headers: { "content-type": "application/json" }, credentials: "same-origin", body: JSON.stringify({ value: 0 }),
  });
});

it.each(["success", "failure"])("ignores the old retry's %s after the market connection changes", async (outcome) => {
  const f = await fixture();
  const old = deferred<Vote>();
  f.read.mockImplementationOnce(() => old.promise);
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  const next = new MockPort() as unknown as AgentPort;
  vi.spyOn(next, "marketDetail").mockResolvedValue({ ...f.detail, package: f.pkg });
  vi.spyOn(next, "marketMyVote").mockResolvedValue({ ...known, value: -1 });
  f.view.rerender(<Market port={next} onInstalled={() => {}} onSignIn={f.onSignIn} />);
  await waitFor(() => expect(screen.getByRole("button", { name: /踩/ }).getAttribute("aria-pressed")).toBe("true"));
  await act(async () => {
    if (outcome === "success") old.resolve(known);
    else old.reject(new Error("old retry unavailable"));
  });
  expect(up().getAttribute("aria-pressed")).toBe("false");
  expect(up().disabled).toBe(false);
  expect(screen.queryByText("old retry unavailable")).toBeNull();
  expect(screen.queryByRole("button", { name: "重试" })).toBeNull();
});
