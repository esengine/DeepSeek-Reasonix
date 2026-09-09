// Run: tsx src/__tests__/tool-card-running-elapsed.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ToolCard } from "../components/ToolCard";
import { localizedNoticeText } from "../lib/controllerNotices";
import { LocaleProvider } from "../lib/i18n";
import { zh } from "../locales/zh";
import { zhTW } from "../locales/zh-TW";
import { initialState, reducer, type Item } from "../lib/useController";

type ToolItem = Extract<Item, { kind: "tool" }>;

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

const originalNow = Date.now;
let fakeNow = 100_000;
Date.now = () => fakeNow;

// ToolCard ticks through window.setInterval; capturing the callbacks lets the
// test advance the clock without waiting real seconds.
const intervals = new Map<number, () => void>();
let nextIntervalId = 1;

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  dom.window.matchMedia = () => ({
    matches: true,
    media: "(prefers-reduced-motion: reduce)",
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  });
  dom.window.setInterval = ((handler: TimerHandler) => {
    const id = nextIntervalId++;
    if (typeof handler === "function") intervals.set(id, handler as () => void);
    return id;
  }) as typeof dom.window.setInterval;
  dom.window.clearInterval = ((id?: number) => {
    if (id !== undefined) intervals.delete(id);
  }) as typeof dom.window.clearInterval;
  return dom;
}

async function renderCard(item: ToolItem) {
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(ToolCard, { item })));
    await flushTimers();
  });
  return {
    async cleanup() {
      await act(async () => {
        root.unmount();
      });
      dom.window.close();
    },
  };
}

async function advance(ms: number) {
  fakeNow += ms;
  await act(async () => {
    for (const fire of [...intervals.values()]) fire();
    await flushTimers();
  });
}

function durationText(): string | null {
  return document.querySelector(".tool__duration")?.textContent ?? null;
}

console.log("\ntool card running elapsed");

let s = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
s = reducer(s, {
  type: "event",
  e: { kind: "tool_dispatch", tool: { id: "run-bash", name: "bash", args: `{"command":"sleep 600"}`, readOnly: false } },
});
const running = s.items.find((it): it is ToolItem => it.kind === "tool" && it.id === "run-bash");
eq(running?.status, "running", "dispatch creates a running card");
eq(running?.startedAt, 100_000, "dispatch stamps startedAt from the frontend clock");

{
  fakeNow += 5_000;
  s = reducer(s, {
    type: "event",
    e: { kind: "tool_dispatch", tool: { id: "run-partial", name: "write_file", partial: true, argChars: 12, readOnly: false } },
  });
  fakeNow += 5_000;
  s = reducer(s, {
    type: "event",
    e: { kind: "tool_dispatch", tool: { id: "run-partial", name: "write_file", args: `{"path":"a.txt","content":"x"}`, readOnly: false } },
  });
  const merged = s.items.find((it): it is ToolItem => it.kind === "tool" && it.id === "run-partial");
  eq(merged?.startedAt, 105_000, "the full dispatch keeps the partial dispatch's startedAt");
  fakeNow = 100_000;
}

{
  const ui = await renderCard(running!);
  eq(durationText(), "0s", "running card shows a live elapsed label at dispatch");
  eq(intervals.size, 1, "running card registers exactly one ticker");
  await advance(83_000);
  eq(durationText(), "1m23s", "live elapsed label advances with the clock");
  await advance(60_000);
  eq(durationText(), "2m23s", "live elapsed label keeps advancing");
  await ui.cleanup();
  eq(intervals.size, 0, "unmount clears the ticker");
  fakeNow = 100_000;
}

{
  const subagent: ToolItem = {
    kind: "tool",
    id: "task-1",
    name: "task",
    args: "{}",
    readOnly: false,
    status: "running",
    startedAt: fakeNow,
    subagentProgress: { phase: "running", reasoning: "", text: "", notice: "", lastActivityAt: fakeNow, truncated: false, startedAt: fakeNow },
  };
  const ui = await renderCard(subagent);
  eq(intervals.size, 1, "sub-agent card registers exactly one ticker (no double tick)");
  eq(durationText(), null, "sub-agent card leaves elapsed to its progress chip");
  await advance(5_000);
  const chip = document.querySelector(".tool__subagent-chip")?.textContent ?? "";
  ok(chip.includes("5s"), `sub-agent chip still ticks (got ${JSON.stringify(chip)})`);
  await ui.cleanup();
  fakeNow = 100_000;
}

{
  s = reducer(s, {
    type: "event",
    e: { kind: "tool_result", tool: { id: "run-bash", name: "bash", readOnly: false, output: "ok", durationMs: 83421 } },
  });
  const done = s.items.find((it): it is ToolItem => it.kind === "tool" && it.id === "run-bash");
  eq(done?.status, "done", "tool_result settles the card");
  const ui = await renderCard(done!);
  eq(durationText(), "83421 ms", "completed card shows the final duration");
  eq(intervals.size, 0, "completed card registers no ticker");
  await advance(10_000);
  eq(durationText(), "83421 ms", "completed card's duration does not drift with the clock");
  await ui.cleanup();
  fakeNow = 100_000;
}

{
  const hydrated: ToolItem = { kind: "tool", id: "hydrated", name: "bash", args: `{"command":"ls"}`, readOnly: false, status: "running" };
  const ui = await renderCard(hydrated);
  eq(durationText(), null, "running card without startedAt hides the elapsed label");
  eq(intervals.size, 0, "running card without startedAt does not tick");
  await ui.cleanup();
}

{
  const before = s;
  s = reducer(s, {
    type: "event",
    e: { kind: "notice", level: "warn", code: "turn_stalled", text: "No events for 10m0s; the turn may be stuck." },
  });
  const notice = s.items[s.items.length - 1];
  eq(notice?.kind, "notice", "turn_stalled appends a transcript notice");
  ok(notice?.kind === "notice" && notice.level === "warn", "turn_stalled notice keeps its warn level");
  ok(
    notice?.kind === "notice" && notice.text === "No progress for a while. The turn is still running; if it looks stuck, press Stop.",
    "turn_stalled notice text is localized by code",
  );
  eq(s.running, before.running, "turn_stalled does not change the running flag");
  eq(s.turnActive, before.turnActive, "turn_stalled does not end the turn");
  eq(s.streamInterruptNoticeShown, before.streamInterruptNoticeShown, "turn_stalled does not touch the stream-interrupt flag");
}

eq(
  localizedNoticeText("No events for 10m0s; the turn may be stuck.", "turn_stalled"),
  "No progress for a while. The turn is still running; if it looks stuck, press Stop.",
  "localizedNoticeText maps turn_stalled to the English copy",
);
eq(zh["notice.turnStalled"], "已经有一段时间没有任何进展。回合仍在运行；如果看起来卡住了，请点击停止。", "zh copy for turn_stalled");
eq(zhTW["notice.turnStalled"], "已經有一段時間沒有任何進展。回合仍在執行；如果看起來卡住了，請點擊停止。", "zh-TW copy for turn_stalled");

Date.now = originalNow;
console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
