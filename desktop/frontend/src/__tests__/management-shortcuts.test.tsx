import assert from "node:assert/strict";
import { managementDom } from "../test-support/managementDom";
const dom = managementDom();
const { default: React, act } = await import("react");
const { createRoot } = await import("react-dom/client");
const { useGlobalShortcut, resolvedShortcutCombo, detectShortcutPlatform } = await import("../lib/keyboardShortcuts");
const { useAppNavigationStore: navigation } = await import("../store/appNavigation");
let sidebar = 0; let palette = 0; let closes = 0;
function Shortcuts() {
  useGlobalShortcut("sidebar.toggle", () => { sidebar++; });
  useGlobalShortcut("commandPalette.open", () => { palette++; });
  useGlobalShortcut("tab.close", () => { closes++; navigation.getState().returnToWorkspace(); });
  return null;
}
const fire = (action: Parameters<typeof resolvedShortcutCombo>[0]) => {
  const combo = resolvedShortcutCombo(action, detectShortcutPlatform());
  document.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: combo.key, ctrlKey: combo.ctrl, metaKey: combo.meta, altKey: combo.alt, shiftKey: combo.shift, bubbles: true, cancelable: true }));
};
const root = createRoot(document.getElementById("root")!);
await act(async () => root.render(<Shortcuts />));
fire("sidebar.toggle"); assert.equal(sidebar, 1);
navigation.getState().openPage({ kind: "automation" });
fire("sidebar.toggle"); assert.equal(sidebar, 1, "hidden workspace shortcuts are suspended");
fire("commandPalette.open"); assert.equal(palette, 1);
const modal = document.createElement("div"); modal.setAttribute("aria-modal", "true"); document.body.append(modal);
fire("tab.close"); assert.equal(closes, 0, "a child dialog has priority over page shortcuts");
modal.remove(); fire("tab.close");
assert.equal(closes, 1); assert.equal(navigation.getState().page.kind, "workspace");
await act(async () => root.unmount()); dom.window.close();
console.log("PASS hidden-workspace shortcuts, command palette, child-dialog priority and return shortcut");
