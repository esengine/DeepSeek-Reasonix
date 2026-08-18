// Run: tsx src/__tests__/message-reasoning-panel.test.tsx

import { JSDOM } from "jsdom";
import { registerHooks } from "node:module";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { LocaleProvider } from "../lib/i18n";
import { AssistantMessage } from "../components/Message";
import { setReasoningSummaryEnabled } from "../lib/reasoningSummaryPreference";
import { applyReasoningDisplayMode, hydrateReasoningDisplayMode } from "../lib/reasoningDisplayPreference";

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.endsWith(".css")) {
      return nextResolve("./asset-stub-for-tests.ts", { ...context, parentURL: import.meta.url });
    }
    return nextResolve(specifier, context);
  },
});

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\nmessage reasoning panel");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: { ...dom.window.navigator, language: "en-US" } });
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

// Most assertions in this file exercise the summary-mode disclosure behavior.
// Select it explicitly so the test remains independent from the product default.
hydrateReasoningDisplayMode("summary", true);

type ReasoningItem = React.ComponentProps<typeof AssistantMessage>["item"];

async function render(item: ReasoningItem, props: { defaultExpanded?: boolean } = {}) {
  await act(async () => {
    root.render(
      <LocaleProvider>
        <AssistantMessage key={item.id} item={item} defaultExpanded={props.defaultExpanded} />
      </LocaleProvider>,
    );
  });
}

async function click(el: Element | null | undefined) {
  await act(async () => {
    el?.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true }));
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

// Completed reasoning: collapsed to a one-line summary, Markdown stays
// unmounted until the user asks for it.
await render({
  kind: "assistant",
  id: "a1",
  text: "",
  reasoning: "initial plan\n\n**important trace**\n\n- line one\n- line two\n\n`inline code`",
  streaming: false,
  reasoningComplete: true,
  reasoningDurationMs: 2_600,
});

const header = document.querySelector<HTMLButtonElement>(".reasoning__head");
ok(Boolean(header), "completed reasoning renders a toggle header");
ok(header?.textContent?.includes("thinking") ?? false, "header keeps the reasoning label");
ok(header?.textContent?.includes("lasted 3s") ?? false, "header shows rounded reasoning duration");
ok(!document.querySelector(".reasoning__body"), "completed reasoning is collapsed by default");
ok(!document.querySelector(".reasoning .md"), "collapsed reasoning mounts no Markdown");

const summary = document.querySelector<HTMLButtonElement>(".reasoning-summary");
ok(summary?.tagName === "BUTTON", "collapsed reasoning shows a clickable summary");
ok(summary?.textContent === "initial plan", "completed summary is the first non-blank line");

await click(summary);
ok(document.querySelector(".reasoning__body")?.textContent?.includes("line two") ?? false, "clicking the summary expands the reasoning body");
ok(document.querySelector(".reasoning__body strong")?.textContent === "important trace", "reasoning renders Markdown emphasis");
ok(document.querySelectorAll(".reasoning__body li").length === 2, "reasoning renders Markdown lists");
ok(document.querySelector(".reasoning__body .md-code")?.textContent === "inline code", "reasoning renders Markdown inline code");
ok(!document.querySelector(".reasoning-summary"), "expanded reasoning hides the summary");

await click(document.querySelector(".reasoning__head"));
ok(!document.querySelector(".reasoning__body"), "clicking the header collapses the reasoning body again");
await click(document.querySelector(".reasoning__head"));
ok(document.querySelector(".reasoning__body")?.textContent?.includes("line two") ?? false, "clicking the header expands the reasoning body");

// Streaming reasoning: also collapsed by default, summary tracks the newest
// tail even after the current line exceeds the summary budget.
const streamingLine = "a".repeat(220);
await render({
  kind: "assistant",
  id: "a2",
  text: "",
  reasoning: `first thought\n\n${streamingLine}LATEST_TOKEN`,
  streaming: true,
  reasoningComplete: false,
});
const streamingSummary = document.querySelector<HTMLButtonElement>(".reasoning-summary");
ok(!document.querySelector(".reasoning__body"), "streaming reasoning is collapsed by default");
ok(streamingSummary?.textContent?.endsWith("LATEST_TOKEN") ?? false, "streaming summary retains the newest tail of a long line");
ok(streamingSummary?.hasAttribute("data-follow-end") ?? false, "streaming summary follows the line tail");
ok(document.querySelector(".reasoning__head")?.hasAttribute("data-running") ?? false, "header keeps the running state");

await render({
  kind: "assistant",
  id: "a2",
  text: "",
  reasoning: `first thought\n\n${streamingLine}LATEST_TOKEN_NEXT`,
  streaming: true,
  reasoningComplete: false,
});
ok(
  document.querySelector(".reasoning-summary")?.textContent?.endsWith("LATEST_TOKEN_NEXT") ?? false,
  "streaming summary updates when more text reaches the same long line",
);

// defaultExpanded keeps the previous always-open behavior.
await render({
  kind: "assistant",
  id: "a3",
  text: "",
  reasoning: "initial plan\n\n**important trace**",
  streaming: false,
  reasoningComplete: true,
}, { defaultExpanded: true });
ok(document.querySelector(".reasoning__body strong")?.textContent === "important trace", "defaultExpanded renders the full Markdown directly");
ok(!document.querySelector(".reasoning-summary"), "defaultExpanded skips the summary");

// The settings switch can disable the preview without mounting Markdown until
// the user opens the reasoning heading.
await act(async () => {
  setReasoningSummaryEnabled(false);
});
const guardedReasoning = new Proxy(new String("guarded reasoning"), {
  get(target, property, receiver) {
    if (property === "length") throw new Error("summary text should not be derived while summaries are disabled");
    return Reflect.get(target, property, receiver);
  },
}) as unknown as string;
let disabledDerivationSkipped = true;
try {
  await render({
    kind: "assistant",
    id: "a-disabled-derivation",
    text: "",
    reasoning: guardedReasoning,
    streaming: true,
    reasoningComplete: false,
  });
} catch {
  disabledDerivationSkipped = false;
}
ok(disabledDerivationSkipped, "disabling reasoning summaries skips summary derivation");
await render({
  kind: "assistant",
  id: "a4",
  text: "",
  reasoning: "initial plan\n\n**important trace**",
  streaming: false,
  reasoningComplete: true,
});
ok(!document.querySelector(".reasoning-summary"), "disabling reasoning summaries hides the collapsed preview");
ok(!document.querySelector(".reasoning__body"), "disabling reasoning summaries keeps Markdown lazy");
await click(document.querySelector(".reasoning__head"));
ok(document.querySelector(".reasoning__body strong")?.textContent === "important trace", "the heading still opens full Markdown when summaries are disabled");
await act(async () => {
  setReasoningSummaryEnabled(true);
});
await render({
  kind: "assistant",
  id: "a5",
  text: "",
  reasoning: "initial plan\n\n**important trace**",
  streaming: false,
  reasoningComplete: true,
});
ok(Boolean(document.querySelector(".reasoning-summary")), "reenabling reasoning summaries restores the preview");

await act(async () => {
  hydrateReasoningDisplayMode("auto", true);
});
await render({
  kind: "assistant",
  id: "a-auto",
  text: "",
  reasoning: "live thought",
  streaming: true,
  reasoningComplete: false,
});
ok(Boolean(document.querySelector(".reasoning__body")), "auto mode opens reasoning while it streams");

await render({
  kind: "assistant",
  id: "a-auto",
  text: "answer",
  reasoning: "live thought",
  streaming: false,
  reasoningComplete: true,
});
ok(!document.querySelector(".reasoning__body"), "auto mode closes untouched reasoning after completion");
ok(Boolean(document.querySelector(".reasoning-summary")), "auto mode leaves a summary after completion");

await render({
  kind: "assistant",
  id: "a-auto-manual",
  text: "",
  reasoning: "manual thought",
  streaming: true,
  reasoningComplete: false,
});
await click(document.querySelector(".reasoning__head"));
await click(document.querySelector(".reasoning__head"));
await render({
  kind: "assistant",
  id: "a-auto-manual",
  text: "answer",
  reasoning: "manual thought",
  streaming: false,
  reasoningComplete: true,
});
ok(Boolean(document.querySelector(".reasoning__body")), "manual reasoning expansion survives auto completion");

await act(async () => {
  applyReasoningDisplayMode("expanded");
});
await render({
  kind: "assistant",
  id: "a-expanded",
  text: "",
  reasoning: "kept thought",
  streaming: false,
  reasoningComplete: true,
});
ok(Boolean(document.querySelector(".reasoning__body")), "expanded mode keeps completed reasoning open");
ok(!document.querySelector(".reasoning-summary"), "expanded mode never falls back to a summary");
await click(document.querySelector(".reasoning__head"));
ok(!document.querySelector(".reasoning__body"), "a manual collapse still wins inside expanded mode");

await act(async () => {
  applyReasoningDisplayMode("hidden");
});
await render({
  kind: "assistant",
  id: "a-hidden",
  text: "answer",
  reasoning: "not shown",
  streaming: false,
  reasoningComplete: true,
});
ok(!document.querySelector(".reasoning"), "hidden mode removes the reasoning panel");
await render({
  kind: "assistant",
  id: "a-hidden-only",
  text: "",
  reasoning: "reasoning-only work",
  streaming: false,
  reasoningComplete: true,
});
ok(!document.querySelector(".msg"), "hidden mode removes reasoning-only message shells");
await act(async () => {
  applyReasoningDisplayMode("summary");
});

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
