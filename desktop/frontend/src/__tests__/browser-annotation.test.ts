// Run: tsx src/__tests__/browser-annotation.test.ts

import { createBrowserAnnotation, formatBrowserAnnotation } from "../lib/browserAnnotation";
import { browserModifiers, browserMouseButton, browserPointFromClient, browserViewportSize } from "../lib/browserInput";
import type { BrowserElementView, BrowserSessionView } from "../lib/bridge";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  const left = JSON.stringify(actual);
  const right = JSON.stringify(expected);
  if (left === right) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${right}, got ${left}\n`);
    failed += 1;
  }
}

const session: BrowserSessionView = {
  tabId: "tab-1",
  pageId: "page-1",
  url: "https://example.com/page",
  title: "Example",
  width: 1440,
  height: 900,
  canGoBack: false,
  canGoForward: false,
  sequence: 4,
};

const selection: BrowserElementView = {
  tabId: "tab-1",
  pageId: "page-1",
  backendNodeId: 12,
  url: session.url,
  title: session.title,
  tag: "button",
  selector: "main > button.submit",
  accessibleName: "发送",
  text: "发送",
  outerHTML: "<button>发送</button>",
  box: { x: 20.2, y: 30.7, width: 120.4, height: 38.2 },
  computedStyles: {
    "background-color": "rgb(25, 26, 31)",
    "border-radius": "6px",
  },
  styleOverrides: {
    "border-radius": "10px",
    "background-color": "#d97757",
  },
};

const annotation = createBrowserAnnotation(
  selection,
  session,
  ".reasonix/attachments/viewport.jpg",
  ".reasonix/attachments/element.jpg",
);

eq(annotation.styleChanges, {
  "background-color": { before: "rgb(25, 26, 31)", after: "#d97757" },
  "border-radius": { before: "6px", after: "10px" },
}, "style differences retain computed before values");

const text = formatBrowserAnnotation(annotation);
eq(text.includes("Selector: main > button.submit"), true, "annotation includes selector");
eq(text.includes("Viewport: 1440x900"), true, "annotation includes viewport");
eq(text.includes("Element screenshot: .reasonix/attachments/element.jpg"), true, "annotation includes element screenshot");

eq(browserViewportSize(200, 5000), { width: 320, height: 4096 }, "viewport dimensions are clamped");
eq(
  browserPointFromClient(60, 45, { left: 10, top: 20, width: 100, height: 50 }, { width: 1000, height: 500 }),
  { x: 500, y: 250 },
  "client coordinates scale into browser viewport",
);
eq(browserMouseButton(2), "right", "mouse buttons map to CDP names");
eq(browserModifiers({ altKey: true, ctrlKey: true, metaKey: false, shiftKey: true }), 11, "modifier mask matches CDP bits");

if (failed > 0) {
  process.stderr.write(`browser annotation tests failed: ${failed}\n`);
  process.exit(1);
}
process.stdout.write(`browser annotation tests passed: ${passed}\n`);