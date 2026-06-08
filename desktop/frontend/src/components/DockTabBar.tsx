import { Plus, X } from "lucide-react";
import type { ReactNode } from "react";
import { useT } from "../lib/i18n";
import type { DockTab } from "../lib/types";

interface DockTabBarProps {
  tabs: DockTab[];
  activeId: string;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
  onAdd: () => void;
  rightExtra?: ReactNode;
}

export function DockTabBar({ tabs, activeId, onSelect, onClose, onAdd, rightExtra }: DockTabBarProps) {
  const t = useT();
  return (
    <div className="dock-tabbar">
      <div className="dock-tabbar__tabs" role="tablist" aria-label={t("rightDock.views")}>
        {tabs.map((tab) => {
          const IconComp = tab.icon;
          return (
            <button
              key={tab.id}
              type="button"
              role="tab"
              aria-selected={tab.id === activeId}
              className={`dock-tabbar__tab${tab.id === activeId ? " dock-tabbar__tab--active" : ""}`}
              onClick={() => onSelect(tab.id)}
            >
              <IconComp size={13} />
              <span className="dock-tabbar__tab-label">{tab.title}</span>
              {tab.closable && (
                <span
                  className="dock-tabbar__tab-close"
                  role="button"
                  tabIndex={-1}
                  aria-label={t("rightDock.closeTab")}
                  onClick={(e) => {
                    e.stopPropagation();
                    onClose(tab.id);
                  }}
                >
                  <X size={12} />
                </span>
              )}
            </button>
          );
        })}
      </div>
      <button
        className="dock-tabbar__add"
        type="button"
        aria-label={t("rightDock.addTab")}
        title={t("rightDock.addTab")}
        onClick={onAdd}
      >
        <Plus size={14} />
      </button>
      {rightExtra}
    </div>
  );
}
