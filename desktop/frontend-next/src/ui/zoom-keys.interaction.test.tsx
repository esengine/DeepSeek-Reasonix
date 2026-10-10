// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, configure, fireEvent, render, waitFor } from "@testing-library/react";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import { host } from "../port/host";
import { MockPort } from "../port/mock";
import { boot, STORAGE } from "../i18n";
import type { AgentPort } from "../port/port";

configure({ asyncUtilTimeout: 10_000 });

beforeEach(() => {
  vi.spyOn(host(), "inShell").mockReturnValue(true);
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  document.documentElement.removeAttribute("style");
});

function held() {
  const hub = new MockHub();
  const port = new MockPort() as unknown as AgentPort;
  port.providerSetup = async () => null;
  port.welcomeSeen = async () => true;
  hub.portFor = () => port;
  return { hub, port };
}

async function mount(hub: MockHub, port: AgentPort) {
  const read = vi.spyOn(port, "appearance");
  render(<App hub={hub} />);
  const box = await waitFor(() => {
    const el = document.querySelector<HTMLTextAreaElement>(".pane textarea");
    if (!el) throw new Error("no composer yet");
    return el;
  });
  // A press before the kernel has announced its range has nothing to step.
  await waitFor(() => expect(read).toHaveBeenCalled());
  await act(async () => { await read.mock.results[0].value; });
  read.mockRestore();
  return box;
}

const painted = () => document.documentElement.style.getPropertyValue("zoom");
const ctrl = (target: Element, key: string, init: Partial<KeyboardEventInit> = {}) =>
  fireEvent.keyDown(target, { key, code: key === "=" ? "Equal" : key === "-" ? "Minus" : `Digit${key}`, ctrlKey: true, ...init });

describe("interface scale from the keyboard", { timeout: 30_000 }, () => {
  it("enlarges, shrinks and resets through the same saved setting", async () => {
    const { hub, port } = held();
    const box = await mount(hub, port);
    const save = vi.spyOn(port, "saveAppearance");

    ctrl(box, "=");
    await waitFor(() => expect(painted()).toBe("1.15"));
    expect(save).toHaveBeenLastCalledWith(expect.objectContaining({ zoom: 1.15 }));
    ctrl(box, "+", { shiftKey: true });
    await waitFor(() => expect(painted()).toBe("1.3"));
    ctrl(box, "-");
    await waitFor(() => expect(painted()).toBe("1.15"));
    ctrl(box, "0");
    await waitFor(() => expect(painted()).toBe(""));
    await waitFor(() => expect(save).toHaveBeenLastCalledWith(expect.objectContaining({ zoom: 1 })));
  });

  it("stops at both ends of the range", async () => {
    const { hub, port } = held();
    const box = await mount(hub, port);
    for (let i = 0; i < 8; i++) ctrl(box, "=");
    await waitFor(() => expect(painted()).toBe("1.8"));
    const save = vi.spyOn(port, "saveAppearance");
    ctrl(box, "=");
    expect(save).not.toHaveBeenCalled();
    for (let i = 0; i < 8; i++) ctrl(box, "-");
    await waitFor(() => expect(painted()).toBe("0.8"));
    save.mockClear();
    ctrl(box, "-");
    expect(save).not.toHaveBeenCalled();
  });

  it("is still the saved choice after the window is reloaded", async () => {
    const { hub, port } = held();
    const box = await mount(hub, port);
    ctrl(box, "=");
    await waitFor(() => expect(painted()).toBe("1.15"));
    cleanup();
    document.documentElement.removeAttribute("style");
    await mount(hub, port);
    await waitFor(() => expect(painted()).toBe("1.15"));
  });

  it("works from a text field because nothing there uses these chords", async () => {
    const { hub, port } = held();
    const box = await mount(hub, port);
    box.focus();
    ctrl(box, "=");
    await waitFor(() => expect(painted()).toBe("1.15"));
  });

  it("leaves a press that something else already answered alone", async () => {
    const { hub, port } = held();
    const box = await mount(hub, port);
    const save = vi.spyOn(port, "saveAppearance");
    const spent = new KeyboardEvent("keydown", { key: "=", code: "Equal", ctrlKey: true, bubbles: true, cancelable: true });
    box.addEventListener("keydown", (e) => e.preventDefault(), { once: true });
    box.dispatchEvent(spent);
    expect(save).not.toHaveBeenCalled();
  });

  it("ignores the plain key and other modifiers", async () => {
    const { hub, port } = held();
    const box = await mount(hub, port);
    const save = vi.spyOn(port, "saveAppearance");
    fireEvent.keyDown(box, { key: "=", code: "Equal" });
    ctrl(box, "=", { altKey: true });
    ctrl(box, "-", { metaKey: true });
    expect(save).not.toHaveBeenCalled();
  });

  it("does not touch the body text size", async () => {
    const { hub, port } = held();
    const box = await mount(hub, port);
    const read = document.documentElement.style.getPropertyValue("--read");
    ctrl(box, "=");
    await waitFor(() => expect(painted()).toBe("1.15"));
    expect(document.documentElement.style.getPropertyValue("--read")).toBe(read);
  });

  it("keeps Ctrl+R and the other window shortcuts as they were", async () => {
    const { hub, port } = held();
    const box = await mount(hub, port);
    ctrl(box, "r");
    expect(painted()).toBe("");
  });

  it("reads a shifted equals and a French zero by their positions", async () => {
    const { hub, port } = held();
    const box = await mount(hub, port);
    fireEvent.keyDown(box, { key: "+", code: "Equal", ctrlKey: true, shiftKey: true });
    await waitFor(() => expect(painted()).toBe("1.15"));
    fireEvent.keyDown(box, { key: "à", code: "Digit0", ctrlKey: true });
    await waitFor(() => expect(painted()).toBe(""));
  });

  it("leaves the browser's own zoom alone outside the shell", async () => {
    vi.spyOn(host(), "inShell").mockReturnValue(false);
    const { hub, port } = held();
    const save = vi.spyOn(port, "saveAppearance");
    const box = await mount(hub, port);
    for (const key of ["=", "+", "-", "0"]) {
      const ev = new KeyboardEvent("keydown", { key, code: key === "-" ? "Minus" : key === "0" ? "Digit0" : "Equal", ctrlKey: true, bubbles: true, cancelable: true });
      box.dispatchEvent(ev);
      expect(ev.defaultPrevented).toBe(false);
    }
    expect(painted()).toBe("");
    expect(save).not.toHaveBeenCalled();
  });
});
