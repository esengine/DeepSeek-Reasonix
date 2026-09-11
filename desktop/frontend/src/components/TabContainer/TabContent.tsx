// TabContent renders the active tab's panel inside the tab container. Panel
// content needs the shell's props (context, usage, workspace state…), so the
// component takes a renderer callback: the shell provides `renderTab(tab)`,
// and TabContent routes the active tab through it, falling back to the tab
// picker when the dock holds no tab at all.

import type { ReactNode } from "react";
import { DockTabPicker } from "./DockTabPicker";
import type { TabItem } from "../../store/activityBar";

interface TabContentProps {
  activeTab: TabItem | null;
  /** App-provided panel renderer for the active tab. */
  renderTab: (tab: TabItem) => ReactNode;
  /** Opens (or activates) the view an empty-state entry stands for. */
  onPickEntry: (entryId: string) => void;
}

export function TabContent({ activeTab, renderTab, onPickEntry }: TabContentProps) {
  if (!activeTab) {
    return <DockTabPicker onSelect={onPickEntry} />;
  }
  return <>{renderTab(activeTab)}</>;
}
