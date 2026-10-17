import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { ToastProvider, useToast, type ToastContextValue } from "../lib/toast";
import { LocaleProvider } from "../lib/i18n";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage,
  Node: dom.window.Node, HTMLElement: dom.window.HTMLElement, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
let toast!: ToastContextValue;
function Fixture() { toast = useToast(); return null; }
try {
  await act(async () => root.render(<LocaleProvider><ToastProvider><Fixture /></ToastProvider></LocaleProvider>));
  // The filename starts with ASCII; generic exception classification must not
  // hide this already-localized, typed upload refusal behind a details toggle.
  const message = "oversize.apk 过大，无法附加（上限 1024 MB）";
  await act(async () => { toast.showToast(message, "warn", { summary: message }); });
  assert.equal(document.querySelector(".toast--warn .user-error__summary")?.textContent, message);
  assert.equal(document.querySelector(".toast--warn .user-error__toggle"), null);
  await act(async () => root.unmount());
  console.log("attachment ceiling toast: localized filename and limit remain visible");
} finally { dom.window.close(); }
