// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { act, cleanup, configure, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import type { HistoryMessage } from "../port/port";

afterEach(cleanup);

// Each case mounts the whole window over a sixty-turn transcript, which on a
// loaded machine takes longer than the defaults allow.
configure({ asyncUtilTimeout: 10_000 });

// A conversation long enough that the transcript mounts only part of it: the
// count has to come from the rows, not from what happens to be in the document.
function history(turns: number): HistoryMessage[] {
  const out: HistoryMessage[] = [];
  for (let i = 0; i < turns; i++) {
    out.push({ role: "user", content: `第 ${i} 个问题，看一下 Parser 的实现。` });
    out.push({ role: "assistant", content: `第 ${i} 段回答。` });
  }
  return out;
}

async function openWindow() {
  const hub = new MockHub();
  const build = hub.portFor.bind(hub);
  hub.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    port.history = async () => history(60);
    return port;
  };
  render(<App hub={hub} />);
  const composer = await waitFor(() => {
    const box = document.querySelector<HTMLTextAreaElement>(".pane textarea");
    if (!box) throw new Error("no composer yet");
    return box;
  });
  await waitFor(() => expect(document.querySelectorAll("[data-item]").length).toBeGreaterThan(0));
  return { composer };
}

const bar = () => document.querySelector(".tfind");
const field = () => screen.getByRole("searchbox", { name: "在这段对话里查找" }) as HTMLInputElement;
const count = () => document.querySelector(".tfind .n")?.textContent ?? "";
// jsdom reports no platform, so the window reads it as not macOS: Ctrl is the chord.
const chordF = (target: Element, init: Partial<KeyboardEventInit> = {}) =>
  fireEvent.keyDown(target, { key: "f", code: "KeyF", ctrlKey: true, ...init });

describe("find in the conversation", { timeout: 30_000 }, () => {
  it("opens from the composer, counts every row, steps both ways and closes on Escape", async () => {
    const { composer } = await openWindow();
    composer.focus();
    chordF(composer);
    await waitFor(() => expect(bar()).not.toBeNull());
    await waitFor(() => expect(document.activeElement).toBe(field()));

    fireEvent.change(field(), { target: { value: "parser" } });
    // Case-insensitive, and every one of the sixty questions — most unmounted.
    await waitFor(() => expect(count()).toBe("1 / 60"));

    await userEvent.keyboard("{Enter}");
    expect(count()).toBe("2 / 60");
    await userEvent.keyboard("{ArrowDown}");
    expect(count()).toBe("3 / 60");
    await userEvent.keyboard("{ArrowUp}");
    expect(count()).toBe("2 / 60");
    await userEvent.keyboard("{Shift>}{Enter}{/Shift}");
    expect(count()).toBe("1 / 60");
    // Wraps: stopping at the first leaves the last to be found by hand.
    await userEvent.keyboard("{Shift>}{Enter}{/Shift}");
    expect(count()).toBe("60 / 60");

    await userEvent.keyboard("{Escape}");
    expect(bar()).toBeNull();
  });

  it("matches Chinese as a plain substring and says when nothing matches", async () => {
    const { composer } = await openWindow();
    chordF(composer);
    await waitFor(() => expect(bar()).not.toBeNull());
    fireEvent.change(field(), { target: { value: "个问" } });
    await waitFor(() => expect(count()).toBe("1 / 60"));
    fireEvent.change(field(), { target: { value: "不存在的话" } });
    await waitFor(() => expect(count()).toBe("无结果"));
    expect(screen.getByRole("button", { name: "下一处" })).toHaveProperty("disabled", true);
  });

  it("leaves a text field that keeps its own keys alone", async () => {
    await openWindow();
    const other = document.createElement("input");
    document.body.append(other);
    other.focus();
    const taken = !chordF(other);
    expect(taken).toBe(false);
    expect(bar()).toBeNull();
    other.remove();
  });

  it("leaves a press an editor has already answered", async () => {
    await openWindow();
    const editor = document.createElement("div");
    editor.addEventListener("keydown", (e) => e.preventDefault());
    document.body.append(editor);
    chordF(editor);
    expect(bar()).toBeNull();
    editor.remove();
  });

  it("does not read AltGr as the chord", async () => {
    const { composer } = await openWindow();
    chordF(composer, { altKey: true });
    expect(bar()).toBeNull();
  });

  it("reads the key's position when the layout reports no Latin letter", async () => {
    const { composer } = await openWindow();
    chordF(composer, { key: "Process" });
    await waitFor(() => expect(bar()).not.toBeNull());
    await userEvent.keyboard("{Escape}");
    chordF(composer, { key: "а" });
    await waitFor(() => expect(bar()).not.toBeNull());
    await userEvent.keyboard("{Escape}");
    chordF(composer, { key: "F" });
    await waitFor(() => expect(bar()).not.toBeNull());
  });

  it("is reachable without a keyboard, from the title bar and the palette", async () => {
    await openWindow();
    await userEvent.click(screen.getByRole("button", { name: "在这段对话里查找" }));
    await waitFor(() => expect(bar()).not.toBeNull());
    await userEvent.keyboard("{Escape}");
    expect(bar()).toBeNull();

    await act(async () => void fireEvent.keyDown(window, { key: "k", code: "KeyK", ctrlKey: true }));
    const palette = await screen.findByRole("dialog", { name: "搜索与快捷操作" });
    await userEvent.click([...palette.querySelectorAll("button")].find((b) => b.textContent?.includes("在这段对话里查找"))!);
    await waitFor(() => expect(bar()).not.toBeNull());
    await waitFor(() => expect(document.activeElement).toBe(field()));
  });
});
