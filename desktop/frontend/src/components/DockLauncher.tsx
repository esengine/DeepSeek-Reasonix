// DockLauncher is the floating menu shown over the transcript's top-right
// corner while the right dock is collapsed. It lists the dock's entry points
// (files / changed / overview); clicking one expands the dock to that panel.
// Once the dock is open the launcher itself is hidden (the panel replaces
// it), so the menu only ever appears in the collapsed state.

import { useT } from "../lib/i18n";
import { Activity, FileText, GitBranch } from "lucide-react";
import type { ComponentType } from "react";
import type { TabType } from "../store/activityBar";
import { ACTIVITY_BAR_ENTRIES } from "./ActivityBar/activityBarConfig";

const ENTRY_ICONS: Record<TabType, ComponentType<{ size?: number | string; className?: string }>> = {
  file: FileText,
  changed: GitBranch,
  context: Activity,
  // Entries beyond the base three are not exposed yet; keep a stub so the
  // map stays total if a future config adds them.
  remote: FileText,
  instructions: FileText,
  terminal: FileText,
  browser: FileText,
};

interface DockLauncherProps {
  onSelect: (entryId: string) => void;
}

export function DockLauncher({ onSelect }: DockLauncherProps) {
  const t = useT();
  return (
    <div className="dock-launcher" role="toolbar" aria-label={t("rightDock.launcher")}>
      {ACTIVITY_BAR_ENTRIES.map((entry) => {
        const Icon = ENTRY_ICONS[entry.defaultTab];
        return (
          <button
            key={entry.id}
            type="button"
            className="dock-launcher__entry"
            aria-label={t(entry.labelKey as never)}
            onClick={() => onSelect(entry.id)}
          >
            <Icon size={14} />
            <span>{t(entry.labelKey as never)}</span>
          </button>
        );
      })}
    </div>
  );
}
