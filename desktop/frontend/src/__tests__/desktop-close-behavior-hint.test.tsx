import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { DesktopCloseBehaviorHint } from "../components/DesktopCloseBehaviorHint";
import type { AppBindings } from "../lib/bridge";

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
window.go = {
  main: {
    App: {
      GetDesktopShellStatus: async () => ({
        trayState: "unavailable",
        backgroundCloseAvailable: false,
        reason: "no_host",
      }),
    } as Partial<AppBindings> as AppBindings,
  },
};

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);
await act(async () => {
  root.render(<DesktopCloseBehaviorHint backgroundSelected hint="Keep running after close." unavailableHint="Closing this time will quit." />);
  await Promise.resolve();
});
if (rootElement.textContent !== "Keep running after close.Closing this time will quit.") {
  throw new Error(`unexpected close fallback hint: ${JSON.stringify(rootElement.textContent)}`);
}
await act(async () => root.unmount());
console.log("desktop close behavior hint: ok");
