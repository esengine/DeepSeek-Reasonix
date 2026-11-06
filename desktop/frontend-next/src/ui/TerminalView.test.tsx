// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";

// The emulator draws to a canvas jsdom does not have; what is under test is how
// the view feeds it and what it does with what the person does to it.
const fake = vi.hoisted(() => {
  const screens: FakeTerm[] = [];
  class FakeTerm {
    cols = 100;
    rows = 30;
    options: Record<string, unknown> = {};
    written: Uint8Array[] = [];
    resets = 0;
    selection = "";
    data: ((d: string) => void) | null = null;
    keys: ((e: KeyboardEvent) => boolean) | null = null;
    buffer = { active: { length: 2, getLine: (i: number) => ({ translateToString: () => ["first line", "second line"][i] }) } };
    constructor() { screens.push(this); }
    loadAddon() {}
    open() {}
    focus() {}
    dispose() {}
    clear() {}
    reset() { this.resets += 1; this.written = []; }
    write(b: Uint8Array) { this.written.push(b); }
    onData(f: (d: string) => void) { this.data = f; }
    onResize() {}
    onSelectionChange() {}
    attachCustomKeyEventHandler(f: (e: KeyboardEvent) => boolean) { this.keys = f; }
    hasSelection() { return this.selection !== ""; }
    getSelection() { return this.selection; }
    clearSelection() { this.selection = ""; }
    text() { return this.written.map((b) => new TextDecoder().decode(b)).join(""); }
  }
  const link: { open: ((e: MouseEvent, url: string) => void) | null } = { open: null };
  return { screens, FakeTerm, link };
});
const screens = fake.screens;
vi.mock("@xterm/xterm", () => ({ Terminal: fake.FakeTerm }));
vi.mock("@xterm/xterm/css/xterm.css", () => ({}));
vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit() {} } }));
vi.mock("@xterm/addon-web-links", () => ({ WebLinksAddon: class { constructor(open: (e: MouseEvent, url: string) => void) { fake.link.open = open; } } }));
globalThis.ResizeObserver ??= class { observe() {} disconnect() {} unobserve() {} } as unknown as typeof ResizeObserver;

import { MockPort } from "../port/mock";
import type { TerminalInfo } from "../port/terminal";
import { TerminalView, type TerminalViewProps } from "./TerminalView";
import { TerminalHostContext } from "./terminal_host";

beforeEach(() => { localStorage.clear(); screens.length = 0; fake.link.open = null; });
afterEach(cleanup);

async function setup(over: Partial<TerminalViewProps> = {}) {
  const port = new MockPort();
  const info: TerminalInfo = await port.openTerminal("bash", 80, 24);
  const props: TerminalViewProps = {
    port, info, shown: true, scheme: "light", shells: [], onLoadShells: () => {}, onShell: () => {}, onQuote: () => {},
    onOpenPage: () => {}, onCommand: () => {}, onRestart: async () => { await port.restartTerminal(info.id); }, onClose: () => {}, ...over,
  };
  const view = render(<TerminalView {...props} />);
  await waitFor(() => expect(screens[0]?.text()).toContain("reasonix"));
  return { port, info, props, view, term: () => screens[0] };
}

it("draws what the shell already wrote when the tab comes to the front", async () => {
  const { term } = await setup();
  expect(term().resets).toBe(1);
  expect(term().text()).toContain("(main)");
});

it("sends each keystroke to the shell and names the line when it is submitted", async () => {
  const typed: string[] = [];
  const commands: string[] = [];
  const { port, info, term } = await setup({ onCommand: (line) => commands.push(line) });
  const input = vi.spyOn(port, "terminalInput");
  act(() => { for (const ch of "ls") term().data!(ch); term().data!("\r"); });
  expect(input).toHaveBeenCalledWith(info.id, "l");
  expect(commands).toEqual(["ls"]);
  expect(typed).toEqual([]);
  await waitFor(() => expect(term().text()).toContain("ls: command not found"));
});

it("does not read the shell while another tab is in front, and reads it again from the start on return", async () => {
  const { props, view, term } = await setup();
  const reading = () => (props.port as unknown as { fakes: Map<string, { readers: Set<unknown> }> }).fakes.get(props.info.id)?.readers.size;
  expect(reading()).toBe(1);
  view.rerender(<TerminalView {...props} shown={false} />);
  expect(reading()).toBe(0);
  view.rerender(<TerminalView {...props} shown />);
  await waitFor(() => expect(term().resets).toBe(2));
  expect(term().text()).toContain("(main)");
});

it("says the process exited with its code and offers to start it again or close the tab", async () => {
  const closed = vi.fn();
  const { port, info, term } = await setup({ onClose: closed });
  act(() => { for (const ch of "exit") term().data!(ch); term().data!("\r"); });
  expect(await screen.findByText("进程已退出（代码 130）")).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "重新启动" }));
  await waitFor(() => expect(screen.queryByText(/进程已退出/)).toBeNull());
  expect((await port.terminals()).find((x) => x.id === info.id)?.running).toBe(true);
  act(() => { for (const ch of "exit") term().data!(ch); term().data!("\r"); });
  await userEvent.click(await screen.findByRole("button", { name: "关闭页签" }));
  expect(closed).toHaveBeenCalled();
});

it("says what the terminal is on first opening and not again once understood", async () => {
  const { view, props } = await setup();
  expect(screen.getByRole("note").textContent).toContain("不经过模型的权限和沙盒");
  await userEvent.click(screen.getByRole("button", { name: "知道了" }));
  expect(screen.queryByRole("note")).toBeNull();
  view.unmount();
  render(<TerminalView {...props} />);
  expect(screen.queryByRole("note")).toBeNull();
});

it("offers a selection to the conversation only when the person picked it, and copies it as asked", async () => {
  const quoted = vi.fn();
  const { term } = await setup({ onQuote: quoted });
  const body = document.querySelector<HTMLElement>(".term-body")!;
  await userEvent.click(body);
  expect(screen.queryByRole("menuitem", { name: "发给对话" })).toBeNull();
  term().selection = "FAIL: TestRetryAfter";
  act(() => { body.dispatchEvent(new MouseEvent("mouseup", { bubbles: true, clientX: 40, clientY: 40 })); });
  await userEvent.click(await screen.findByRole("menuitem", { name: "发给对话" }));
  expect(quoted).toHaveBeenCalledWith("FAIL: TestRetryAfter");
  expect(screen.queryByRole("menuitem", { name: "发给对话" })).toBeNull();
});

it("opens an address from the output in the built-in browser or copies it, on the person's choice", async () => {
  const page = vi.fn();
  await setup({ onOpenPage: page });
  act(() => fake.link.open!(new MouseEvent("click", { clientX: 10, clientY: 10 }), "http://localhost:5173/"));
  await userEvent.click(await screen.findByRole("menuitem", { name: "在内置浏览器打开" }));
  expect(page).toHaveBeenCalledWith("http://localhost:5173/");
});

it("copies the selection on Ctrl+C instead of interrupting the shell, and sends the interrupt otherwise", async () => {
  const write = vi.fn(async () => {});
  Object.defineProperty(navigator, "clipboard", { value: { writeText: write }, configurable: true });
  const { term } = await setup();
  term().selection = "picked";
  const press = () => term().keys!(new KeyboardEvent("keydown", { key: "c", ctrlKey: true }));
  expect(press()).toBe(false);
  expect(write).toHaveBeenCalledWith("picked");
  term().selection = "";
  expect(press()).toBe(true);
});

it("copies everything the screen holds from the more menu", async () => {
  const write = vi.fn(async () => {});
  Object.defineProperty(navigator, "clipboard", { value: { writeText: write }, configurable: true });
  await setup();
  await userEvent.click(screen.getByRole("button", { name: "更多：复制全部、字体大小" }));
  await userEvent.click(screen.getByRole("menuitem", { name: "复制全部" }));
  expect(write).toHaveBeenCalledWith("first line\nsecond line");
});

it("opens a terminal in the picked shell beside this one, and does nothing for the shell it already is", async () => {
  const shell = vi.fn();
  await setup({ onShell: shell, shells: [{ name: "Git Bash", kind: "bash", path: "/usr/bin/bash" }, { name: "PowerShell", kind: "pwsh", path: "/usr/bin/pwsh" }] });
  await userEvent.click(screen.getByRole("button", { name: "终端程序" }));
  await userEvent.click(screen.getByRole("menuitem", { name: /PowerShell/ }));
  expect(shell).toHaveBeenCalledWith("pwsh");
});

it("says which machine a remote project's terminal is on, and says nothing for this one", async () => {
  const { props, view } = await setup();
  expect(document.querySelector(".term-host")).toBeNull();
  view.unmount();
  render(<TerminalHostContext.Provider value="gpu-box"><TerminalView {...props} /></TerminalHostContext.Provider>);
  expect(document.querySelector(".term-host")?.textContent).toBe("在 gpu-box 上");
});
