// Run: node --import tsx src/__tests__/process-card-render.test.tsx
//
// ProcessCard rendering: status icon mapping (running/done/failed/waiting/
// stopped), controlled vs uncontrolled open state, no-body rendering (no
// toggle, chevron, or aria-expanded), Escape handling, and the
// data-tone/data-open/data-has-body attributes. Pure component — no mocks.

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ProcessCard, ProcessStatusIcon } from "../components/ProcessCard";

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

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><head></head><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  return dom;
}

console.log("\nprocess card");

// ProcessStatusIcon state → icon mapping: running spins, done checks, failed
// crosses, and every other state renders as a tonal dot.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(
      <>
        <ProcessStatusIcon state="running" label="Running" />
        <ProcessStatusIcon state="done" label="Done" />
        <ProcessStatusIcon state="failed" label="Failed" />
        <ProcessStatusIcon state="waiting" label="Waiting" />
        <ProcessStatusIcon state="stopped" label="Stopped" />
      </>,
    );
  });

  const spin = document.querySelector("span.process-card__spin");
  ok(!!spin, "running maps to the spinner");
  eq(spin?.getAttribute("role"), "img", "spinner announces as an image");
  eq(spin?.getAttribute("aria-label"), "Running", "spinner carries the running label");
  eq(spin?.getAttribute("title"), "Running", "spinner carries a title tooltip");

  const doneIcon = document.querySelector("svg.process-card__status--done");
  ok(!!doneIcon, "done maps to the check icon");
  eq(doneIcon?.getAttribute("aria-label"), "Done", "check icon carries the done label");

  const failedIcon = document.querySelector("svg.process-card__status--failed");
  ok(!!failedIcon, "failed maps to the x icon");
  eq(failedIcon?.getAttribute("aria-label"), "Failed", "x icon carries the failed label");

  const waitingDot = document.querySelector("span.process-card__dot--waiting");
  ok(!!waitingDot, "waiting maps to a tonal dot");
  eq(waitingDot?.getAttribute("role"), "img", "dot announces as an image");
  eq(waitingDot?.getAttribute("aria-label"), "Waiting", "waiting dot carries the waiting label");
  eq(waitingDot?.getAttribute("title"), "Waiting", "waiting dot carries a title tooltip");

  const stoppedDot = document.querySelector("span.process-card__dot--stopped");
  ok(!!stoppedDot, "stopped maps to a tonal dot");
  eq(stoppedDot?.getAttribute("aria-label"), "Stopped", "stopped dot carries the stopped label");

  eq(document.querySelectorAll("svg.process-card__status").length, 2, "only done and failed use the status icon classes");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Uncontrolled card: data attributes, initial closed state, head-click toggle.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(
      <ProcessCard
        tone="danger"
        icon={<span id="card-icon" />}
        kind="Build"
        name="web"
        meta="42s"
        defaultOpen={false}
        className="custom-card"
      >
        <p id="card-body">payload</p>
      </ProcessCard>,
    );
  });

  const card = document.querySelector(".process-card");
  const head = document.querySelector("button.process-card__head") as HTMLButtonElement | null;
  if (!card || !head) throw new Error("process card did not render");

  eq(card.getAttribute("data-tone"), "danger", "card exposes the tone attribute");
  eq(card.getAttribute("data-open"), "false", "card starts closed by default");
  eq(card.getAttribute("data-has-body"), "true", "card with children is marked as having a body");
  ok(card.classList.contains("custom-card"), "className is appended");
  eq(head.getAttribute("aria-expanded"), "false", "collapsed head announces aria-expanded=false");
  ok(!!document.querySelector(".process-card__chevron"), "card with a body renders the chevron");
  ok(!!document.querySelector("#card-icon"), "icon renders");
  eq(document.querySelector(".process-card__kind")?.textContent, "Build", "kind renders");
  eq(document.querySelector(".process-card__name")?.textContent, "web", "name renders");
  eq(document.querySelector(".process-card__meta")?.textContent, "42s", "meta renders");
  eq(document.querySelector("#card-body")?.textContent, "payload", "body children render");

  await act(async () => {
    head.click();
  });
  eq(card.getAttribute("data-open"), "true", "clicking the head opens the card");
  eq(head.getAttribute("aria-expanded"), "true", "open head announces aria-expanded=true");

  await act(async () => {
    head.click();
  });
  eq(card.getAttribute("data-open"), "false", "clicking the head again closes the card");
  eq(head.getAttribute("aria-expanded"), "false", "closed head announces aria-expanded=false");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// defaultOpen starts the card open.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(
      <ProcessCard icon={<span />} kind="K" defaultOpen>
        <p>body</p>
      </ProcessCard>,
    );
  });

  const card = document.querySelector(".process-card");
  const head = document.querySelector("button.process-card__head") as HTMLButtonElement | null;
  if (!card || !head) throw new Error("process card did not render");
  eq(card.getAttribute("data-open"), "true", "defaultOpen starts the card open");
  eq(head.getAttribute("aria-expanded"), "true", "defaultOpen head announces aria-expanded=true");

  await act(async () => {
    head.click();
  });
  eq(card.getAttribute("data-open"), "false", "defaultOpen card closes on click");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Controlled card: open + onOpenChange drive the state; internal state never
// overrides the prop.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const changes: boolean[] = [];
  await act(async () => {
    root.render(
      <ProcessCard icon={<span />} kind="K" open={false} onOpenChange={(next) => changes.push(next)}>
        <p>body</p>
      </ProcessCard>,
    );
  });

  const card = document.querySelector(".process-card");
  const head = document.querySelector("button.process-card__head") as HTMLButtonElement | null;
  if (!card || !head) throw new Error("process card did not render");
  eq(card.getAttribute("data-open"), "false", "controlled card starts closed");
  eq(head.getAttribute("aria-expanded"), "false", "controlled closed head announces aria-expanded=false");

  await act(async () => {
    head.click();
  });
  eq(changes.length, 1, "controlled click reports the next open value");
  eq(changes[0], true, "controlled click reports open=true");
  eq(card.getAttribute("data-open"), "false", "controlled open prop keeps the card closed");
  eq(head.getAttribute("aria-expanded"), "false", "controlled aria-expanded stays false");

  // The parent adopts the reported value: re-render with open=true.
  await act(async () => {
    root.render(
      <ProcessCard icon={<span />} kind="K" open={true} onOpenChange={(next) => changes.push(next)}>
        <p>body</p>
      </ProcessCard>,
    );
  });
  eq(card.getAttribute("data-open"), "true", "controlled card follows the open prop");
  eq(head.getAttribute("aria-expanded"), "true", "controlled open head announces aria-expanded=true");

  await act(async () => {
    head.click();
  });
  eq(changes.length, 2, "second controlled click reports again");
  eq(changes[1], false, "second controlled click reports open=false");
  eq(card.getAttribute("data-open"), "true", "controlled open prop keeps the card open");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// No body: no chevron, no aria-expanded, no wrap, and the head cannot toggle.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const changes: boolean[] = [];
  await act(async () => {
    root.render(
      <ProcessCard icon={<span />} kind="K" onOpenChange={(next) => changes.push(next)} />,
    );
  });

  const card = document.querySelector(".process-card");
  const head = document.querySelector("button.process-card__head") as HTMLButtonElement | null;
  if (!card || !head) throw new Error("process card did not render");
  eq(card.getAttribute("data-has-body"), "false", "bodyless card is marked as having no body");
  ok(!document.querySelector(".process-card__chevron"), "bodyless card renders no chevron");
  eq(head.getAttribute("aria-expanded"), null, "bodyless head has no aria-expanded attribute");
  ok(!document.querySelector(".process-card__wrap"), "bodyless card renders no body wrapper");
  ok(!document.querySelector(".process-card__body"), "bodyless card renders no body");

  await act(async () => {
    head.click();
  });
  eq(changes.length, 0, "clicking a bodyless head never reports a toggle");
  eq(card.getAttribute("data-open"), "false", "bodyless card stays closed after a head click");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// No body with defaultOpen: renders open but still cannot toggle.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(<ProcessCard icon={<span />} kind="K" defaultOpen />);
  });

  const card = document.querySelector(".process-card");
  const head = document.querySelector("button.process-card__head") as HTMLButtonElement | null;
  if (!card || !head) throw new Error("process card did not render");
  eq(card.getAttribute("data-open"), "true", "bodyless defaultOpen card renders open");

  await act(async () => {
    head.click();
  });
  eq(card.getAttribute("data-open"), "true", "bodyless card cannot be toggled closed");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Escape toggles the card; other keys do not.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(
      <ProcessCard icon={<span />} kind="K" defaultOpen>
        <p>body</p>
      </ProcessCard>,
    );
  });

  const card = document.querySelector(".process-card");
  const head = document.querySelector("button.process-card__head") as HTMLButtonElement | null;
  if (!card || !head) throw new Error("process card did not render");
  eq(card.getAttribute("data-open"), "true", "escape scenario starts open");

  await act(async () => {
    head.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
  });
  eq(card.getAttribute("data-open"), "false", "Escape closes an open card");

  await act(async () => {
    head.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
  });
  eq(card.getAttribute("data-open"), "true", "Escape on a closed card opens it (toggle)");

  await act(async () => {
    head.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
  });
  eq(card.getAttribute("data-open"), "true", "non-Escape keys do not toggle the card");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
