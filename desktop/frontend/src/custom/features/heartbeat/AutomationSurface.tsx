import type { PropsWithChildren } from "react";
import { PanelRight } from "lucide-react";

interface AutomationSurfaceProps extends PropsWithChildren {
  onToggleSidebar?: () => void;
  sidebarLabel: string;
}

export function AutomationSurface({ children, onToggleSidebar, sidebarLabel }: AutomationSurfaceProps) {
  return (
    <div className="automation-surface">
      <button
        className="automation-sidebar-toggle"
        type="button"
        onClick={onToggleSidebar}
        aria-label={sidebarLabel}
        title={sidebarLabel}
      >
        <PanelRight size={15} aria-hidden="true" />
      </button>
      <div className="automation-surface__drag-region" aria-hidden="true" />
      {children}
    </div>
  );
}
