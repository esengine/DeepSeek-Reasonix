import assert from "node:assert/strict";
import { useAppNavigationStore as store } from "../store/appNavigation";
import { SETTINGS_NAV_TABS } from "../components/SettingsNavigation";
store.getState().openPage({ kind: "automation" });
const link = store.getState().generation;
store.getState().openPage({ kind: "trash" });
store.getState().returnFromAutomationLink(link);
assert.equal(store.getState().page.kind, "trash", "late linked-session result cannot replace a newer page");
store.getState().openPage({ kind: "automation" });
store.getState().returnFromAutomationLink(store.getState().generation);
assert.equal(store.getState().page.kind, "workspace");
assert.equal(store.getState().automationReturn, true);
store.getState().enterConversation();
assert.equal(store.getState().automationReturn, false);
for (const tab of SETTINGS_NAV_TABS) {
  store.getState().openPage({ kind: "settings", tab });
  assert.deepEqual(store.getState().page, { kind: "settings", tab }, `openPage preserves ${tab}`);
  store.getState().setSettingsTarget(null);
  assert.equal(store.getState().page.kind, "workspace");
  assert.equal(store.getState().lastSettingsTarget, tab, `closing remembers ${tab}`);
  store.getState().setSettingsTarget(store.getState().lastSettingsTarget);
  assert.deepEqual(store.getState().page, { kind: "settings", tab }, `reopening preserves ${tab}`);
  store.getState().setSettingsTarget((previous) => {
    assert.equal(previous, tab);
    return previous;
  });
  assert.deepEqual(store.getState().page, { kind: "settings", tab }, `functional updates preserve ${tab}`);
}
store.getState().returnToWorkspace();
assert.equal(store.getState().visitedTrash, true);
assert.equal(store.getState().visitedAutomation, true);
console.log("PASS all settings routes, remembered targets, functional updates, page retention and linked-session generations");
