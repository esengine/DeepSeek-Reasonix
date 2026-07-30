// Run: tsx src/__tests__/confirm-dialog.test.tsx

import { JSDOM } from "jsdom";
import React, { useState } from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { useConfirmDialog } from "../components/ConfirmDialog";

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

function Harness() {
  const { confirm, dialog } = useConfirmDialog();
  const [result, setResult] = useState("pending");
  const open = async () => {
    const confirmed = await confirm({
      title: "Delete theme",
      message: "Delete Example? This cannot be undone.",
      confirmLabel: "Delete",
      cancelLabel: "Cancel",
      tone: "danger",
    });
    setResult(String(confirmed));
  };
  return (
    <>
      <button id="open-confirm" type="button" onClick={() => void open()}>Open</button>
      <output id="confirm-result">{result}</output>
      {dialog}
    </>
  );
}

async function flush() {
  await new Promise((resolve) => setTimeout(resolve, 20));
}

console.log("\nReasonix confirmation dialog");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
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
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);

await act(async () => {
  root.render(<Harness />);
  await flush();
});

const openButton = document.getElementById("open-confirm") as HTMLButtonElement;
openButton.focus();
await act(async () => {
  openButton.click();
  await flush();
});

const dialog = document.querySelector<HTMLElement>(".reasonix-confirm-dialog");
const cancelButton = Array.from(document.querySelectorAll<HTMLButtonElement>(".reasonix-confirm-dialog button"))
  .find((button) => button.textContent === "Cancel");
const deleteButton = Array.from(document.querySelectorAll<HTMLButtonElement>(".reasonix-confirm-dialog button"))
  .find((button) => button.textContent === "Delete");
ok(dialog?.getAttribute("role") === "dialog" && dialog.getAttribute("aria-modal") === "true", "renders an accessible modal dialog");
ok(dialog?.textContent?.includes("Delete Example? This cannot be undone.") === true, "renders the confirmation message");
ok(deleteButton?.classList.contains("btn--danger") === true, "uses the danger button recipe for deletion");
ok(document.activeElement === cancelButton, "focuses Cancel first for destructive confirmation");

await act(async () => {
  document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  await flush();
});
ok(document.querySelector(".reasonix-confirm-dialog") === null, "Escape cancels and closes the dialog");
ok(document.getElementById("confirm-result")?.textContent === "false", "cancellation resolves false");
ok(document.activeElement === openButton, "restores focus to the triggering control");

await act(async () => {
  openButton.click();
  await flush();
});
const secondDeleteButton = Array.from(document.querySelectorAll<HTMLButtonElement>(".reasonix-confirm-dialog button"))
  .find((button) => button.textContent === "Delete");
await act(async () => {
  secondDeleteButton?.click();
  await flush();
});
ok(document.getElementById("confirm-result")?.textContent === "true", "confirm action resolves true");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
