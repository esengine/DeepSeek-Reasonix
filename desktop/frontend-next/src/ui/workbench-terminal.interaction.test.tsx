// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";

vi.mock("./TerminalLayer", () => ({
  default: ({ terminals, active, shown }: { terminals: { id: string }[]; active: string; shown: boolean }) => (
    <>{terminals.map((t) => <div key={t.id} data-testid="screen" data-shown={shown && t.id === active ? "yes" : "no"} />)}</>
  ),
}));

import { WorkbenchPanel } from "./WorkbenchPanel";
import { MockPort } from "../port/mock";

const props = { scheme: "light" as const, shown: true, changes: [], onSurfaces: vi.fn(), onCloseManual: vi.fn(), onExternal: vi.fn() };
const tabNames = () => [...document.querySelectorAll(".workbench-tab-pick")].map((t) => t.textContent);

beforeEach(() => localStorage.clear());
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

async function openTerminalFromLauncher(port: MockPort) {
  const view = render(<WorkbenchPanel {...props} port={port} tabs={[]} manual />);
  fireEvent.click(document.querySelector('[data-action="workbench.new"]')!);
  fireEvent.click(document.querySelector('[data-action="workbench.launch-terminal"]')!);
  await waitFor(() => expect(tabNames()).toContain("终端"));
  return view;
}

it("offers a terminal in a new tab and opens it as a tab in front", async () => {
  const port = new MockPort();
  const open = vi.spyOn(port, "openTerminal");
  await openTerminalFromLauncher(port);
  expect(open).toHaveBeenCalledOnce();
  expect(document.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toBe("终端");
  expect((await screen.findByTestId("screen")).getAttribute("data-shown")).toBe("yes");
});

it("keeps a terminal's screen mounted but not in front when another tab is chosen", async () => {
  const port = new MockPort();
  await openTerminalFromLauncher(port);
  fireEvent.click(document.querySelector('[data-action="workbench.new"]')!);
  fireEvent.click(document.querySelector('[data-action="workbench.launch-find"]')!);
  expect((await screen.findByTestId("screen")).getAttribute("data-shown")).toBe("no");
});

it("numbers terminals once there are two, and names a busy one for the command it runs with a dot", async () => {
  const port = new MockPort();
  await openTerminalFromLauncher(port);
  await port.openTerminal("bash", 80, 24);
  await waitFor(() => expect(tabNames()).toEqual(["终端 1", "终端 2"]));
  expect(document.querySelector(".workbench-tab-busy")).toBeNull();
});

it("brings back the terminals the kernel already holds, as tabs, without opening another", async () => {
  const port = new MockPort();
  await port.openTerminal("bash", 80, 24);
  const open = vi.spyOn(port, "openTerminal");
  render(<WorkbenchPanel {...props} port={port} tabs={[]} manual />);
  await waitFor(() => expect(tabNames()).toEqual(["终端"]));
  expect(open).not.toHaveBeenCalled();
});

it("ends the shell when its tab is closed", async () => {
  const port = new MockPort();
  const close = vi.spyOn(port, "closeTerminal");
  await openTerminalFromLauncher(port);
  await userEvent.click(document.querySelector<HTMLElement>('[data-action="workbench.close"]')!);
  await waitFor(() => expect(close).toHaveBeenCalledOnce());
  await waitFor(() => expect(tabNames()).not.toContain("终端"));
});

it("asks before closing a terminal with a command running, and ends it only when told to", async () => {
  const port = new MockPort();
  const close = vi.spyOn(port, "closeTerminal");
  await openTerminalFromLauncher(port);
  const [info] = await port.terminals();
  vi.spyOn(port, "terminals").mockResolvedValue([{ ...info, busy: true }]);
  await waitFor(() => expect(document.querySelector(".workbench-tab-busy")).not.toBeNull(), { timeout: 5000 });
  await userEvent.click(document.querySelector<HTMLElement>('[data-action="workbench.close"]')!);
  expect(await screen.findByRole("alertdialog", { name: /命令在运行/ })).toBeTruthy();
  expect(close).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "取消" }));
  expect(close).not.toHaveBeenCalled();
  await userEvent.click(document.querySelector<HTMLElement>('[data-action="workbench.close"]')!);
  await userEvent.click(await screen.findByRole("button", { name: "结束并关闭" }));
  await waitFor(() => expect(close).toHaveBeenCalledOnce());
});

it("opens the latest terminal on the shortcut's request, or a first one when there is none", async () => {
  const port = new MockPort();
  const open = vi.spyOn(port, "openTerminal");
  const view = render(<WorkbenchPanel {...props} port={port} tabs={[]} manual terminalRequest={0} />);
  view.rerender(<WorkbenchPanel {...props} port={port} tabs={[]} manual terminalRequest={1} />);
  await waitFor(() => expect(open).toHaveBeenCalledOnce());
  await waitFor(() => expect(tabNames()).toEqual(["终端"]));
  view.rerender(<WorkbenchPanel {...props} port={port} tabs={[]} manual terminalRequest={2} />);
  await act(async () => {});
  expect(open).toHaveBeenCalledOnce();
});

it("remembers the shell last chosen for the next terminal", async () => {
  const port = new MockPort();
  const open = vi.spyOn(port, "openTerminal");
  localStorage.setItem("rx-terminal-shell", "pwsh");
  await openTerminalFromLauncher(port);
  expect(open).toHaveBeenCalledWith("pwsh", 0, 0);
});
