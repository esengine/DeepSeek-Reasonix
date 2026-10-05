// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Packages } from "./Packages";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, PluginExport, PluginPackage, PluginPlan, SessionStatus } from "../port/port";

afterEach(cleanup);

const diagnostic = "installed plugin manifest could not be read";
const failure = "package action unavailable";
const broken: PluginPackage = { name: "notes-kit", root: "/plugins/notes-kit", source: "/sources/notes-kit", enabled: true, error: diagnostic };

function row() {
  return document.querySelector<HTMLDetailsElement>('[data-extension-name="notes-kit"]')!;
}

function draw(port: AgentPort, onChanged = vi.fn(), p = broken) {
  return render(<Packages port={port} packages={[p]} onChanged={onChanged} updating="" onUpdate={() => {}} />);
}

it.each(["export", "toggle", "remove-reject", "remove-denied"])("shows the %s failure alongside the existing package diagnostic", async (operation) => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "plugins").mockResolvedValue([broken]);
  const exportPackage = vi.spyOn(port, "exportPlugin").mockRejectedValue(new Error(failure));
  const toggle = vi.spyOn(port, "setPluginEnabled").mockRejectedValue(new Error(failure));
  const remove = vi.spyOn(port, "removePlugin");
  if (operation === "remove-reject") remove.mockRejectedValue(new Error(failure));
  else remove.mockResolvedValue({ ok: false, status: "denied", applied: false, error: failure } as PluginPlan);
  const onChanged = vi.fn();
  render(<Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={onChanged} onError={() => {}} at="ext:installed"
    account={null} accountUnread="" reloadAccount={() => {}}
  />);
  await screen.findByText("notes-kit");
  const pane = within(row());
  if (operation === "export") await userEvent.click(pane.getByRole("button", { name: "导出" }));
  else if (operation === "toggle") await userEvent.click(pane.getByRole("switch", { name: "关闭 notes-kit" }));
  else {
    await userEvent.click(pane.getByRole("button", { name: "移除 notes-kit" }));
    await userEvent.click(pane.getByRole("button", { name: "删除" }));
  }
  expect(pane.getByRole("alert").textContent).toBe(failure);
  expect(pane.getByText(diagnostic)).toBeTruthy();
  expect(row().open).toBe(true);
  expect(row().getAttribute("aria-busy")).toBe("false");
  expect(pane.getByRole<HTMLButtonElement>("button", { name: "导出" }).disabled).toBe(false);
  expect(onChanged).toHaveBeenCalledTimes(1);
  if (operation === "export") expect(exportPackage).toHaveBeenCalledExactlyOnceWith("notes-kit");
  else if (operation === "toggle") expect(toggle).toHaveBeenCalledExactlyOnceWith("notes-kit", false);
  else expect(remove).toHaveBeenCalledExactlyOnceWith("notes-kit");
});

it("clears the old action failure on retry without losing the package diagnostic", async () => {
  const port = new MockPort() as unknown as AgentPort;
  let finish!: (value: PluginExport) => void;
  const pending = new Promise<PluginExport>((resolve) => { finish = resolve; });
  const exportPackage = vi.spyOn(port, "exportPlugin").mockRejectedValueOnce(new Error(failure)).mockImplementationOnce(() => pending);
  const onChanged = vi.fn();
  draw(port, onChanged);
  const pane = within(row());
  await userEvent.click(pane.getByRole("button", { name: "导出" }));
  expect(pane.getByRole("alert").textContent).toBe(failure);
  await userEvent.click(pane.getByRole("button", { name: "导出" }));
  expect(pane.queryByRole("alert")).toBeNull();
  expect(pane.getByText(diagnostic)).toBeTruthy();
  expect(pane.getByRole<HTMLButtonElement>("button", { name: "打包中…" }).disabled).toBe(true);
  await act(async () => finish({ required: [], savedTo: "/exports/notes-kit.zip" }));
  expect(pane.queryByText(failure)).toBeNull();
  expect(pane.getByText(diagnostic)).toBeTruthy();
  expect(pane.getByText(/notes-kit.zip/)).toBeTruthy();
  expect(exportPackage).toHaveBeenCalledTimes(2);
  expect(onChanged).toHaveBeenCalledTimes(2);
});

it("opens a healthy package row to expose an export failure", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "exportPlugin").mockRejectedValue(new Error(failure));
  draw(port, vi.fn(), { ...broken, error: undefined });
  expect(row().open).toBe(false);
  await userEvent.click(within(row()).getByRole("button", { name: "导出" }));
  expect(row().open).toBe(true);
  expect(within(row()).getByRole("alert").textContent).toBe(failure);
});

it("leaves the package diagnostic available before any operation", () => {
  draw(new MockPort() as unknown as AgentPort);
  expect(within(row()).getByText(diagnostic)).toBeTruthy();
  expect(within(row()).queryByRole("alert")).toBeNull();
  expect(row().open).toBe(false);
  expect(within(row()).getByRole<HTMLButtonElement>("button", { name: "导出" }).disabled).toBe(false);
});
