// Run: tsx src/__tests__/transcript-process-fold.test.ts

import { JSDOM } from "jsdom";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createServer, type ViteDevServer } from "vite";
import type { Item } from "../lib/useController";

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

let displayMode = "standard";
let processFoldPref = "auto";
let reasoningSummaryPref = true;
Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: {
    getItem(key: string) {
      if (key === "reasonix-display-mode") return displayMode;
      if (key === "reasonix-process-fold") return processFoldPref;
      if (key === "reasonix-reasoning-summary") return reasoningSummaryPref ? "1" : "0";
      return null;
    },
    setItem() {},
    removeItem() {},
    clear() {},
    key() { return null; },
    length: 0,
  },
});

let server: ViteDevServer | undefined;
try {
  server = await createServer({
    appType: "custom",
    logLevel: "silent",
    server: { middlewareMode: true },
  });
  const { Transcript } = await server.ssrLoadModule("/src/components/Transcript.tsx");
  const { LocaleProvider } = await server.ssrLoadModule("/src/lib/i18n.tsx");

  function render(items: Item[], options: { mode?: "standard" | "compact"; running?: boolean; turnStartAt?: number; foldPref?: "auto" | "expanded"; summaryEnabled?: boolean } = {}) {
    displayMode = options.mode ?? "standard";
    processFoldPref = options.foldPref ?? "auto";
    reasoningSummaryPref = options.summaryEnabled ?? true;
    const markup = renderToStaticMarkup(
      React.createElement(
        LocaleProvider,
        null,
        React.createElement(Transcript, {
          items,
          onPrompt: () => {},
          questionNavigator: false,
          running: options.running ?? false,
          turnStartAt: options.turnStartAt,
        }),
      ),
    );
    return new JSDOM(markup).window.document;
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

  for (const mode of ["standard", "compact"] as const) {
    const doc = render(warningTurn, { mode });
    const warning = doc.querySelector(".notice-line--warn");
    const finalAnswer = Array.from(doc.querySelectorAll(".msg--assistant")).find((node) => node.textContent?.includes("final answer"));
    ok(doc.querySelectorAll(".turn-collapse").length === 1, `${mode} mode renders one work fold for the turn`);
    ok(warning && !warning.closest(".turn-collapse"), `${mode} warning remains visible without splitting the fold`);
    ok(finalAnswer && !finalAnswer.closest(".turn-collapse"), `${mode} final answer renders outside the work fold`);
  }

  // Assistant content is model output addressed to the user — every message
  // with answer text stays outside the fold, not just the last one (#4092),
  // and process that ran AFTER an answer opens a new fold so the transcript
  // keeps the real timeline: plan → answer → tool work → answer.
  const intermediateDoc = render([
    { kind: "user", id: "u2", text: "continue" },
    { kind: "assistant", id: "a4", text: "I will inspect the files", reasoning: "plan", streaming: false },
    { kind: "tool", id: "t3", name: "read_file", args: "{}", readOnly: true, status: "done" },
    { kind: "assistant", id: "a5", text: "all done", reasoning: "verify", streaming: false },
  ]);
  const intermediate = Array.from(intermediateDoc.querySelectorAll(".msg--assistant")).find((node) => node.textContent?.includes("I will inspect the files"));
  const final = Array.from(intermediateDoc.querySelectorAll(".msg--assistant")).find((node) => node.textContent?.includes("all done"));
  const folds = Array.from(intermediateDoc.querySelectorAll(".turn-collapse"));
  ok(folds.length === 2, "work after an intermediate answer opens a second fold");
  ok(intermediate && !intermediate.closest(".turn-collapse"), "intermediate assistant text renders outside the work fold");
  ok(final && !final.closest(".turn-collapse"), "final assistant answer renders outside the work fold");
  const inOrder = (a: Element | null | undefined, b: Element | null | undefined) => Boolean(
    a && b && (a.compareDocumentPosition(b) & intermediateDoc.defaultView!.Node.DOCUMENT_POSITION_FOLLOWING),
  );
  ok(
    inOrder(folds[0], intermediate) && inOrder(intermediate, folds[1]) && inOrder(folds[1], final),
    "folds and answers keep the turn's real timeline",
  );
  ok(folds[0]?.textContent?.includes("plan") && !folds[0]?.textContent?.includes("verify"), "first fold holds only the work before the first answer");
  ok(folds[1]?.textContent?.includes("verify") && folds[1]?.textContent?.includes("read_file"), "second fold holds the work after the first answer");
  ok(folds[0]?.querySelector(".turn-collapse__label")?.textContent === "1 thoughts", "earlier folds carry a counts-only label");
  ok(folds[1]?.querySelector(".turn-collapse__label")?.textContent?.startsWith("Worked"), "the closing fold carries the turn's work label");

  // A mid-turn steer is the user's own message (#6238): it renders on the
  // user side, outside the fold, at its real position — work that followed
  // the steer folds after it, not ahead of it. Ordinary info notices keep
  // folding.
  const steerDoc = render([
    { kind: "user", id: "u-steer", text: "start" },
    { kind: "assistant", id: "a-steer-1", text: "", reasoning: "thinking", streaming: false },
    { kind: "notice", id: "s1", level: "info", text: "↪ use plan B instead" },
    { kind: "notice", id: "i1", level: "info", text: "plain info notice" },
    { kind: "assistant", id: "a-steer-2", text: "done via plan B", reasoning: "", streaming: false },
  ]);
  const steer = steerDoc.querySelector(".steer-line");
  ok(steer && !steer.closest(".turn-collapse"), "steer notice renders outside the work fold");
  ok(steer?.textContent?.includes("use plan B instead"), "steer bubble carries the user's guidance text");
  const plainInfo = Array.from(steerDoc.querySelectorAll(".notice-line")).find((node) => node.textContent?.includes("plain info notice"));
  ok(plainInfo && plainInfo.closest(".turn-collapse"), "plain info notices keep folding");
  const steerFolds = Array.from(steerDoc.querySelectorAll(".turn-collapse"));
  const steerInOrder = (a: Element | null | undefined, b: Element | null | undefined) => Boolean(
    a && b && (a.compareDocumentPosition(b) & steerDoc.defaultView!.Node.DOCUMENT_POSITION_FOLLOWING),
  );
  ok(
    steerFolds.length === 2 && steerInOrder(steerFolds[0], steer) && steerInOrder(steer, steerFolds[1]),
    "work after the steer folds after it, keeping the steer's position",
  );

  const errorDoc = render([
    { kind: "user", id: "u-error", text: "finish" },
    { kind: "assistant", id: "a-error", text: "partial result", reasoning: "worked", streaming: false },
    { kind: "notice", id: "n-error", level: "warn", text: "turn stopped" },
  ]);
  const errorAnswer = Array.from(errorDoc.querySelectorAll(".msg--assistant")).find((node) => node.textContent?.includes("partial result"));
  const trailingWarning = errorDoc.querySelector(".notice-line--warn");
  const followsAnswer = Boolean(
    errorAnswer &&
    trailingWarning &&
    (errorAnswer.compareDocumentPosition(trailingWarning) & errorDoc.defaultView!.Node.DOCUMENT_POSITION_FOLLOWING),
  );
  ok(followsAnswer, "warnings outside the fold preserve their order relative to the final answer");

  // A delivery pause is a decision point addressed to the user: the status
  // card and its continue action must stay visible when the turn's process
  // fold closes, unlike plain info notices.
  const deliveryDoc = render([
    { kind: "user", id: "u-delivery", text: "ship it" },
    { kind: "assistant", id: "a-delivery", text: "", reasoning: "attempted delivery", streaming: false },
    {
      kind: "notice",
      id: "n-delivery",
      level: "info",
      variant: "delivery",
      title: "Delivery checks are not complete",
      text: "The response was generated, but verification and review still need to be completed.",
      detail: "final-answer readiness failed 3 times: missing verification",
      action: "continue_delivery",
    },
  ]);
  const deliveryCard = deliveryDoc.querySelector(".notice-line--delivery");
  ok(deliveryCard && !deliveryCard.closest(".turn-collapse"), "delivery status card renders outside the work fold");
  ok(Boolean(deliveryCard?.querySelector("button")), "delivery status card keeps its continue action reachable");

  const originalNow = Date.now;
  Date.now = () => 25_000;
  try {
    const runningDoc = render([
      { kind: "user", id: "u3", text: "run" },
      { kind: "assistant", id: "a6", text: "", reasoning: "working", streaming: false, workDurationMs: 5_000 },
    ], { running: true, turnStartAt: 1_000 });
    ok(runningDoc.querySelector(".turn-collapse__label")?.textContent === "Working 24s · 1 thoughts", "active turn stays Working and counts its process items");
  } finally {
    Date.now = originalNow;
  }

  const completedDoc = render([
    { kind: "user", id: "u4", text: "finish" },
    { kind: "assistant", id: "a7", text: "done", reasoning: "worked", streaming: false, workDurationMs: 24_000 },
  ]);
  ok(completedDoc.querySelector(".turn-collapse__label")?.textContent === "Worked 24s · 1 thoughts", "completed turn keeps the persisted wall-clock duration and counts");

  const countsDoc = render(warningTurn);
  const countsLabel = countsDoc.querySelector(".turn-collapse__label")?.textContent ?? "";
  ok(countsLabel.includes("2 tools") && countsLabel.includes("3 thoughts"), "fold label surfaces tool and thought counts");

  // A turn whose fold is the only content (e.g. cancelled before any answer)
  // must not collapse into a bare label — nothing would remain visible.
  const aloneDoc = render([
    { kind: "user", id: "u5", text: "cancelled" },
    { kind: "assistant", id: "a8", text: "", reasoning: "got cut off", streaming: false, workDurationMs: 3_000 },
  ]);
  ok(aloneDoc.querySelector(".turn-collapse--open"), "fold with nothing outside stays expanded");
  const aloneSummary = aloneDoc.querySelector(".turn-collapse--open .reasoning-summary");
  ok(aloneSummary?.textContent === "got cut off", "an open fold still shows reasoning as a summary by default");
  ok(!aloneDoc.querySelector(".turn-collapse--open .md"), "an open fold mounts no reasoning Markdown until the segment expands");
  const noSummaryDoc = render([
    { kind: "user", id: "u5b", text: "cancelled" },
    { kind: "assistant", id: "a8b", text: "", reasoning: "got cut off", streaming: false, workDurationMs: 3_000 },
  ], { summaryEnabled: false });
  ok(!noSummaryDoc.querySelector(".turn-collapse .reasoning-summary"), "disabling reasoning summaries hides inline fold previews");
  ok(!noSummaryDoc.querySelector(".turn-collapse .md"), "disabled inline previews keep Markdown lazy");
  const answeredDoc = render([
    { kind: "user", id: "u6", text: "ask" },
    { kind: "assistant", id: "a9", text: "answered", reasoning: "quick", streaming: false, workDurationMs: 3_000 },
  ]);
  ok(!answeredDoc.querySelector(".turn-collapse--open"), "fold with an answer outside starts collapsed");

  // settings.processFold = expanded keeps completed folds open (#4233, #2278).
  const expandedDoc = render([
    { kind: "user", id: "u7", text: "ask" },
    { kind: "assistant", id: "a10", text: "answered", reasoning: "quick", streaming: false, workDurationMs: 3_000 },
  ], { foldPref: "expanded" });
  ok(expandedDoc.querySelector(".turn-collapse--open"), "keep-expanded preference leaves the fold open");

  // Each reasoning segment inside the fold is independently collapsible (#6340)
  // and now defaults to a one-line plain-text summary; the full Markdown body
  // only mounts after the segment is expanded.
  const segmentDoc = render(warningTurn);
  const segmentHeads = segmentDoc.querySelectorAll("button.turn-collapse__reasoning-head");
  ok(segmentHeads.length === 3, "every reasoning segment gets its own toggle");
  ok(Array.from(segmentHeads).every((head) => head.getAttribute("aria-expanded") === "false"), "reasoning segments default to collapsed");
  const segmentSummaries = Array.from(segmentDoc.querySelectorAll(".turn-collapse .reasoning-summary"));
  ok(segmentSummaries.length === 3, "collapsed reasoning segments render summaries");
  ok(segmentSummaries[0]?.textContent === "first thought", "segment summary is the first non-blank line");
  ok(segmentDoc.querySelectorAll(".turn-collapse .md").length === 0, "unexpanded segments mount no Markdown");
  ok(segmentDoc.querySelectorAll(".turn-collapse .turn-collapse__inline-reasoning").length === 0, "a closed fold mounts no reasoning Markdown body");

  // Interactive: clicking a segment summary or head mounts the full Markdown.
  const liveDom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = liveDom.window as unknown as Window & typeof globalThis;
  globalThis.document = liveDom.window.document;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: liveDom.window.navigator });
  globalThis.Node = liveDom.window.Node;
  globalThis.Element = liveDom.window.Element;
  globalThis.HTMLElement = liveDom.window.HTMLElement;
  globalThis.Event = liveDom.window.Event;
  globalThis.MouseEvent = liveDom.window.MouseEvent;
  globalThis.requestAnimationFrame = liveDom.window.requestAnimationFrame.bind(liveDom.window);
  globalThis.cancelAnimationFrame = liveDom.window.cancelAnimationFrame.bind(liveDom.window);

  const { act } = await import("react");
  const { createRoot } = await import("react-dom/client");
  const liveRootEl = document.getElementById("root");
  if (!liveRootEl) throw new Error("missing root");
  const liveRoot = createRoot(liveRootEl);
  displayMode = "standard";
  processFoldPref = "auto";
  await act(async () => {
    liveRoot.render(
      React.createElement(
        LocaleProvider,
        null,
        React.createElement(Transcript, {
          items: [
            { kind: "user", id: "iu1", text: "cancelled" },
            { kind: "assistant", id: "ia1", text: "", reasoning: "**got cut off**\n\n- tail detail", streaming: false, workDurationMs: 3_000 },
          ],
          onPrompt: () => {},
          questionNavigator: false,
          running: false,
        }),
      ),
    );
    await new Promise((resolve) => setTimeout(resolve, 0));
  });

  const liveFold = document.querySelector(".turn-collapse--open");
  ok(Boolean(liveFold), "interactive fold with nothing outside stays expanded");
  const liveSummary = document.querySelector<HTMLButtonElement>(".turn-collapse .reasoning-summary");
  ok(liveSummary?.textContent === "**got cut off**", "interactive segment shows the plain-text summary");
  ok(!document.querySelector(".turn-collapse .md"), "interactive segment mounts no Markdown while collapsed");

  const liveClick = async (el: Element | null | undefined, until?: () => boolean) => {
    await act(async () => {
      el?.dispatchEvent(new liveDom.window.MouseEvent("click", { bubbles: true }));
      // The Markdown renderer lazy-loads; flush macrotasks until it lands.
      for (let i = 0; i < 500; i += 1) {
        await new Promise((resolve) => setTimeout(resolve, 0));
        if (until?.()) break;
      }
    });
  };

  await liveClick(liveSummary, () => Boolean(document.querySelector(".turn-collapse .md strong")));
  ok(document.querySelector(".turn-collapse .md strong")?.textContent === "got cut off", "clicking the summary mounts the full Markdown");
  ok(document.querySelector(".turn-collapse .md li")?.textContent === "tail detail", "expanded segment renders Markdown lists");
  ok(document.querySelector(".turn-collapse__reasoning-head")?.getAttribute("aria-expanded") === "true", "segment head reflects the expanded state");

  await liveClick(document.querySelector(".turn-collapse__reasoning-head"));
  ok(!document.querySelector(".turn-collapse .md"), "clicking the segment head collapses back to the summary");
  await liveClick(document.querySelector(".turn-collapse__reasoning-head"));
  ok(document.querySelector(".turn-collapse .md strong")?.textContent === "got cut off", "clicking the segment head expands the full Markdown");

  await act(async () => {
    liveRoot.unmount();
  });
  liveDom.window.close();
} finally {
  await server?.close();
  delete (globalThis as { localStorage?: Storage }).localStorage;
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
