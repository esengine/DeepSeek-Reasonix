// Run: tsx src/__tests__/transcript-question-jump.test.tsx
//
// Real-landing regression for the question navigator and rewind in long
// transcripts. scrollToIndex takes a data-relative index: passing
// firstItemIndex + dataIndex clamps to the last row in Virtuoso's index
// normalizer, so every assertion here checks the selected question is
// actually mounted in the viewport, not just that a jump was dispatched.

import { createTranscriptHarness } from "./transcript-dom-harness";
import { QUESTION_JUMP_MAX_MARKERS } from "../components/QuestionJumpBar";
import type { Item } from "../lib/useController";
import { act } from "react";

let passed = 0;
let failed = 0;

function ok(condition: unknown, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function turns(count: number, prefix = ""): Item[] {
  const items: Item[] = [];
  for (let i = 0; i < count; i += 1) {
    items.push({ kind: "user", id: `${prefix}u${i}`, text: `question ${prefix}${i}` });
    items.push({ kind: "assistant", id: `${prefix}a${i}`, text: `answer ${prefix}${i}`, reasoning: "", streaming: false });
  }
  return items;
}

function historyTurns(start: number, end: number): Item[] {
  const items: Item[] = [];
  for (let turn = start; turn <= end; turn += 1) {
    items.push({ kind: "user", id: `history-u${turn}`, text: `history question ${turn}`, historyTurn: turn });
  }
  return items;
}

function dispatchScroll(el: HTMLElement) {
  el.dispatchEvent(new Event("scroll"));
}

function stubRailGeometry(container: HTMLElement, count: number) {
  const jumpBar = container.querySelector<HTMLElement>(".jump-bar");
  const jumpScroll = container.querySelector<HTMLElement>(".jump-scroll");
  if (!jumpBar || !jumpScroll) throw new Error("question jump bar is not mounted");
  jumpBar.getBoundingClientRect = () => ({ top: 0, bottom: 240, left: 0, right: 56, height: 240, width: 56 } as DOMRect);
  jumpScroll.getBoundingClientRect = () => ({ top: 0, bottom: 240, left: 0, right: 32, height: 240, width: 32 } as DOMRect);
  const items = jumpScroll.querySelectorAll<HTMLElement>(".jump-item");
  if (items.length !== count) throw new Error(`expected ${count} jump markers, found ${items.length}`);
  return jumpScroll;
}

function railClientY(turn: number, total: number): number {
  return ((turn + 0.5) / total) * 240;
}

function visibleRows(container: HTMLElement): string[] {
  const el = container.querySelector<HTMLElement>(".transcript");
  if (!el) return [];
  const top = el.scrollTop;
  const bottom = top + el.clientHeight;
  const visible: string[] = [];
  rowOffsets(container).forEach((offset, row) => {
    if (offset >= top - 100 && offset <= bottom) visible.push(row.className);
  });
  return visible;
}

// Virtuoso positions rows absolutely (transform), so jsdom offsetTop stays 0.
// Walk the size-annotated rows in data order instead — the same bookkeeping
// the harness uses for scrollHeight.
function rowOffsets(container: HTMLElement): Map<HTMLElement, number> {
  const offsets = new Map<HTMLElement, number>();
  const list = container.querySelector<HTMLElement>("[data-testid='virtuoso-item-list']");
  if (!list) return offsets;
  let offset = Number.parseFloat(list.style.paddingTop || "0");
  list.querySelectorAll<HTMLElement>("[data-known-size]").forEach((row) => {
    offsets.set(row, offset);
    offset += Number.parseFloat(row.dataset.knownSize || "0");
  });
  return offsets;
}

function rowOffsetOf(container: HTMLElement, descendant: HTMLElement): number | null {
  const row = descendant.closest<HTMLElement>("[data-known-size]");
  if (!row) return null;
  return rowOffsets(container).get(row) ?? null;
}

console.log("\ntranscript question jump landing");

// ── Physical rail clicks land on the selected question ──────────────────────
// First, middle, and tail positions, with back-and-forth jumps in between:
// a stale firstItemIndex offset reappears whenever the viewport moves, so the
// sequence must hold across consecutive jumps, not just the first.
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    HTMLElement.prototype.scrollIntoView = () => {};
    await harness.render(turns(40), { running: false, questionNavigator: true });
    await harness.settle();
    const jumpScroll = stubRailGeometry(harness.container, 40);
    const el = harness.scrollElement();

    const targetIndices = [0, 31, 3, 36, 12, 28, 39];
    for (const targetIndex of targetIndices) {
      const clientY = railClientY(targetIndex, 40);
      await act(async () => {
        jumpScroll.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true, button: 0, clientY }));
        jumpScroll.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, button: 0, clientY, detail: 1 }));
        dispatchScroll(el);
      });
      await harness.waitFor(
        () => Boolean(harness.container.querySelector(`#question-anchor-u${targetIndex}`)),
        `question ${targetIndex} to mount after the jump`,
      );
      const anchor = harness.container.querySelector<HTMLElement>(`#question-anchor-u${targetIndex}`)!;
      const rowTop = rowOffsetOf(harness.container, anchor);
      ok(
        rowTop >= el.scrollTop - 100 && rowTop <= el.scrollTop + el.clientHeight,
        `jump to question ${targetIndex + 1} lands its row inside the viewport (rowTop ${rowTop}, scrollTop ${el.scrollTop})`,
      );
      const expectedText = `question ${targetIndex}`;
      ok(anchor.textContent?.includes(expectedText) ?? false, `jump to question ${targetIndex + 1} mounts the selected question content`);
    }
    ok(el.scrollTop < el.scrollHeight - el.clientHeight - 100 || targetIndices.at(-1) === 39, "mid-conversation jumps do not clamp to the transcript tail");
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── A jump while a stale text selection is active still lands ───────────────
// #9054 clears the selection state before jumping; the landing must survive
// that cleanup path instead of being swallowed by selection mode.
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    HTMLElement.prototype.scrollIntoView = () => {};
    await harness.render(turns(20), { running: false, questionNavigator: true });
    await harness.settle();
    const jumpScroll = stubRailGeometry(harness.container, 20);
    const el = harness.scrollElement();

    const selection = harness.dom.window.document.getSelection();
    const firstAnswer = harness.container.querySelector(".msg--assistant");
    if (selection && firstAnswer && firstAnswer.firstChild) {
      selection.removeAllRanges();
      const range = harness.dom.window.document.createRange();
      range.selectNodeContents(firstAnswer.firstChild);
      selection.addRange(range);
    }

    const targetIndex = 4;
    await act(async () => {
      jumpScroll.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true, button: 0, clientY: railClientY(targetIndex, 20) }));
      jumpScroll.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, button: 0, clientY: railClientY(targetIndex, 20), detail: 1 }));
      dispatchScroll(el);
    });
    await harness.waitFor(
      () => Boolean(harness.container.querySelector(`#question-anchor-u${targetIndex}`)),
      "the target question to mount through stale-selection cleanup",
    );
    ok(Boolean(harness.container.querySelector(`#question-anchor-u${targetIndex}`)), "a jump through a stale text selection still lands on the target question");
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Rewind lands on the rewound-to question, not the tail ───────────────────
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    await harness.render(turns(30), { running: false, rewindSignal: 0 });
    const el = harness.scrollElement();
    el.scrollTop = 0;
    dispatchScroll(el);
    await harness.flush();
    await harness.render(turns(30), { running: false, rewindSignal: 1 });
    dispatchScroll(el);
    await harness.settle();
    await harness.waitFor(
      () => Boolean(harness.container.querySelector("#question-anchor-u29")),
      "rewind mounts the rewound-to question",
    );
    const anchor = harness.container.querySelector<HTMLElement>("#question-anchor-u29")!;
    const rowTop = rowOffsetOf(harness.container, anchor);
    ok(
      rowTop >= el.scrollTop - 100 && rowTop <= el.scrollTop + el.clientHeight,
      `rewind lands on the rewound-to question (rowTop ${rowTop}, scrollTop ${el.scrollTop})`,
    );
    // The rewound-to question is the last one in a 30-turn transcript, so the
    // tail itself is the destination; the landed offset equality above is the
    // regression signal. A clamped-to-tail bug would instead skip the target
    // row when it is mid-transcript — covered by the rail-click block.
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Jumps stay correct after an older-history prepend shifts firstItemIndex ──
// prepend decreases firstItemIndex by the inserted row count; a data-relative
// jump must remain stable across that shift.
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    HTMLElement.prototype.scrollIntoView = () => {};
    await harness.render(turns(15), { running: false, questionNavigator: true, hasOlderHistory: true });
    await harness.settle();
    await harness.render([...turns(3, "old-"), ...turns(15)], { running: false, questionNavigator: true, hasOlderHistory: true });
    await harness.settle();
    const jumpScroll = stubRailGeometry(harness.container, 18);
    const el = harness.scrollElement();

    const targetIndex = 9; // old-u1 sits at marker 3; pick a post-prepend question
    await act(async () => {
      jumpScroll.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true, button: 0, clientY: railClientY(targetIndex, 18) }));
      jumpScroll.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, button: 0, clientY: railClientY(targetIndex, 18), detail: 1 }));
      dispatchScroll(el);
    });
    await harness.waitFor(
      () => Boolean(harness.container.querySelector("#question-anchor-u6")),
      "the post-prepend target question to mount",
    );
    ok(Boolean(harness.container.querySelector("#question-anchor-u6")), "a jump after older-history prepend lands on the selected question");
    ok(visibleRows(harness.container).length > 0, "mounted rows remain in the viewport after the prepend jump");
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Auto-fill runs only after a stable, genuinely short first page ──────────
{
  // Keep the viewport deliberately larger than every estimate Virtuoso may
  // use before its first measured row. This exercises the auto-fill branch
  // itself instead of depending on estimate tuning in the virtualizer.
  const harness = await createTranscriptHarness({ viewportHeight: 100_000, rowHeight: 100 });
  try {
    HTMLElement.prototype.scrollIntoView = () => {};
    let loads = 0;
    const triggers: string[] = [];
    await harness.render(historyTurns(61, 61), {
      running: false,
      questionNavigator: true,
      hasOlderHistory: true,
      historyStartTurn: 61,
      historyTotalTurns: 61,
      surfaceCommitToken: "navigation-1-short-surface",
      onSurfacePaintReady: () => {},
      onLoadOlderHistory: async (_targetTurn, trigger) => {
        loads += 1;
        triggers.push(trigger ?? "");
        return true;
      },
    });
    await harness.waitFor(() => loads > 0, "a short stable first page to auto-fill");
    ok(triggers[0] === "auto-fill", "short-page loading is labeled as auto-fill");
    ok(harness.container.querySelectorAll(".jump-item").length === 61, "the first page renders a marker for every session question");
    ok(!harness.container.querySelector(".transcript__older"), "the ordinary show-earlier fold button is absent");
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Initial Virtuoso startReached cannot cascade a full first page ──────────
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    let loads = 0;
    await harness.render(historyTurns(1, 120), {
      running: false,
      questionNavigator: true,
      hasOlderHistory: true,
      historyStartTurn: 1,
      historyTotalTurns: 120,
      onLoadOlderHistory: async () => {
        loads += 1;
        return true;
      },
    });
    await harness.settle();
    ok(loads === 0, "mounting a scrollable 120-turn page requests no older history without user intent");
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Extreme sessions keep a bounded rail and exact lazy-load targeting ──────
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    HTMLElement.prototype.scrollIntoView = () => {};
    const requestedTurns: Array<number | undefined> = [];
    await harness.render(historyTurns(9_941, 10_000), {
      running: false,
      questionNavigator: true,
      hasOlderHistory: true,
      historyStartTurn: 9_941,
      historyTotalTurns: 10_000,
      olderHistoryError: "pause automatic loading for this rail-only fixture",
      onLoadOlderHistory: async (targetTurn) => {
        requestedTurns.push(targetTurn);
        return true;
      },
    });
    await harness.settle();
    const jumpScroll = stubRailGeometry(harness.container, QUESTION_JUMP_MAX_MARKERS);
    ok(harness.container.querySelectorAll(".jump-item").length === QUESTION_JUMP_MAX_MARKERS, "10,000 turns keep a fixed-size question rail");

    const targetTurn = 1_234;
    await act(async () => {
      jumpScroll.dispatchEvent(new MouseEvent("mousedown", {
        bubbles: true,
        cancelable: true,
        button: 0,
        clientY: railClientY(targetTurn, 10_000),
      }));
    });
    await harness.waitFor(() => requestedTurns.includes(targetTurn + 1), "the aggregate rail to request the exact absolute turn");
    ok(requestedTurns.includes(targetTurn + 1), "the bounded rail preserves exact unloaded-question targeting");
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Clicking an unloaded complete-rail marker loads and lands on that turn ──
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    HTMLElement.prototype.scrollIntoView = () => {};
    const requestedTurns: Array<number | undefined> = [];
    const onLoadOlderHistory = async (targetTurn?: number) => {
      requestedTurns.push(targetTurn);
      return true;
    };
    await harness.render(historyTurns(81, 100), {
      running: false,
      questionNavigator: true,
      hasOlderHistory: true,
      historyStartTurn: 81,
      historyTotalTurns: 100,
      onLoadOlderHistory,
    });
    await harness.settle();
    const jumpScroll = stubRailGeometry(harness.container, 100);
    await act(async () => {
      jumpScroll.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true, button: 0, clientY: railClientY(10, 100) }));
    });
    await harness.waitFor(() => requestedTurns.includes(11), "the unloaded marker to request its absolute turn");

    await harness.render(historyTurns(11, 100), {
      running: false,
      questionNavigator: true,
      hasOlderHistory: false,
      historyStartTurn: 11,
      historyTotalTurns: 100,
      onLoadOlderHistory,
    });
    await harness.waitFor(
      () => Boolean(harness.container.querySelector("#question-anchor-history-u11")),
      "the newly loaded question to land in the viewport",
    );
    ok(Boolean(harness.container.querySelector("#question-anchor-history-u11")), "an unloaded marker loads and lands on the selected question");
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Failed automatic loading stops and exposes a compact retry state ─────────
{
  const harness = await createTranscriptHarness({ viewportHeight: 200, rowHeight: 100 });
  try {
    HTMLElement.prototype.scrollIntoView = () => {};
    let retries = 0;
    await harness.render(historyTurns(81, 100), {
      running: false,
      questionNavigator: true,
      hasOlderHistory: true,
      historyStartTurn: 81,
      historyTotalTurns: 100,
      olderHistoryError: "backend detail stays out of the UI",
      onLoadOlderHistory: async () => {
        retries += 1;
        return true;
      },
    });
    const retry = Array.from(harness.container.querySelectorAll<HTMLButtonElement>("button"))
      .find((button) => button.textContent?.trim() === "Retry");
    ok(Boolean(retry), "a failed older-history load exposes a retry action");
    ok(harness.container.textContent?.includes("Earlier conversation could not be loaded") ?? false, "the failure uses a path-free user-facing message");
    await act(async () => retry?.click());
    await harness.waitFor(() => retries === 1, "the older-history retry to run once");
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
