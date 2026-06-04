// TabBar renders the browser-like workspace tab strip. Each tab represents one
// open project/global topic, so switching tabs switches the active conversation.
import { useState } from "react";
import { X, Plus } from "lucide-react";
import type { TabMeta } from "../lib/types";
import { Tooltip } from "./Tooltip";

interface TabBarProps {
  tabs: TabMeta[];
  activeTabId?: string;
  onTabChange: (tabId: string) => void;
  onTabClose: (tabId: string) => void;
  onNewTab: () => void;
}

export function TabBar({ tabs, activeTabId, onTabChange, onTabClose, onNewTab }: TabBarProps) {
  const [confirmingClose, setConfirmingClose] = useState<string | null>(null);

  const handleClose = (tabId: string) => {
    if (confirmingClose !== tabId) {
      setConfirmingClose(tabId);
      return;
    }
    setConfirmingClose(null);
    onTabClose(tabId);
  };

  return (
    <div className="tabbar">
      <div className="tabbar__tabs">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            className={`tabbar__tab${tab.id === activeTabId || tab.active ? " tabbar__tab--active" : ""}`}
            onClick={() => onTabChange(tab.id)}
          >
            <span className={`tabbar__status${tab.running ? " tabbar__status--running" : ""}`} />
            <span className="tabbar__tab-label">
              {tab.scope === "global" ? "Global" : `${tab.workspaceName || "Project"} · ${tab.topicTitle || "Untitled"}`}
            </span>
            {confirmingClose === tab.id ? (
              <span
                className="tabbar__tab-close tabbar__tab-close--confirm"
                onClick={(e) => {
                  e.stopPropagation();
                  handleClose(tab.id);
                }}
              >
                <X size={10} />
              </span>
            ) : (
              <span
                className="tabbar__tab-close"
                onClick={(e) => {
                  e.stopPropagation();
                  handleClose(tab.id);
                }}
              >
                <X size={10} />
              </span>
            )}
          </button>
        ))}
      </div>
      <Tooltip label="Open project or topic">
        <button className="tabbar__new" onClick={onNewTab}>
          <Plus size={13} />
        </button>
      </Tooltip>
    </div>
  );
}
