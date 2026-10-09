// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import { SsePort } from "../port/sse";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, PluginPackage, SessionStatus } from "../port/port";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const refusal = "cannot reload extensions while active work or background jobs are running";
const warning = "更改已保存，运行时未重载：{reason}。请用「重载运行时」重试。";
const row = () => document.querySelector<HTMLElement>('[data-extension-name="notes-kit"]')!;

function draw(port: AgentPort, onChanged = vi.fn()) {
  return <Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={onChanged} onError={() => {}} at="ext:installed"
    account={null} accountUnread="" reloadAccount={() => {}}
  />;
}

function fixture(enabled: boolean, reloadError?: string, denied = false) {
  const pkg: PluginPackage = { name: "notes-kit", root: "/plugins/notes-kit", enabled };
  const calls = vi.fn(async (url: string, init?: RequestInit) => {
    if (url === "/rt/plugins") return Response.json([{ ...pkg }]);
    if (url === "/rt/extensions/reload") return Response.json({});
    if (url === "/rt/plugins/enabled") {
      if (denied) return Response.json({ code: "plugin.toggle_failed", error: "cannot save state", params: { detail: "state file unavailable" } }, { status: 422 });
      const body = JSON.parse(init!.body as string);
      pkg.enabled = body.enabled;
      return Response.json({ name: pkg.name, enabled: pkg.enabled, ...(reloadError ? { reloadError } : {}) });
    }
    throw new Error("unexpected fixture request: " + url);
  });
  vi.stubGlobal("fetch", calls);
  const transport = new SsePort("/rt");
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "plugins").mockImplementation(() => transport.plugins());
  vi.spyOn(port, "setPluginEnabled").mockImplementation((name, value) => transport.setPluginEnabled(name, value));
  vi.spyOn(port, "reloadExtensions").mockImplementation(() => transport.reloadExtensions());
  return { port, calls, allowReload: () => { reloadError = undefined; } };
}

it.each([true, false].flatMap((enabled) => ["zh", "en"].map((lang) => ({ enabled, lang }))))(
  "keeps the saved toggle and offers a reload retry (enabled=$enabled, $lang)", async ({ enabled, lang }) => {
    localStorage.setItem(STORAGE, lang);
    boot();
    const { port, calls } = fixture(enabled, refusal);
    const changed = vi.fn();
    render(draw(port, changed));
    await screen.findByText("notes-kit");
    await userEvent.click(within(row()).getByRole("switch"));
    const note = await screen.findByText(t(warning, { reason: refusal }));
    expect(note.getAttribute("role")).toBe("status");
    expect(note.getAttribute("data-s")).toBe("bad");
    expect(within(row()).getByRole("switch").getAttribute("aria-checked")).toBe(String(!enabled));
    expect(within(row()).queryByRole("alert")).toBeNull();
    expect(changed).toHaveBeenCalledTimes(1);
    expect(calls.mock.calls.filter(([url]) => url.endsWith("/plugins/enabled"))).toEqual([["/rt/plugins/enabled", {
      method: "POST", headers: { "content-type": "application/json" }, credentials: "same-origin",
      body: JSON.stringify({ name: "notes-kit", enabled: !enabled }),
    }]]);
    await userEvent.click(screen.getByRole("button", { name: t("重载运行时") }));
    expect(await screen.findByText(t("已生效，下一轮开始用新的扩展"))).toBeTruthy();
    expect(screen.queryByText(t(warning, { reason: refusal }))).toBeNull();
    expect(changed).toHaveBeenCalledTimes(2);
    expect(calls.mock.calls.filter(([url]) => url.endsWith("/plugins/enabled"))).toHaveLength(1);
    expect(calls.mock.calls.filter(([url]) => url.endsWith("/extensions/reload"))).toHaveLength(1);
  },
);

it.each([true, false])("keeps a successful toggle without a reload diagnostic (enabled=%s)", async (enabled) => {
  const { port } = fixture(enabled);
  const changed = vi.fn();
  render(draw(port, changed));
  await screen.findByText("notes-kit");
  await userEvent.click(within(row()).getByRole("switch"));
  expect(within(row()).getByRole("switch").getAttribute("aria-checked")).toBe(String(!enabled));
  expect(screen.queryByText(t(warning, { reason: refusal }))).toBeNull();
  expect(within(row()).queryByRole("alert")).toBeNull();
  expect(changed).toHaveBeenCalledTimes(1);
});

it("keeps a refused save as an action failure without saying it was saved", async () => {
  const { port } = fixture(true, undefined, true);
  render(draw(port));
  await screen.findByText("notes-kit");
  await userEvent.click(within(row()).getByRole("switch"));
  expect(within(row()).getByRole("alert").textContent).toContain("state file unavailable");
  expect(within(row()).getByRole("switch").getAttribute("aria-checked")).toBe("true");
  expect(screen.queryByText(t(warning, { reason: refusal }))).toBeNull();
});

it("keeps a pending toggle's reload report on its original connection", async () => {
  const { port, calls } = fixture(true, refusal);
  let finish!: (response: Response) => void;
  calls.mockImplementationOnce(async () => Response.json([{ name: "notes-kit", root: "/plugins/notes-kit", enabled: true }]));
  const next = new MockPort() as unknown as AgentPort;
  const changed = vi.fn();
  const view = render(draw(port, changed));
  await screen.findByText("notes-kit");
  calls.mockImplementationOnce(() => new Promise<Response>((resolve) => { finish = resolve; }));
  await userEvent.click(within(row()).getByRole("switch"));
  view.rerender(draw(next, changed));
  await screen.findByText("review-kit");
  await act(async () => finish(Response.json({ name: "notes-kit", enabled: false, reloadError: refusal })));
  expect(screen.queryByText(t(warning, { reason: refusal }))).toBeNull();
  expect(changed).not.toHaveBeenCalled();
});

it("clears the earlier warning when a later saved toggle reloads successfully", async () => {
  const { port, calls, allowReload } = fixture(true, refusal);
  const changed = vi.fn();
  render(draw(port, changed));
  await screen.findByText("notes-kit");
  await userEvent.click(within(row()).getByRole("switch"));
  expect(await screen.findByText(t(warning, { reason: refusal }))).toBeTruthy();
  allowReload();
  await userEvent.click(within(row()).getByRole("switch"));
  expect(screen.queryByText(t(warning, { reason: refusal }))).toBeNull();
  expect(screen.getByText(t("已生效，下一轮开始用新的扩展"))).toBeTruthy();
  expect(within(row()).getByRole("switch").getAttribute("aria-checked")).toBe("true");
  expect(changed).toHaveBeenCalledTimes(2);
  expect(calls.mock.calls.filter(([url]) => url.endsWith("/plugins/enabled"))).toHaveLength(2);
  expect(calls.mock.calls.filter(([url]) => url.endsWith("/extensions/reload"))).toHaveLength(0);
});

async function timedSettings() {
  const result = fixture(true);
  const changed = vi.fn();
  const view = render(draw(result.port, changed));
  await screen.findByText("notes-kit");
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  const click = (element: HTMLElement) => act(async () => { fireEvent.click(element); });
  const toggle = () => click(within(row()).getByRole("switch"));
  const advance = (ms: number) => act(async () => { await vi.advanceTimersByTimeAsync(ms); });
  return { ...result, changed, view, click, toggle, advance };
}

it.each(["package", "manual"])("gives a later package application its own confirmation window after %s success", async (first) => {
  const { click, toggle, advance, changed, calls } = await timedSettings();
  if (first === "manual") await click(screen.getByRole("button", { name: t("重载运行时") }));
  else await toggle();
  expect(screen.getByText(t("已生效，下一轮开始用新的扩展"))).toBeTruthy();
  await advance(3500);
  await toggle();
  expect(changed).toHaveBeenCalledTimes(2);
  await advance(500);
  expect(screen.getByText(t("已生效，下一轮开始用新的扩展"))).toBeTruthy();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: t("已生效") }).disabled).toBe(false);
  await advance(3499);
  expect(screen.getByText(t("已生效，下一轮开始用新的扩展"))).toBeTruthy();
  await advance(1);
  expect(screen.queryByText(t("已生效，下一轮开始用新的扩展"))).toBeNull();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: t("重载运行时") }).disabled).toBe(false);
  expect(calls.mock.calls.filter(([url]) => url.endsWith("/plugins/enabled"))).toHaveLength(first === "manual" ? 1 : 2);
  expect(calls.mock.calls.filter(([url]) => url.endsWith("/extensions/reload"))).toHaveLength(first === "manual" ? 1 : 0);
});

it("expires a single confirmation without extending it on a Settings rerender", async () => {
  const { port, changed, view, toggle, advance } = await timedSettings();
  await toggle();
  await advance(3500);
  view.rerender(draw(port, changed));
  await advance(499);
  expect(screen.getByText(t("已生效，下一轮开始用新的扩展"))).toBeTruthy();
  await advance(1);
  expect(screen.queryByText(t("已生效，下一轮开始用新的扩展"))).toBeNull();
  expect(changed).toHaveBeenCalledTimes(1);
});

it("retains a later saved-but-not-reloaded warning past the old success expiry", async () => {
  const { port, toggle, advance } = await timedSettings();
  await toggle();
  await advance(3500);
  vi.spyOn(port, "setPluginEnabled").mockResolvedValueOnce({ reloadError: refusal });
  await toggle();
  await advance(10000);
  expect(screen.getByText(t(warning, { reason: refusal }))).toBeTruthy();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: t("重载运行时") }).disabled).toBe(false);
});

it("keeps a later manual reload pending past the old success expiry", async () => {
  const { port, click, toggle, advance, changed } = await timedSettings();
  await toggle();
  await advance(3500);
  let finish!: () => void;
  vi.spyOn(port, "reloadExtensions").mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve; }));
  await click(screen.getByRole("button", { name: t("已生效") }));
  await advance(10000);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: t("重载中") }).disabled).toBe(true);
  await act(async () => finish());
  await advance(3999);
  expect(screen.getByText(t("已生效，下一轮开始用新的扩展"))).toBeTruthy();
  await advance(1);
  expect(screen.queryByText(t("已生效，下一轮开始用新的扩展"))).toBeNull();
  expect(changed).toHaveBeenCalledTimes(2);
});

it("does not let a previous connection's confirmation expire the new connection's success", async () => {
  const { changed, view, click, toggle, advance } = await timedSettings();
  await toggle();
  await advance(3500);
  const next = new MockPort() as unknown as AgentPort;
  vi.spyOn(next, "reloadExtensions").mockResolvedValue();
  await act(async () => view.rerender(draw(next, changed)));
  expect(screen.queryByText(t("已生效，下一轮开始用新的扩展"))).toBeNull();
  await click(screen.getByRole("button", { name: t("重载运行时") }));
  await advance(3999);
  expect(screen.getByText(t("已生效，下一轮开始用新的扩展"))).toBeTruthy();
  await advance(1);
  expect(screen.queryByText(t("已生效，下一轮开始用新的扩展"))).toBeNull();
  expect(changed).toHaveBeenCalledTimes(2);
});
