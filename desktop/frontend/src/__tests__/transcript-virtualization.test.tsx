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
    ok(harness.scrollElement().scrollHeight > 2000, "Virtuoso exposes the full virtual height to the transcript scrollbar");
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
    // Match a reader leaving the tail (wheel-up). A raw scrollTop write leaves
    // the pin set, and a later LAST/undershoot path would snap back to bottom.
    el.dispatchEvent(new WheelEvent("wheel", { deltaY: -40, bubbles: true }));
    dispatchScroll(el);
    await harness.flush();
    const before = el.scrollTop;
    const anchor = harness.container.querySelector<HTMLElement>("#question-anchor-u5")?.closest<HTMLElement>(".transcript__row") ?? null;
    const anchorIdBefore = anchor?.querySelector("[data-question-anchor]")?.id;
    const absoluteIndexBefore = anchor?.dataset.itemIndex;
    ok(anchorIdBefore != null && absoluteIndexBefore != null, "found a stable mounted anchor row before the prepend");
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
    const anchorRow = anchorIdBefore ? harness.container.querySelector(`#${anchorIdBefore}`)?.closest<HTMLElement>(".transcript__row") : null;
    ok(
      anchorRow?.dataset.itemIndex === absoluteIndexBefore,
      `prepend preserves the anchor's absolute Virtuoso index (${absoluteIndexBefore} → ${anchorRow?.dataset.itemIndex})`,
    );
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
    const historyRow = Array.from(harness.container.querySelectorAll<HTMLElement>(".transcript__row"))
      .find((row) => row.dataset.rowKey !== "a:live-1") ?? null;

    const beforeDistance = el.scrollHeight - el.clientHeight - el.scrollTop;
    ok(Math.abs(beforeDistance) <= 1, "the live transcript starts pinned to the tail");
    await harness.render(items, { running: true, live: { ...live, text: "token token token token token" } });
    await harness.flush();
    const distance = el.scrollHeight - el.clientHeight - el.scrollTop;
    ok(Math.abs(distance) <= 1, `streaming update re-pins to the tail (bottom distance ${distance})`);
    const historyRowAfter = historyRow?.dataset.rowKey
      ? Array.from(harness.container.querySelectorAll<HTMLElement>(".transcript__row")).find((row) => row.dataset.rowKey === historyRow.dataset.rowKey) ?? null
      : null;
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

// ── Lazy content size invalidation rebuilds the Virtuoso size tree ───────────
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    const items = turns(20);
    await harness.render(items, { running: false, tabId: "layout-tab", historyLayoutRevision: 0 });
    await harness.settle();
    const before = harness.container.querySelector("[data-testid='virtuoso-item-list']");
    await harness.render(items, { running: false, tabId: "layout-tab", historyLayoutRevision: 1 });
    for (let i = 0; i < 5; i += 1) await harness.flush();
    const after = harness.container.querySelector("[data-testid='virtuoso-item-list']");
    ok(before != null && after != null && before !== after, "lazy content layout revision rebuilds Virtuoso's cached size tree");
    const el = harness.scrollElement();
    const distance = el.scrollHeight - el.clientHeight - el.scrollTop;
    ok(Math.abs(distance) <= 1, `tail-follow survives the size-tree rebuild (bottom distance ${distance})`);
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Cross-page selection promotes to the logical model ───────────────────────
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

    const focusBody = harness.container.querySelector<HTMLElement>("#question-anchor-u1 .msg__body")
      ?? harness.container.querySelector<HTMLElement>("#question-anchor-u1")?.closest<HTMLElement>(".msg")?.querySelector(".msg__body")
      ?? null;
    ok(focusBody != null, "a neighboring focus row is mounted before logical promotion");

    const anchorText = anchorBody ? firstTextNode(anchorBody) : null;
    const focusText = focusBody ? firstTextNode(focusBody) : null;
    const selection = document.getSelection();
    if (anchorText && focusText && selection) {
      const caretDocument = document as Document & {
        caretPositionFromPoint?: () => { offsetNode: Node; offset: number };
      };
      caretDocument.caretPositionFromPoint = () => ({ offsetNode: focusText, offset: focusText.data.length });
      const range = document.createRange();
      range.setStart(anchorText, 0);
      range.setEnd(focusText, focusText.data.length);
      selection.removeAllRanges();
      selection.addRange(range);
      document.dispatchEvent(new Event("selectionchange"));
      await harness.flush();
      const storeModule = await harness.loadModule<typeof import("../lib/transcriptSelectionStore")>("/src/lib/transcriptSelectionStore.ts");
      ok(storeModule.transcriptSelectionStore.getSnapshot().mode === "logical-dragging", "cross-row selection promotes before virtualization can unmount its anchor");

      el.scrollTop = 6000;
      dispatchScroll(el);
      await harness.flush();
      ok(harness.container.querySelectorAll(".transcript__row").length <= 30, "logical selection keeps the transcript windowed across virtual pages");
      ok(storeModule.transcriptSelectionStore.getSnapshot().mode === "logical-dragging", "logical selection survives its native anchor row unmounting");

      document.dispatchEvent(new MouseEvent("pointerup", { bubbles: true, button: 0 }));
      await harness.flush();
      ok(storeModule.transcriptSelectionStore.getSnapshot().mode === "logical-settled", "cross-page logical selection settles after pointerup");
      delete caretDocument.caretPositionFromPoint;
      storeModule.transcriptSelectionStore.clear("test-cleanup");
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

// ── Short transcripts must not clone the first user bubble at the top ────────
// Virtuoso alignToBottom uses margin-top:auto to pin short lists to the
// composer. Combined with firstItemIndex it also paints a second copy of the
// first user row at the scroller top, leaving a large empty band in between.
{
  const harness = await createTranscriptHarness({ viewportHeight: 600, rowHeight: 80 });
  try {
    await harness.render(
      [
        { kind: "user", id: "u-short", text: "你好" },
        { kind: "assistant", id: "a-short", text: "hello", reasoning: "", streaming: false },
      ],
      { running: false },
    );
    await harness.settle();
    const users = harness.container.querySelectorAll(".msg--user");
    ok(users.length === 1, `a one-turn transcript mounts the user bubble once (got ${users.length})`);
    const list = harness.container.querySelector<HTMLElement>('[data-testid="virtuoso-item-list"]');
    ok(list != null, "short transcript mounts the Virtuoso item list");
    ok(list?.style.marginTop !== "auto", `short content is not bottom-shifted (marginTop=${JSON.stringify(list?.style.marginTop ?? null)})`);
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── A short interrupted turn has no phantom "bottom" to jump to ─────────────
// Once alignToBottom was removed, short content correctly stayed at the top.
// An upward wheel intent still unpinned it even though the scroller had no
// overflow, exposing a jump-bottom button whose click could not move anywhere.
{
  const harness = await createTranscriptHarness({ viewportHeight: 700, rowHeight: 60 });
  try {
    const interrupted: Item[] = [
      { kind: "user", id: "u-interrupted", text: "inspect the four metrics" },
      { kind: "assistant", id: "r1", text: "", reasoning: "checking the first source", streaming: false },
      { kind: "tool", id: "t1", name: "read_file", args: "{}", output: "ok", status: "done", readOnly: true },
      { kind: "assistant", id: "r2", text: "", reasoning: "checking the second source", streaming: false },
      { kind: "tool", id: "t2", name: "read_file", args: "{}", output: "ok", status: "done", readOnly: true },
      { kind: "notice", id: "cancelled", level: "info", code: "cancelled_turn_display", text: "This turn was interrupted." },
    ];
    await harness.render(interrupted, { running: false });
    await harness.settle();
    const el = harness.scrollElement();
    ok(el.scrollHeight <= el.clientHeight, `interrupted fixture has no scroll range (${el.scrollHeight}/${el.clientHeight})`);

    el.dispatchEvent(new WheelEvent("wheel", { deltaY: -40, bubbles: true }));
    dispatchScroll(el);
    await harness.flush();

    ok(
      el.dataset.scrollMode === "tail-follow",
      "wheel-up on a non-overflowing interrupted turn preserves tail-follow state",
    );
    ok(
      harness.container.querySelector(".transcript__jump-bottom") === null,
      "wheel-up on a non-overflowing interrupted turn does not expose a dead jump-bottom button",
    );
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Real overflow still exposes a working jump-bottom control ──────────────
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    await harness.render(turns(10), { running: false });
    await harness.settle();
    const el = harness.scrollElement();
    el.scrollTop = Math.max(0, el.scrollHeight - el.clientHeight - 400);
    el.dispatchEvent(new WheelEvent("wheel", { deltaY: -40, bubbles: true }));
    dispatchScroll(el);
    await harness.flush();

    const jump = harness.container.querySelector<HTMLButtonElement>(".transcript__jump-bottom");
    ok(jump != null, "a transcript with real overflow still exposes jump-bottom after wheel-up");
    jump?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await harness.settle();

    ok(
      el.scrollHeight - el.scrollTop - el.clientHeight <= 1,
      "jump-bottom still reaches the native bottom when overflow exists",
    );
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
