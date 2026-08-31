// TabContent renders the active tab's panel inside the tab container. Panel
// content needs App's props (context, usage, workspace state…), so the
// component takes a renderer callback: App provides `renderTab(tab)`, and
// TabContent just routes the active tab through it, falling back to the
// empty-state hint when no tab is active.

import type { ReactNode } from "react";
import { useT } from "../../lib/i18n";
import type { TabItem } from "../../store/activityBar";

interface TabContentProps {
  activeTab: TabItem | null;
  /** App-provided panel renderer for the active tab. */
  renderTab: (tab: TabItem) => ReactNode;
}

export function TabContent({ activeTab, renderTab }: TabContentProps) {
  const t = useT();
  if (!activeTab) {
    return <div className="tab-container__empty">{t("rightDock.empty")}</div>;
  }
  return <>{renderTab(activeTab)}</>;
}
