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
Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: {
    getItem(key: string) {
      return key === "reasonix-display-mode" ? displayMode : null;
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

  function render(items: Item[], options: { mode?: "standard" | "compact"; running?: boolean; turnStartAt?: number } = {}) {
    displayMode = options.mode ?? "standard";
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

  const intermediateDoc = render([
    { kind: "user", id: "u2", text: "continue" },
    { kind: "assistant", id: "a4", text: "I will inspect the files", reasoning: "plan", streaming: false },
    { kind: "tool", id: "t3", name: "read_file", args: "{}", readOnly: true, status: "done" },
    { kind: "assistant", id: "a5", text: "all done", reasoning: "verify", streaming: false },
  ]);
  const intermediate = Array.from(intermediateDoc.querySelectorAll(".msg--assistant")).find((node) => node.textContent?.includes("I will inspect the files"));
  const final = Array.from(intermediateDoc.querySelectorAll(".msg--assistant")).find((node) => node.textContent?.includes("all done"));
  ok(intermediateDoc.querySelectorAll(".turn-collapse").length === 1, "intermediate assistant text does not create another fold");
  ok(intermediate?.closest(".turn-collapse"), "intermediate assistant text stays inside the work fold");
  ok(final && !final.closest(".turn-collapse"), "only the last assistant answer stays outside the work fold");

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

  const originalNow = Date.now;
  Date.now = () => 25_000;
  try {
    const runningDoc = render([
      { kind: "user", id: "u3", text: "run" },
      { kind: "assistant", id: "a6", text: "", reasoning: "working", streaming: false, workDurationMs: 5_000 },
    ], { running: true, turnStartAt: 1_000 });
    ok(runningDoc.querySelector(".turn-collapse__label")?.textContent === "Working 24s", "active turn stays Working between model and tool events");
  } finally {
    Date.now = originalNow;
  }

  const completedDoc = render([
    { kind: "user", id: "u4", text: "finish" },
    { kind: "assistant", id: "a7", text: "done", reasoning: "worked", streaming: false, workDurationMs: 24_000 },
  ]);
  ok(completedDoc.querySelector(".turn-collapse__label")?.textContent === "Worked 24s", "completed turn keeps the persisted wall-clock duration");
} finally {
  await server?.close();
  delete (globalThis as { localStorage?: Storage }).localStorage;
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
