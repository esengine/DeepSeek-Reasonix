import { configure } from "@testing-library/react";
import { afterEach, beforeEach, expect } from "vitest";
import { MESSAGES } from "./i18n/msg";
import { forgetAskDrafts } from "./state/askdraft";

configure({ asyncUtilTimeout: 30_000 });
if (typeof window !== "undefined" && !window.matchMedia) {
  window.matchMedia = query => ({ matches: false, media: query, onchange: null, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent: () => false });
}

afterEach(forgetAskDrafts);

// A message key on screen is a call site that skipped t(): the reader sees
// "plan.action.start" instead of the words. Every test watches for it.
const SHOWN = /\b[a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+\b/g;
const ATTRS = ["aria-label", "title", "placeholder", "alt", "aria-description"];
let seen: Set<string> = new Set();
let watch: MutationObserver | null = null;
const note = (text: string | null | undefined) => {
  for (const m of (text ?? "").matchAll(SHOWN)) if (m[0] in MESSAGES) seen.add(m[0]);
};
const scan = (node: Node) => {
  if (node.nodeType === Node.TEXT_NODE) return note(node.textContent);
  if (!(node instanceof Element)) return;
  note(node.textContent);
  for (const el of [node, ...node.querySelectorAll("*")]) for (const a of ATTRS) note(el.getAttribute(a));
};
if (typeof document !== "undefined" && typeof MutationObserver !== "undefined") {
  beforeEach(() => {
    seen = new Set();
    watch = new MutationObserver((records) => {
      for (const r of records) {
        if (r.type === "characterData") note(r.target.textContent);
        else if (r.type === "attributes" && r.target instanceof Element && r.attributeName) note(r.target.getAttribute(r.attributeName));
        else r.addedNodes.forEach(scan);
      }
    });
    watch.observe(document.body, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ATTRS });
  });
  afterEach(() => {
    watch?.takeRecords().forEach((r) => r.addedNodes.forEach(scan));
    watch?.disconnect();
    expect([...seen], "message keys rendered as text; pass them through t()").toEqual([]);
  });
}
