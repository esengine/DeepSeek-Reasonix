import { configure } from "@testing-library/react";
import { afterEach } from "vitest";
import { forgetAskDrafts } from "./state/askdraft";

configure({ asyncUtilTimeout: 30_000 });
if (typeof window !== "undefined" && !window.matchMedia) {
  window.matchMedia = query => ({ matches: false, media: query, onchange: null, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent: () => false });
}

afterEach(forgetAskDrafts);

// A catalogue id on screen means a message reached the DOM without passing
// through t(). Checked after every test that rendered something.
const ID_TEXT = /(?<![\w.])[a-z][A-Za-z0-9]*\.[A-Za-z0-9]+(?![\w.])/g;
afterEach(async () => {
  if (typeof document === "undefined" || !document.body) return;
  const { ZH } = await import("./i18n/zh");
  const seen = new Set<string>();
  const scan = (text: string | null | undefined) => {
    for (const m of text?.matchAll(ID_TEXT) ?? []) if (m[0] in ZH) seen.add(m[0]);
  };
  scan(document.body.textContent);
  for (const el of document.body.querySelectorAll("[title],[aria-label],[placeholder],[alt]")) {
    for (const a of ["title", "aria-label", "placeholder", "alt"]) scan(el.getAttribute(a));
  }
  if (seen.size) throw new Error(`catalogue ids rendered without t(): ${[...seen].join(", ")}`);
});
