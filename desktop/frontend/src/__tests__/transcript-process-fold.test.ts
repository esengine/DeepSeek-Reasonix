// Run: tsx src/__tests__/transcript-process-fold.test.ts
//
// Fold behavior against the virtual row model: a collapsed fold is just its
// header row (no body rows mount), an expanded fold inserts its process rows
// into the list. Outside content (answers, warnings, steers, delivery cards)
// is never inside a `.turn-collapse__body` row.
//
// The harness installs process-wide globals (jsdom window, localStorage), so
// the auto-preference and expanded-preference phases run SEQUENTIALLY, each
// with its own harness.

import { createTranscriptHarness, type TranscriptHarness } from "./transcript-dom-harness";
import type { Item } from "../lib/useController";
import { act } from "react";

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

console.log("\ntranscript process fold");

function render(harness: TranscriptHarness, items: Item[], props: Record<string, unknown> = {}) {
  return harness.render(items, props);
}

function inOrder(a: Element | null | undefined, b: Element | null | undefined): boolean {
  return Boolean(a && b && (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING));
}

const warningTurn: Item[] = [
  { kind: "user", id: "u1", text: "inspect" },
  { kind: "assistant", id: "a1", text: "", reasoning: "first thought", streaming: false },
  { kind: "tool", id: "t1", name: "read_file", args: "{}", readOnly: true, status: "done", durationMs: 400 },
  { kind: "notice", id: "n1", level: "warn", text: "gateway warning" },
  { kind: "assistant", id: "a2", text: "", reasoning: "second thought", streaming: false },
  { kind: "tool", id: "t2", name: "bash", args: "{}", readOnly: false, status: "done", durationMs: 600 },
  { kind: "assistant", id: "a3", text: "final answer", reasoning: "final thought", streaming: false, workDurationMs: 24_000 },
];

// ── Phase 1: auto fold preference ────────────────────────────────────────────
{
  const harness = await createTranscriptHarness();
  const container = harness.container;
  try {
    await render(harness, warningTurn);
    {
      const warning = container.querySelector(".notice-line--warn");
      const finalAnswer = Array.from(container.querySelectorAll(".msg--assistant")).find((node) => node.textContent?.includes("final answer"));
      ok(container.querySelectorAll(".turn-collapse").length === 1, "renders one work fold for the turn");
      ok(warning && !warning.closest(".turn-collapse__body"), "warning remains visible without splitting the fold");
      ok(finalAnswer && !finalAnswer.closest(".turn-collapse__body"), "final answer renders outside the work fold");
      ok(!container.textContent?.includes("first thought"), "collapsed fold mounts no process rows");
    }

    await render(harness, [
      { kind: "user", id: "u-error", text: "finish" },
      { kind: "assistant", id: "a-error", text: "partial result", reasoning: "worked", streaming: false },
      { kind: "notice", id: "n-error", level: "warn", text: "turn stopped" },
    ]);
    {
      const errorAnswer = Array.from(container.querySelectorAll(".msg--assistant")).find((node) => node.textContent?.includes("partial result"));
      const trailingWarning = container.querySelector(".notice-line--warn");
      ok(inOrder(errorAnswer, trailingWarning), "warnings outside the fold preserve their order relative to the final answer");
    }

    // A delivery pause is a decision point addressed to the user: the status
    // card and its continue action must stay visible when the turn's process
    // fold closes, unlike plain info notices.
    await render(harness, [
      { kind: "user", id: "u-delivery", text: "ship it" },
      { kind: "assistant", id: "a-delivery", text: "", reasoning: "attempted delivery", streaming: false },
      {
        kind: "notice",
        id: "n-delivery",
        level: "info",
        variant: "delivery",
        title: "Task completion checks are not complete",
        text: "The response was generated, but verification and review still need to be completed.",
        detail: "final-answer readiness failed 3 times: missing verification",
        action: "continue_delivery",
      },
    ]);
    {
      const deliveryCard = container.querySelector(".notice-line--delivery");
      ok(deliveryCard && !deliveryCard.closest(".turn-collapse__body"), "delivery status card renders outside the work fold");
      ok(Boolean(deliveryCard?.querySelector("button")), "delivery status card keeps its continue action reachable");
    }

    const originalNow = Date.now;
    Date.now = () => 25_000;
    try {
      await render(harness, [
        { kind: "user", id: "u3", text: "run" },
        { kind: "assistant", id: "a6", text: "", reasoning: "working", streaming: false, workDurationMs: 5_000 },
      ], { running: true, turnStartAt: 1_000 });
      ok(container.querySelector(".turn-collapse__label")?.textContent === "Working 24s · 1 thoughts", "active turn stays Working and counts its process items");
    } finally {
      Date.now = originalNow;
    }

    await render(harness, [
      { kind: "user", id: "u4", text: "finish" },
      { kind: "assistant", id: "a7", text: "done", reasoning: "worked", streaming: false, workDurationMs: 24_000 },
    ]);
    ok(container.querySelector(".turn-collapse__label")?.textContent === "Worked 24s · 1 thoughts", "completed turn keeps the persisted wall-clock duration and counts");

    await render(harness, warningTurn);
    {
      const countsLabel = container.querySelector(".turn-collapse__label")?.textContent ?? "";
      ok(countsLabel.includes("2 tools") && countsLabel.includes("3 thoughts"), "fold label surfaces tool and thought counts");
    }

    // A turn whose fold is the only content (e.g. cancelled before any answer)
    // must not collapse into a bare label — nothing would remain visible.
    await render(harness, [
      { kind: "user", id: "u5", text: "cancelled" },
      { kind: "assistant", id: "a8", text: "", reasoning: "got cut off", streaming: false, workDurationMs: 3_000 },
    ]);
    ok(container.querySelector(".turn-collapse--open"), "fold with nothing outside stays expanded");
    ok(container.querySelector(".reasoning-summary")?.textContent === "got cut off", "an open fold shows a lightweight reasoning summary");
    ok(!container.querySelector(".turn-collapse__body .md"), "an open fold keeps reasoning Markdown lazy");

    await render(harness, [
      { kind: "user", id: "u6", text: "ask" },
      { kind: "assistant", id: "a9", text: "answered", reasoning: "quick", streaming: false, workDurationMs: 3_000 },
    ]);
    ok(!container.querySelector(".turn-collapse--open"), "fold with an answer outside starts collapsed");
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Phase 2: keep-expanded fold preference ────────────────────────────────────
{
  const harness = await createTranscriptHarness({ storage: { "reasonix-process-fold": "expanded" } });
  const container = harness.container;
  try {
    // Assistant content is model output addressed to the user — every message
    // with answer text stays outside the fold, not just the last one (#4092),
    // and process that ran AFTER an answer opens a new fold so the transcript
    // keeps the real timeline: plan → answer → tool work → answer.
    await render(harness, [
      { kind: "user", id: "u2", text: "continue" },
      { kind: "assistant", id: "a4", text: "I will inspect the files", reasoning: "plan", streaming: false },
      { kind: "tool", id: "t3", name: "read_file", args: "{}", readOnly: true, status: "done" },
      { kind: "assistant", id: "a5", text: "all done", reasoning: "verify", streaming: false },
    ]);
    {
      const intermediate = Array.from(container.querySelectorAll(".msg--assistant")).find((node) => node.textContent?.includes("I will inspect the files"));
      const final = Array.from(container.querySelectorAll(".msg--assistant")).find((node) => node.textContent?.includes("all done"));
      const folds = Array.from(container.querySelectorAll(".turn-collapse"));
      ok(folds.length === 2, "work after an intermediate answer opens a second fold");
      ok(intermediate && !intermediate.closest(".turn-collapse__body"), "intermediate assistant text renders outside the work fold");
      ok(final && !final.closest(".turn-collapse__body"), "final assistant answer renders outside the work fold");
      const foldBodies = folds.map((fold) => {
        // Body rows follow the header row until the next non-body row.
        let text = "";
        let sibling = fold.closest(".transcript__row")?.nextElementSibling ?? null;
        while (sibling?.querySelector(".turn-collapse__body")) {
          text += sibling.textContent ?? "";
          sibling = sibling.nextElementSibling;
        }
        return text;
      });
      ok(
        inOrder(folds[0], intermediate) && inOrder(intermediate, folds[1]) && inOrder(folds[1], final),
        "folds and answers keep the turn's real timeline",
      );
      ok(foldBodies[0]?.includes("plan") && !foldBodies[0]?.includes("verify"), "first fold holds only the work before the first answer");
      ok(foldBodies[1]?.includes("verify") && foldBodies[1]?.includes("read_file"), "second fold holds the work after the first answer");
      ok(folds[0]?.querySelector(".turn-collapse__label")?.textContent === "1 thoughts", "earlier folds carry a counts-only label");
      ok(folds[1]?.querySelector(".turn-collapse__label")?.textContent?.startsWith("Worked"), "the closing fold carries the turn's work label");
    }

    // A mid-turn steer is the user's own message (#6238): it renders on the
    // user side, outside the fold, at its real position — work that followed
    // the steer folds after it, not ahead of it. Ordinary info notices keep
    // folding.
    await render(harness, [
      { kind: "user", id: "u-steer", text: "start" },
      { kind: "assistant", id: "a-steer-1", text: "", reasoning: "thinking", streaming: false },
      { kind: "notice", id: "s1", level: "info", text: "↪ use plan B instead" },
      { kind: "notice", id: "i1", level: "info", text: "plain info notice" },
      { kind: "assistant", id: "a-steer-2", text: "done via plan B", reasoning: "", streaming: false },
    ]);
    {
      const steer = container.querySelector(".steer-line");
      ok(steer && !steer.closest(".turn-collapse__body"), "steer notice renders outside the work fold");
      ok(steer?.textContent?.includes("use plan B instead"), "steer bubble carries the user's guidance text");
      const plainInfo = Array.from(container.querySelectorAll(".notice-line")).find((node) => node.textContent?.includes("plain info notice"));
      ok(plainInfo && plainInfo.closest(".turn-collapse__body"), "plain info notices keep folding");
      const steerFolds = Array.from(container.querySelectorAll(".turn-collapse"));
      ok(
        steerFolds.length === 2 && inOrder(steerFolds[0], steer) && inOrder(steer, steerFolds[1]),
        "work after the steer folds after it, keeping the steer's position",
      );
    }

    // settings.processFold = expanded keeps completed folds open (#4233, #2278).
    await render(harness, [
      { kind: "user", id: "u7", text: "ask" },
      { kind: "assistant", id: "a10", text: "answered", reasoning: "quick", streaming: false, workDurationMs: 3_000 },
    ]);
    ok(container.querySelector(".turn-collapse--open"), "keep-expanded preference leaves the fold open");

    // Each reasoning segment starts as a one-line summary. Full Markdown only
    // mounts for the selected virtual row after the user expands it (#6340).
    await render(harness, [
      { kind: "user", id: "u-segment", text: "inspect" },
      { kind: "assistant", id: "a-segment", text: "", reasoning: "**first thought**\n\n- tail detail", streaming: false },
    ]);
    {
      const segmentHeads = container.querySelectorAll("button.turn-collapse__reasoning-head");
      ok(segmentHeads.length === 1, "every reasoning segment gets its own toggle");
      ok(segmentHeads[0]?.getAttribute("aria-expanded") === "false", "reasoning segments default to collapsed");
      const summary = container.querySelector<HTMLButtonElement>(".reasoning-summary");
      ok(summary?.textContent === "**first thought**", "collapsed reasoning renders a plain-text summary");
      ok(!container.querySelector(".turn-collapse__body .md"), "collapsed reasoning mounts no Markdown");

      await act(async () => {
        summary?.dispatchEvent(new harness.dom.window.MouseEvent("click", { bubbles: true }));
      });
      for (let i = 0; i < 20 && !container.querySelector(".turn-collapse__body .md strong"); i += 1) await harness.flush();
      ok(container.querySelector(".turn-collapse__body .md strong")?.textContent === "first thought", "clicking the summary mounts full Markdown");
      ok(container.querySelector(".turn-collapse__body .md li")?.textContent === "tail detail", "expanded reasoning renders Markdown lists");

      await act(async () => {
        container.querySelector(".turn-collapse__reasoning-head")?.dispatchEvent(new harness.dom.window.MouseEvent("click", { bubbles: true }));
      });
      await harness.flush();
      ok(!container.querySelector(".turn-collapse__body .md"), "clicking the segment head returns to the summary");
    }
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Phase 3: expanded reasoning pins its parent process fold ─────────────────
{
  const harness = await createTranscriptHarness({ reasoningDisplayMode: "expanded" });
  const container = harness.container;
  try {
    const runningItems: Item[] = [
      { kind: "user", id: "u-expanded-parent", text: "inspect" },
      {
        kind: "assistant",
        id: "a-expanded-parent",
        text: "done",
        reasoning: "completed reasoning should remain visible",
        streaming: true,
        reasoningComplete: false,
        workDurationMs: 1_000,
      },
    ];
    await render(harness, runningItems, { running: true });
    await harness.waitFor(
      () => Boolean(
        container.querySelector(".turn-collapse--open")
          && container.querySelector(".turn-collapse__reasoning-phase--open .reasoning__stream-text"),
      ),
      "expanded live reasoning",
    );
    ok(Boolean(container.querySelector(".turn-collapse--open")), "expanded reasoning starts with its running parent fold open");
    ok(
      container.querySelector(".turn-collapse__reasoning-phase--open .reasoning__stream-text")?.textContent?.includes("completed reasoning should remain visible") ?? false,
      "expanded running reasoning streams its full body as plain text",
    );

    const completedItems: Item[] = runningItems.map((item) => item.kind === "assistant"
      ? { ...item, streaming: false, reasoningComplete: true }
      : item);
    await render(harness, completedItems, { running: false });
    await harness.waitFor(
      () => Boolean(
        container.querySelector(".turn-collapse--open")
          && container.querySelector(".turn-collapse__reasoning-phase--open .md"),
      ),
      "expanded completed reasoning",
    );
    ok(Boolean(container.querySelector(".turn-collapse--open")), "expanded reasoning keeps its parent fold open after completion");
    ok(Boolean(container.querySelector(".turn-collapse__reasoning-phase--open .md")), "expanded completed reasoning remains visible");

    await act(async () => {
      container.querySelector<HTMLButtonElement>(".turn-collapse > .reasoning__head")?.click();
    });
    await harness.flush();
    ok(!container.querySelector(".turn-collapse--open"), "manual parent collapse still wins in expanded reasoning mode");
    ok(!container.querySelector(".turn-collapse__body"), "manual parent collapse unmounts the expanded reasoning body");

    await render(harness, completedItems, { running: false });
    ok(!container.querySelector(".turn-collapse--open"), "completed rerenders preserve a manual parent collapse");
    await harness.settle();
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// A completed reasoning fold restored from history has no live transition to
// open it, so the expanded-mode default must pin it on first reconciliation.
{
  const harness = await createTranscriptHarness({ reasoningDisplayMode: "expanded" });
  const container = harness.container;
  try {
    await render(harness, [
      { kind: "user", id: "u-expanded-history", text: "inspect" },
      {
        kind: "assistant",
        id: "a-expanded-history",
        text: "done",
        reasoning: "restored completed reasoning",
        streaming: false,
        reasoningComplete: true,
        workDurationMs: 1_000,
      },
    ]);
    await harness.waitFor(
      () => Boolean(
        container.querySelector(".turn-collapse--open")
          && container.querySelector(".turn-collapse__reasoning-phase--open .md"),
      ),
      "expanded reasoning restored from history",
    );
    ok(Boolean(container.querySelector(".turn-collapse--open")), "expanded history restores the parent reasoning fold open");
    ok(Boolean(container.querySelector(".turn-collapse__reasoning-phase--open .md")), "expanded history restores completed reasoning visible");
    await harness.settle();
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

// ── Phase 4: summaries disabled ─────────────────────────────────────────────
{
  const harness = await createTranscriptHarness({
    storage: { "reasonix-process-fold": "expanded", "reasonix-reasoning-summary": "0" },
  });
  try {
    await render(harness, [
      { kind: "user", id: "u-no-summary", text: "inspect" },
      { kind: "assistant", id: "a-no-summary", text: "", reasoning: "hidden preview", streaming: false },
    ]);
    ok(!harness.container.querySelector(".reasoning-summary"), "disabling reasoning summaries hides inline previews");
    ok(!harness.container.querySelector(".turn-collapse__body .md"), "disabled previews keep Markdown lazy");
    ok(Boolean(harness.container.querySelector(".turn-collapse__reasoning-head")), "the reasoning toggle remains accessible without a summary");
  } finally {
    await harness.unmount();
    await harness.close();
  }
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
