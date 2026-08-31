// TabContainer is the right dock's tab shell: the tab strip on top (with the
// + add menu) and the active tab's panel below, routed through TabContent.
// The ActivityBar sits to its left inside the workbench-dock (rendered by
// App); panel content is provided as a renderer because the panels need App's
// props. When the last tab is closed the container collapses (activityBarOpen
// flips false in the store), so this component only renders while tabs exist.

import { useCallback, useRef } from "react";
import type { ReactNode } from "react";
import { useActivityBarStore, type TabItem, type TabType } from "../../store/activityBar";
import { TabAddMenu } from "./TabAddMenu";
import { TabBar } from "./TabBar";
import { TabContent } from "./TabContent";

interface TabContainerProps {
  /** App-provided panel renderer for a given tab. */
  renderTab: (tab: TabItem) => ReactNode;
  /** Active session tab id — forwarded to the tab bar for workspace-scoped
   *  file operations in the file-tab context menu. */
  workspaceTabId?: string;
}

export function TabContainer({ renderTab, workspaceTabId }: TabContainerProps) {
  const tabs = useActivityBarStore((s) => s.tabs);
  const activeTabId = useActivityBarStore((s) => s.activeTabId);
  const addMenuOpen = useActivityBarStore((s) => s.addMenuOpen);
  const addTab = useActivityBarStore((s) => s.addTab);
  const closeTab = useActivityBarStore((s) => s.closeTab);
  const activateTab = useActivityBarStore((s) => s.activateTab);
  const moveTab = useActivityBarStore((s) => s.moveTab);
  const setAddMenuOpen = useActivityBarStore((s) => s.setAddMenuOpen);
  const addButtonRef = useRef<HTMLButtonElement>(null);
  const activeTab = tabs.find((tab) => tab.id === activeTabId) ?? null;
  // Closing the add menu on outside click / Escape lives inside TabAddMenu
  // (it knows its own bounds); the panel itself must not swallow those clicks.

  // Every panel type can be added repeatedly, so the pick handler always
  // appends a fresh tab (no dedup via openEntry).
  const handlePickTab = useCallback(
    (type: TabType, label: string) => {
      addTab(type, label);
    },
    [addTab],
  );

  return (
    <div className="tab-container">
      <TabBar
        tabs={tabs}
        activeTabId={activeTabId}
        onActivate={activateTab}
        onClose={closeTab}
        onMoveTab={moveTab}
        onAdd={() => setAddMenuOpen(!addMenuOpen)}
        addButtonRef={addButtonRef}
        workspaceTabId={workspaceTabId}
      />
      {addMenuOpen && <TabAddMenu anchorRef={addButtonRef} onPick={handlePickTab} onClose={() => setAddMenuOpen(false)} />}
      <div className="tab-container__content">
        <TabContent activeTab={activeTab} renderTab={renderTab} />
      </div>
    </div>
  );
}
