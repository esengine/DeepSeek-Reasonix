// Run: tsx src/__tests__/wails-resize-fix.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { useWailsResizeFix } from "../lib/useWailsResizeFix";

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
  if (actual === expected) {
    ok(true, label);
  } else {
    ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
  }
}

function installDom(): JSDOM {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.MouseEvent = dom.window.MouseEvent;
  Object.defineProperty(window, "innerWidth", { configurable: true, value: 100 });
  Object.defineProperty(window, "innerHeight", { configurable: true, value: 80 });
  return dom;
}

function installWailsFlags(flags?: Partial<NonNullable<Window["wails"]>["flags"]>) {
  window.wails = {
    Callback: () => undefined,
    EventsNotify: () => undefined,
    flags: {
      enableResize: true,
      resizeEdge: undefined,
      borderThickness: 6,
      defaultCursor: null,
      cssDragProperty: "--wails-draggable",
      cssDragValue: "drag",
      cssDropProperty: "--wails-drop-target",
      cssDropValue: "drop",
      shouldDrag: false,
      deferDragToMouseMove: false,
      disableScrollbarDrag: false,
      disableDefaultContextMenu: false,
      enableWailsDragAndDrop: false,
      ...flags,
    },
  };
}

function Harness({ enabled, maximised }: { enabled: boolean; maximised: boolean }) {
  useWailsResizeFix(enabled, maximised);
  return null;
}

async function renderHarness(root: Root, enabled: boolean, maximised = false) {
  await act(async () => {
    root.render(<Harness enabled={enabled} maximised={maximised} />);
  });
}

async function unmount(root: Root) {
  await act(async () => {
    root.unmount();
  });
}

console.log("\nwails resize fix");

{
  const dom = installDom();
  installWailsFlags({ enableResize: true, resizeEdge: "n-resize" });
  const root = createRoot(document.getElementById("root")!);

  await renderHarness(root, false, true);
  window.dispatchEvent(new MouseEvent("mousemove", { clientX: 97, clientY: 40 }));
  eq(window.wails?.flags.enableResize, true, "disabled hook leaves Wails resize enabled");
  eq(window.wails?.flags.resizeEdge, "n-resize", "disabled hook leaves resize edge unchanged");

  await unmount(root);
  dom.window.close();
}

{
  const dom = installDom();
  document.documentElement.style.cursor = "grab";
  installWailsFlags({ enableResize: true, resizeEdge: "n-resize" });
  const root = createRoot(document.getElementById("root")!);

  await renderHarness(root, true);
  eq(window.wails?.flags.enableResize, false, "enabled hook disables Wails built-in mousemove resize");

  window.dispatchEvent(new MouseEvent("mousemove", { clientX: 97, clientY: 40 }));
  eq(window.wails?.flags.resizeEdge, "e-resize", "enabled hook detects right edge using innerWidth CSS pixels");
  eq(document.documentElement.style.cursor, "e-resize", "enabled hook mirrors detected edge cursor");

  await unmount(root);
  eq(window.wails?.flags.enableResize, true, "cleanup restores previous enableResize");
  eq(window.wails?.flags.resizeEdge, "n-resize", "cleanup restores previous resizeEdge");
  eq(document.documentElement.style.cursor, "grab", "cleanup restores previous cursor");

  dom.window.close();
}

{
  const dom = installDom();
  document.documentElement.style.cursor = "n-resize";
  installWailsFlags({ enableResize: true, resizeEdge: undefined, defaultCursor: null });
  const root = createRoot(document.getElementById("root")!);

  await renderHarness(root, true, true);
  eq(window.wails?.flags.resizeEdge, undefined, "maximised startup clears an absent resize edge safely");
  eq(document.documentElement.style.cursor, "default", "maximised startup replaces a stale resize cursor");

  await unmount(root);
  dom.window.close();
}

{
  const dom = installDom();
  document.documentElement.style.cursor = "e-resize";
  installWailsFlags({ enableResize: true, resizeEdge: "e-resize", defaultCursor: "grab" });
  const root = createRoot(document.getElementById("root")!);

  await renderHarness(root, true, true);
  eq(window.wails?.flags.resizeEdge, undefined, "maximised startup clears a stale resize edge");
  eq(document.documentElement.style.cursor, "grab", "maximised startup restores Wails' remembered cursor");

  await unmount(root);
  dom.window.close();
}

{
  const dom = installDom();
  Object.defineProperty(window, "outerWidth", { configurable: true, value: 104 });
  Object.defineProperty(window, "outerHeight", { configurable: true, value: 84 });
  Object.defineProperty(window.screen, "availWidth", { configurable: true, value: 100 });
  Object.defineProperty(window.screen, "availHeight", { configurable: true, value: 80 });
  installWailsFlags({ enableResize: true, resizeEdge: undefined });
  const root = createRoot(document.getElementById("root")!);

  await renderHarness(root, true, false);
  window.dispatchEvent(new MouseEvent("mousemove", { clientX: 97, clientY: 40 }));
  eq(window.wails?.flags.resizeEdge, "e-resize", "near-full-size normal window keeps edge resizing");

  await unmount(root);
  dom.window.close();
}

{
  const dom = installDom();
  installWailsFlags({ enableResize: true, resizeEdge: undefined });
  const root = createRoot(document.getElementById("root")!);

  await renderHarness(root, true, false);
  window.dispatchEvent(new MouseEvent("mousemove", { clientX: 97, clientY: 40 }));
  eq(window.wails?.flags.resizeEdge, "e-resize", "normal window detects the resize edge");

  await renderHarness(root, true, true);
  eq(window.wails?.flags.resizeEdge, undefined, "maximise transition clears the resize edge immediately");
  eq(document.documentElement.style.cursor, "default", "maximise transition restores the cursor");

  await renderHarness(root, true, false);
  window.dispatchEvent(new MouseEvent("mousemove", { clientX: 97, clientY: 40 }));
  eq(window.wails?.flags.resizeEdge, "e-resize", "restore transition re-enables edge resizing");

  await unmount(root);
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
