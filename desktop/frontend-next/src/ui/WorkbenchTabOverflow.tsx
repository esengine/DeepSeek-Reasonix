import { useState } from "react";
import { StudioIcon } from "./StudioIcon";

/** "+n ⌄" at the end of a strip too narrow for its tabs: the ones it cannot show, to pick from. */
export function WorkbenchTabOverflow({ tabs, onPick }: { tabs: { key: string; label: string }[]; onPick: (key: string) => void }) {
  const [open, setOpen] = useState(false);
  if (tabs.length === 0) return null;
  return (
    <span className="workbench-overflow" data-action-blur="workbench.tab-overflow" data-action-keydown="workbench.tab-overflow"
      onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false); }}
      onKeyDown={(e) => { if (e.key === "Escape" && open) { e.stopPropagation(); setOpen(false); } }}>
      <button type="button" className="workbench-overflow-btn" data-action="workbench.tab-overflow" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)}>
        +{tabs.length}
        <StudioIcon name="down" />
      </button>
      {open && (
        <div className="workbench-overflow-menu" role="menu">
          {tabs.map((tab) => (
            <button key={tab.key} type="button" role="menuitem" data-action="workbench.tab" data-target={tab.key} onClick={() => { setOpen(false); onPick(tab.key); }}>
              {tab.label}
            </button>
          ))}
        </div>
      )}
    </span>
  );
}
