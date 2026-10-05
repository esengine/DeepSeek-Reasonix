// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, PluginPlan, SessionStatus } from "../port/port";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const refusal = "cannot reload extensions while active work or background jobs are running";
const warning = "更改已保存，运行时未重载：{reason}。请用「重载运行时」重试。";
const row = () => document.querySelector('[data-extension-name="review-kit"]');

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

async function removePackage() {
  await screen.findByText("review-kit");
  const pane = within(row() as HTMLElement);
  await userEvent.click(pane.getByRole("button", { name: t("移除 {name}", { name: "review-kit" }) }));
  await userEvent.click(pane.getByRole("button", { name: t("删除") }));
}

function deferReload(port: AgentPort) {
  let finish!: () => void;
  let fail!: (error: Error) => void;
  vi.spyOn(port, "reloadExtensions").mockImplementationOnce(() => new Promise<void>((resolve, reject) => {
    finish = resolve;
    fail = reject;
  }));
  return { finish: () => finish(), settle: (outcome: string) => outcome === "success" ? finish() : fail(new Error("older reload failed")) };
}

it.each(["zh", "en"])("retains an applied removal's reload warning after its row disappears (%s)", async (lang) => {
  localStorage.setItem(STORAGE, lang);
  boot();
  const port = new MockPort() as unknown as AgentPort;
  const original = port.removePlugin.bind(port);
  vi.spyOn(port, "removePlugin").mockImplementation(async (name) => ({ ...await original(name), reloadError: refusal }));
  const onChanged = vi.fn();
  render(draw(port, onChanged));
  await removePackage();
  expect(row()).toBeNull();
  const note = screen.getByText(t(warning, { reason: refusal }));
  expect(note.getAttribute("role")).toBe("status");
  expect(note.getAttribute("data-s")).toBe("bad");
  expect(onChanged).toHaveBeenCalledTimes(1);
  await userEvent.click(screen.getByRole("button", { name: t("重载运行时") }));
  expect(await screen.findByText(t("已生效，下一轮开始用新的扩展"))).toBeTruthy();
  expect(screen.queryByText(t(warning, { reason: refusal }))).toBeNull();
  expect(onChanged).toHaveBeenCalledTimes(2);
});

it.each(["success", "failure"])("does not let an older manual reload %s overwrite the removal warning or retry", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const original = port.removePlugin.bind(port);
  vi.spyOn(port, "removePlugin").mockImplementation(async (name) => ({ ...await original(name), reloadError: refusal }));
  const old = deferReload(port);
  const onChanged = vi.fn();
  render(draw(port, onChanged));
  await userEvent.click(screen.getByRole("button", { name: t("重载运行时") }));
  await removePackage();
  expect(screen.getByText(t(warning, { reason: refusal }))).toBeTruthy();
  const current = deferReload(port);
  await userEvent.click(screen.getByRole("button", { name: t("重载运行时") }));
  await act(async () => old.settle(outcome));
  expect(screen.getByRole<HTMLButtonElement>("button", { name: t("重载中") }).disabled).toBe(true);
  expect(screen.queryByText("older reload failed")).toBeNull();
  expect(screen.queryByText(t("已生效，下一轮开始用新的扩展"))).toBeNull();
  expect(onChanged).toHaveBeenCalledTimes(1);
  await act(async () => current.finish());
  expect(screen.getByText(t("已生效，下一轮开始用新的扩展"))).toBeTruthy();
  expect(onChanged).toHaveBeenCalledTimes(2);
});

it("does not report a previous connection's applied removal in the current runtime", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  let finish!: (result: PluginPlan) => void;
  vi.spyOn(port, "removePlugin").mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  const onChanged = vi.fn();
  const view = render(draw(port, onChanged));
  await removePackage();
  view.rerender(draw(next, onChanged));
  await screen.findByText("review-kit");
  await act(async () => finish({ ok: true, status: "done", applied: true, reloadError: refusal }));
  expect(row()).not.toBeNull();
  expect(screen.queryByText(t(warning, { reason: refusal }))).toBeNull();
  expect(onChanged).not.toHaveBeenCalled();
});

it.each(["success", "denied"])("does not report a reload warning for a removal without one (%s)", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  if (outcome === "denied") vi.spyOn(port, "removePlugin").mockResolvedValue({ ok: false, status: "denied", applied: false, error: "removal denied" });
  render(draw(port));
  await removePackage();
  expect(screen.queryByText(t(warning, { reason: refusal }))).toBeNull();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: t("重载运行时") }).disabled).toBe(false);
  if (outcome === "denied") expect(screen.getByRole("alert").textContent).toBe("removal denied");
  else expect(row()).toBeNull();
});
