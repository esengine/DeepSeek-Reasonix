import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { RecoveryLineageView } from "../lib/types";

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", { pretendToBeVisual: true, url: "http://localhost/" });
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(dom.window.navigator, "language", { configurable: true, value: "en-US" });
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;

const { RecoveryLineageDialog } = await import("../components/RecoveryLineageDialog");
const { LocaleProvider } = await import("../lib/i18n");

const initial: RecoveryLineageView = {
  groupId: "group",
  state: "diverged",
  branchCount: 3,
  unresolved: 1,
  cleanupEligible: 1,
  members: [
    { path: "/private/root.jsonl", role: "normal", canonical: true, turns: 3, open: true, running: false, preview: "original preview", lastActivityAt: 100 },
    { path: "/private/covered.jsonl", role: "covered_copy", canonical: false, turns: 3, open: false, running: false, preview: "covered preview" },
    { path: "/private/fork.jsonl", role: "diverged", canonical: false, turns: 4, open: false, running: false, preview: "unique preview", versionNote: "investigation" },
  ],
};

let opened = "";
const root = createRoot(document.getElementById("root")!);
await act(async () => {
  root.render(
    <LocaleProvider>
      <RecoveryLineageDialog
        topic={{ scope: "global", topicId: "topic" }}
        initial={initial}
        onClose={() => {}}
        onChanged={() => {}}
        onOpenVersion={(member) => { opened = member.path; }}
      />
    </LocaleProvider>,
  );
});

const body = document.body.textContent || "";
if (!body.includes("Session versions") || !body.includes("Default version") || !body.includes("Another version")) throw new Error("dialog does not use the simplified version language");
if (!body.includes("original preview") || !body.includes("unique preview") || !body.includes("investigation")) throw new Error("dialog omits user-facing version metadata");
if (body.includes("covered preview") || body.includes("covered_copy")) throw new Error("covered copies leaked into the ordinary version dialog");
if (body.includes("/private/") || body.includes(".jsonl")) throw new Error("physical session paths leaked into the dialog");
if (body.includes("cleanup") || body.includes("Move covered")) throw new Error("ordinary version dialog exposes cleanup controls");

const openButtons = Array.from(document.querySelectorAll<HTMLButtonElement>("button")).filter((button) => button.textContent?.trim() === "Open this version");
if (openButtons.length !== 2) throw new Error(`expected 2 visible version actions, got ${openButtons.length}`);
await act(async () => { openButtons[1].click(); });
if (opened !== "/private/fork.jsonl") throw new Error("open action was not bound to the selected version");

await act(async () => root.unmount());
dom.window.close();
console.log("  PASS  session version dialog hides persistence details");
