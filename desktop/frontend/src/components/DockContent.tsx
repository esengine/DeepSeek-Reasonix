import { BrowserPanel } from "./BrowserPanel";
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
          mode="files"
          {...workspaceProps}
        />
      );
    case "changes":
      return (
        <WorkspacePanel
          key="changes"
          mode="changes"
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
    default:
      return <div className="workspace-empty">Unknown panel type</div>;
  }
}
