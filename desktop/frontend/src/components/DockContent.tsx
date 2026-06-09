import { BrowserPanel } from "./BrowserPanel";
import { GitPanel } from "./GitPanel";
import { MemoryDockPanel } from "./MemoryDockPanel";
import { WorkspacePanel } from "./WorkspacePanel";
import type { DockTab } from "../lib/types";

interface DockContentProps {
  tab: DockTab;
  open: boolean;
  cwd?: string;
  maximized: boolean;
  panelWidth?: number;
  onClose: () => void;
  onToggleMaximized: () => void;
  onPreviewModeChange?: (active: boolean) => void;
  onAddToChat?: (text: string) => void;
  onRequestPanelWidth?: (width: number) => void;
  refreshKey?: number;
  onMetadataUpdate: (id: string, meta: Record<string, unknown>) => void;
  onTitleUpdate: (id: string, title: string) => void;
}

export function DockContent({ tab, onMetadataUpdate, onTitleUpdate, ...workspaceProps }: DockContentProps) {
  switch (tab.type) {
    case "files":
      return (
        <WorkspacePanel
          key="files"
          initialViewMode="files"
          showViewTabs={false}
          {...workspaceProps}
        />
      );
    case "changes":
      return (
        <WorkspacePanel
          key="changes"
          initialViewMode="changed"
          showViewTabs={false}
          {...workspaceProps}
        />
      );
    case "browser":
      return (
        <BrowserPanel
          tab={tab}
          onMetadataUpdate={onMetadataUpdate}
          onTitleUpdate={onTitleUpdate}
        />
      );
    case "context":
      return <div className="workspace-empty">Context panel</div>;
    case "memory":
      return <MemoryDockPanel />;
    case "commit":
      return <GitPanel />;
    default:
      return <div className="workspace-empty">Unknown panel type</div>;
  }
}
