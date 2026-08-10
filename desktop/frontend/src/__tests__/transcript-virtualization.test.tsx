// Run: tsx src/__tests__/transcript-virtualization.test.tsx
//
// Block-level DOM virtualization of the transcript:
// - a small viewport mounts only the visible rows + overscan (offscreen rows
//   create no Markdown/ToolCard subtrees),
// - prepending an older-history page keeps the reading position (key-anchored
//   compensation),
// - while pinned, streaming growth re-pins to the tail without remounting
//   history rows,
// - mounted history rows trigger lazy full-content resolution,
// - the rewind signal scrolls to the rewound-to question's virtual row.

import { createTranscriptHarness } from "./transcript-dom-harness";
import type { Item, LiveStream } from "../lib/useController";

let passed = 0;
let failed = 0;

function ok(cond: unknown, label: string) {
  if (cond) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\ntranscript virtualization");

function turns(count: number, prefix = ""): Item[] {
  const items: Item[] = [];
  for (let i = 0; i < count; i += 1) {
    items.push({ kind: "user", id: `${prefix}u${i}`, text: `question ${prefix}${i}` });
    items.push({ kind: "assistant", id: `${prefix}a${i}`, text: `answer ${prefix}${i}`, reasoning: "", streaming: false });
  }
  return items;
}

function dispatchScroll(el: HTMLElement) {
  el.dispatchEvent(new Event("scroll"));
}

function firstTextNode(root: Node): Text | null {
  if (root.nodeType === Node.TEXT_NODE) return root as Text;
  for (const child of Array.from(root.childNodes)) {
    const found = firstTextNode(child);
    if (found) return found;
  }
  return null;
}

// ── Windowed mounting ─────────────────────────────────────────────────────────
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    await harness.render(turns(30), { running: false });
    const container = harness.container;
    const mountedRows = container.querySelectorAll(".transcript__row").length;
    const mountedAnswers = container.querySelectorAll(".msg--assistant").length;
    ok(mountedRows > 0 && mountedRows <= 24, `small viewport mounts only a window of rows (mounted ${mountedRows} of 90)`);
    ok(mountedAnswers > 0 && mountedAnswers < 30, `offscreen answers mount no Markdown subtree (mounted ${mountedAnswers} of 30)`);
    const sizer = container.querySelector<HTMLElement>(".transcript__virtual-sizer");
    ok(Number.parseFloat(sizer?.style.height ?? "0") > 2000, "sizer carries the full virtual height so the scrollbar maps the whole transcript");
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Prepend anchor compensation ───────────────────────────────────────────────
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    await harness.render(turns(20), { running: false });
    // Let the initial bottom-pin frames (scrollToBottomAfterLayout) settle
    // before taking manual control of the scroll position.
    await harness.settle();
    const el = harness.scrollElement();
    el.scrollTop = 2000;
    dispatchScroll(el);
    await harness.flush();
    const before = el.scrollTop;
    // The first fully-visible row before the prepend is the anchor.
    const anchorIdBefore = Array.from(harness.container.querySelectorAll(".transcript__row"))
      .map((row) => {
        const match = /translate3d\(0(?:px)?, ([\d.-]+)px/.exec((row as HTMLElement).style.transform);
        return {
          anchorId: row.querySelector("[data-question-anchor]")?.id,
          top: match ? Number(match[1]) : -1,
        };
      })
      .filter(({ anchorId, top }) => anchorId != null && top >= before)
      .sort((a, b) => a.top - b.top)[0]?.anchorId;
    ok(anchorIdBefore != null, "found a fully-visible anchor row before the prepend");
    // Prepend five older turns (15 rows) — the reading position must follow
    // the anchor row, not the row index.
    await harness.render([...turns(5, "old-"), ...turns(20)], { running: false });
    await harness.waitFor(
      () => anchorIdBefore != null && harness.container.querySelector(`#${anchorIdBefore}`) !== null,
      "the pre-prepend anchor row to remount",
    );
    const delta = el.scrollTop - before;
    ok(delta > 0, `prepended history shifts the scroll offset down (delta ${delta})`);
    ok(
      anchorIdBefore != null && harness.container.querySelector(`#${anchorIdBefore}`) !== null,
      "the pre-prepend anchor row is still mounted after the prepend",
    );
    const anchorRow = anchorIdBefore ? harness.container.querySelector(`#${anchorIdBefore}`)?.closest(".transcript__row") : null;
    if (anchorRow) {
      const transform = (anchorRow as HTMLElement).style.transform;
      const match = /translate3d\(0(?:px)?, ([\d.-]+)px/.exec(transform);
      const top = match ? Number(match[1]) : Number.NaN;
      // The anchor question keeps its viewport position: row start ≈ scrollTop
      // plus the same intra-row offset as before (it was exactly at 2000).
      ok(Math.abs(el.scrollTop - top) < 200, `anchor row stays at the reading position (scrollTop ${el.scrollTop}, row top ${top})`);
    }
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Tail streaming pin + history row isolation ────────────────────────────────
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    const items: Item[] = [
      ...turns(10),
      { kind: "user", id: "u-live", text: "stream" },
      { kind: "assistant", id: "live-1", text: "", reasoning: "", streaming: true },
    ];
    const live: LiveStream = { id: "live-1", text: "token", reasoning: "", reasoningComplete: true };
    await harness.render(items, { running: true, live });
    const el = harness.scrollElement();
    const historyRow = harness.container.querySelector("#question-anchor-u0")?.closest(".transcript__row") ?? null;

    // Scroll away programmatically WITHOUT a scroll event: the pin intent is
    // still set, so the next streaming update must re-pin to the tail.
    el.scrollTop = 0;
    await harness.render(items, { running: true, live: { ...live, text: "token token token token token" } });
    const sizer = harness.container.querySelector<HTMLElement>(".transcript__virtual-sizer");
    const total = Number.parseFloat(sizer?.style.height ?? "0");
    ok(total > 0 && el.scrollTop === total, `streaming update re-pins to the tail (scrollTop ${el.scrollTop}, total ${total})`);
    const historyRowAfter = harness.container.querySelector("#question-anchor-u0")?.closest(".transcript__row") ?? null;
    ok(historyRow !== null && historyRow === historyRowAfter, "streaming tokens never remount history rows");
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Lazy content refs resolve on row mount ────────────────────────────────────
{
  const harness = await createTranscriptHarness();
  try {
    const storeModule = await harness.loadModule<typeof import("../lib/transcriptStore")>("/src/lib/transcriptStore.ts");
    const store = storeModule.getTranscriptStore();
    const calls: Array<[string | undefined, string]> = [];
    const original = store.requestEntryFullContent.bind(store);
    store.requestEntryFullContent = (tabId: string | undefined, entryId: string) => {
      calls.push([tabId, entryId]);
      original(tabId, entryId);
    };
    const items: Item[] = [
      { kind: "user", id: "he:e1", text: "restored question" },
      { kind: "assistant", id: "he:e2", text: "restored answer", reasoning: "", streaming: false },
    ];
    await harness.render(items, { running: false, tabId: "tab-x" });
    ok(calls.some(([tabId, entryId]) => tabId === "tab-x" && entryId === "e1"), "mounted user row triggers lazy content resolution");
    ok(calls.some(([tabId, entryId]) => tabId === "tab-x" && entryId === "e2"), "mounted answer row triggers lazy content resolution");
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Native cross-page selection keeps a continuous DOM range ─────────────────
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    await harness.render(turns(30), { running: false, tabId: "selection-tab" });
    await harness.settle();
    const el = harness.scrollElement();
    el.scrollTop = 0;
    dispatchScroll(el);
    await harness.flush();

    const anchorBody = harness.container.querySelector<HTMLElement>("#question-anchor-u0 .msg__body")
      ?? harness.container.querySelector<HTMLElement>("#question-anchor-u0")?.closest<HTMLElement>(".msg")?.querySelector(".msg__body")
      ?? null;
    ok(anchorBody != null, "selection anchor row is mounted");
    anchorBody?.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true, button: 0 }));
    await harness.flush();

    let focusBody: HTMLElement | null = null;
    for (let top = 1000; top <= 8000 && !focusBody; top += 500) {
      el.scrollTop = top;
      dispatchScroll(el);
      await harness.flush();
      focusBody = harness.container.querySelector<HTMLElement>("#question-anchor-u20 .msg__body")
        ?? harness.container.querySelector<HTMLElement>("#question-anchor-u20")?.closest<HTMLElement>(".msg")?.querySelector(".msg__body")
        ?? null;
    }
    ok(focusBody != null, "selection focus row mounts after crossing virtual pages");

    const anchorText = anchorBody ? firstTextNode(anchorBody) : null;
    const focusText = focusBody ? firstTextNode(focusBody) : null;
    const selection = document.getSelection();
    if (anchorText && focusText && selection) {
      const range = document.createRange();
      range.setStart(anchorText, 0);
      range.setEnd(focusText, focusText.data.length);
      selection.removeAllRanges();
      selection.addRange(range);
      document.dispatchEvent(new Event("selectionchange"));
      await harness.flush();
      ok(selection.anchorNode?.isConnected && selection.focusNode?.isConnected, "native selection endpoints stay connected");
      ok(harness.container.querySelectorAll(".transcript__row").length >= 61, "every intervening row stays mounted while native selection is active");

      document.dispatchEvent(new MouseEvent("pointerup", { bubbles: true, button: 0 }));
      selection.removeAllRanges();
      document.dispatchEvent(new Event("selectionchange"));
      await harness.flush();
      ok(harness.container.querySelectorAll(".transcript__row").length <= 24, "clearing selection releases retained rows");
    } else {
      ok(false, "selection endpoint text nodes are available");
    }
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Rewind signal lands on the rewound-to question row ───────────────────────
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    const items = turns(10);
    await harness.render(items, { running: false, rewindSignal: 0 });
    const el = harness.scrollElement();
    el.scrollTop = 0;
    dispatchScroll(el);
    await harness.flush();
    await harness.render(items, { running: false, rewindSignal: 1 });
    // jsdom does not fire scroll events for programmatic scrolls; browsers do.
    dispatchScroll(el);
    await harness.settle();
    const target = harness.container.querySelector("#question-anchor-u9");
    ok(Boolean(target), "rewind mounts the rewound-to question row");
    ok(el.scrollTop > 1000, `rewind scrolls down to the last question (scrollTop ${el.scrollTop})`);
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
