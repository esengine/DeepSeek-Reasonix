// Run: tsx src/__tests__/browser-annotation.test.ts

import { createBrowserAnnotation, formatBrowserAnnotation } from "../lib/browserAnnotation";
import {
  browserElementBoxToCanvas,
  browserFloatingPosition,
  browserModifiers,
  browserMouseButton,
  browserPointFromClient,
  browserViewportSize,
  stepBrowserCSSNumericValue,
} from "../lib/browserInput";
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
  canAnnotate: true,
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
    "background-color": "rgb(217, 119, 87)",
    "border-radius": "10px",
  },
  originalStyles: {
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
  "把按钮文案加粗，并增加圆角。",
);

eq(annotation.styleChanges, {
  "background-color": { before: "rgb(25, 26, 31)", after: "#d97757" },
  "border-radius": { before: "6px", after: "10px" },
}, "style differences retain computed before values");

const text = formatBrowserAnnotation(annotation);
eq(text.includes("Selector: main > button.submit"), true, "annotation includes selector");
eq(text.includes("Viewport: 1440x900"), true, "annotation includes viewport");
eq(text.includes("Screenshot:"), false, "annotation excludes screenshots");
eq(text.includes("attachments"), false, "annotation excludes image attachments");
eq(text.includes("用户批注:\n把按钮文案加粗，并增加圆角。"), true, "annotation prominently includes free-form note");
eq(text.indexOf("用户批注:") < text.indexOf("URL:"), true, "annotation places the user's note before element metadata");

const noteOnly = createBrowserAnnotation(
  { ...selection, styleOverrides: {} },
  session,
  "只调整源码结构，不预览样式。",
);
const noteOnlyText = formatBrowserAnnotation(noteOnly);
eq(noteOnly.styleChanges, {}, "annotation allows no style overrides");
eq(noteOnlyText.includes("- No temporary style overrides"), true, "note-only annotation explains empty overrides");
eq(noteOnlyText.includes("只调整源码结构，不预览样式。"), true, "note-only annotation retains its request");

eq(browserViewportSize(200, 5000), { width: 320, height: 4096 }, "viewport dimensions are clamped");
eq(
  browserPointFromClient(60, 45, { left: 10, top: 20, width: 100, height: 50 }, { width: 1000, height: 500 }),
  { x: 500, y: 250 },
  "client coordinates scale into browser viewport",
);
eq(browserMouseButton(2), "right", "mouse buttons map to CDP names");
eq(browserModifiers({ altKey: true, ctrlKey: true, metaKey: false, shiftKey: true }), 11, "modifier mask matches CDP bits");
eq(
  browserElementBoxToCanvas(
    { x: 20, y: 40, width: 100, height: 50 },
    { width: 1000, height: 500 },
    { width: 500, height: 250 },
  ),
  { x: 10, y: 20, width: 50, height: 25 },
  "element bounds scale into canvas coordinates",
);
const constrainedPosition = browserFloatingPosition(
  { x: 220, y: 170, width: 20, height: 20 },
  { width: 250, height: 200 },
);
eq(constrainedPosition.x >= 8, true, "floating inspector stays inside the left edge");
eq(constrainedPosition.y >= 8, true, "floating inspector stays inside the top edge");
eq(constrainedPosition.x + constrainedPosition.width <= 242, true, "floating inspector stays inside the right edge");
eq(constrainedPosition.y + constrainedPosition.maxHeight <= 192, true, "floating inspector stays inside the bottom edge");
eq(stepBrowserCSSNumericValue("font-size", "16px", 1), "17px", "numeric stepping preserves units");
eq(stepBrowserCSSNumericValue("font-size", "16px", 1, true), "26px", "shift accelerates numeric stepping");
eq(stepBrowserCSSNumericValue("opacity", "0.95", 1), "1", "opacity stepping clamps at one");
eq(stepBrowserCSSNumericValue("font-weight", "400", -1), "300", "font weight steps by hundreds");
eq(stepBrowserCSSNumericValue("margin", "1px 2px", 1), null, "compound CSS values are not stepped");

if (failed > 0) {
  process.stderr.write(`browser annotation tests failed: ${failed}\n`);
  process.exit(1);
}
process.stdout.write(`browser annotation tests passed: ${passed}\n`);