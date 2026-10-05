// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Packages } from "./Packages";
import { MockPort } from "../port/mock";
import type { AgentPort, PluginExport, PluginPackage } from "../port/port";

afterEach(cleanup);

const oldExport: PluginExport = { savedTo: "/tmp/previous-export.zip", required: ["OLD_EXPORT_TOKEN"] };
const newExport: PluginExport = { savedTo: "/tmp/current-export.zip", required: [] };

function draw(port: AgentPort, packages: PluginPackage[], onChanged: () => void) {
  return <Packages port={port} packages={packages} onChanged={onChanged} updating="" onUpdate={() => {}} />;
}

function row() {
  return within(document.querySelector<HTMLElement>('[data-extension-name="review-kit"]')!);
}

function expectNoOldResult() {
  expect(screen.queryByText(/previous-export.zip/)).toBeNull();
  expect(screen.queryByText(/OLD_EXPORT_TOKEN/)).toBeNull();
  expect(screen.queryByText("previous export failed")).toBeNull();
}

function deferredExport() {
  let finish!: (result: PluginExport) => void;
  let fail!: (error: Error) => void;
  const promise = new Promise<PluginExport>((resolve, reject) => { finish = resolve; fail = reject; });
  return { promise, finish, settle: (outcome: string) => outcome === "success" ? finish(oldExport) : fail(new Error("previous export failed")) };
}

it.each(["same", "replacement"])("keeps a remove confirmation only for a %s connection", async (connection) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const packages = await port.plugins();
  const remove = vi.spyOn(next, "removePlugin");
  const view = render(draw(port, packages, () => {}));
  await userEvent.click(row().getByRole("button", { name: "移除 review-kit" }));
  expect(row().getByRole("button", { name: "删除" })).toBeTruthy();
  view.rerender(draw(connection === "same" ? port : next, packages.map((p) => ({ ...p })), () => {}));
  if (connection === "same") expect(row().getByRole("button", { name: "删除" })).toBeTruthy();
  else {
    expect(row().queryByRole("button", { name: "删除" })).toBeNull();
    expect(row().queryByRole("button", { name: "取消" })).toBeNull();
    expect(remove).not.toHaveBeenCalled();
  }
});

it.each([
  ["success", "same"], ["failure", "same"],
  ["success", "replacement"], ["failure", "replacement"],
])("keeps an export %s result only for a %s connection", async (outcome, connection) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const packages = await port.plugins();
  const old = deferredExport();
  vi.spyOn(port, "exportPlugin").mockImplementationOnce(() => old.promise);
  const view = render(draw(port, packages, () => {}));
  await userEvent.click(row().getByRole("button", { name: "导出" }));
  await act(async () => old.settle(outcome));
  expect(screen.getByText(outcome === "success" ? /previous-export.zip/ : "previous export failed")).toBeTruthy();
  view.rerender(draw(connection === "same" ? port : next, packages.map((p) => ({ ...p })), () => {}));
  if (connection === "replacement") expectNoOldResult();
  else expect(screen.getByText(outcome === "success" ? /previous-export.zip/ : "previous export failed")).toBeTruthy();
});

it.each([
  ["success", false], ["failure", false],
  ["success", true], ["failure", true],
] as const)("isolates a pending export %s after port replacement (return=%s)", async (outcome, returnToPort) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const packages = await port.plugins();
  const old = deferredExport();
  const current = deferredExport();
  const originalExport = vi.spyOn(port, "exportPlugin").mockImplementationOnce(() => old.promise).mockImplementationOnce(() => current.promise);
  const nextExport = vi.spyOn(next, "exportPlugin").mockImplementationOnce(() => current.promise);
  const onChanged = vi.fn();
  const view = render(draw(port, packages, onChanged));
  await userEvent.click(row().getByRole("button", { name: "导出" }));
  view.rerender(draw(next, packages, onChanged));
  if (returnToPort) view.rerender(draw(port, packages, onChanged));
  expect(row().getByRole<HTMLButtonElement>("button", { name: "导出" }).disabled).toBe(false);
  await userEvent.click(row().getByRole("button", { name: "导出" }));
  const applying = row().getByRole<HTMLButtonElement>("button", { name: "打包中…" });
  await act(async () => old.settle(outcome));
  expectNoOldResult();
  expect(applying.disabled).toBe(true);
  expect(row().getByRole<HTMLButtonElement>("switch", { name: "关闭 review-kit" }).disabled).toBe(true);
  expect(onChanged).not.toHaveBeenCalled();
  await userEvent.click(applying);
  expect(originalExport).toHaveBeenCalledTimes(returnToPort ? 2 : 1);
  expect(nextExport).toHaveBeenCalledTimes(returnToPort ? 0 : 1);
  await act(async () => current.finish(newExport));
  expect(screen.getByText(/current-export.zip/)).toBeTruthy();
  expect(row().getByRole<HTMLButtonElement>("button", { name: "导出" }).disabled).toBe(false);
  expect(onChanged).toHaveBeenCalledTimes(1);
});
