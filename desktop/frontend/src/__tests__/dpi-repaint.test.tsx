// Run: tsx src/__tests__/dpi-repaint.test.tsx
import { JSDOM } from "jsdom";
import { forceWindowsDpiRepaint } from "../lib/dpiScale";

let passed = 0;
let failed = 0;
function ok(v: boolean, label: string) {
  if (v) { process.stdout.write(`  PASS  ${label}\n`); passed++; }
  else { process.stdout.write(`  FAIL  ${label}\n`); failed++; }
}

const dom = new JSDOM("<!doctype html><html><body></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
const g = globalThis as typeof globalThis & {
  window: Window & typeof globalThis;
  document: Document;
  navigator: Navigator;
  requestAnimationFrame: typeof requestAnimationFrame;
};
g.window = dom.window as unknown as Window & typeof globalThis;
g.document = dom.window.document;
Object.defineProperty(g, "navigator", {
  configurable: true,
  value: { platform: "Win32", userAgent: "Windows" },
});
let raf = 0;
const runRaf = (cb: FrameRequestCallback) => {
  raf += 1;
  cb(0);
  return raf;
};
g.requestAnimationFrame = runRaf as typeof requestAnimationFrame;
dom.window.requestAnimationFrame = runRaf as typeof requestAnimationFrame;

forceWindowsDpiRepaint();
ok(true, "forceWindowsDpiRepaint does not throw on Windows");
ok(raf >= 1, "schedules a repaint frame on Windows");
ok(dom.window.document.documentElement.style.getPropertyValue("transform") === "", "transform cleaned up after repaint frame");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed) process.exit(1);
