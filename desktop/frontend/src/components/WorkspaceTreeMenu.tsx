import { ExternalLink, FileText, FolderOpen, MessageSquarePlus, TerminalSquare } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import {
  WORKSPACE_CONTEXT_MENU_FILE_HEIGHT,
  WORKSPACE_CONTEXT_MENU_REF_HEIGHT,
  workspacePathCopyMenuItems,
} from "../lib/workspacePathCopyMenuItems";
import { FloatingMenu, FloatingMenuItems } from "./FloatingMenu";

export interface WorkspaceTreeMenuTarget {
  x: number;
  y: number;
  path: string;
  isDir: boolean;
}

export function WorkspaceTreeMenu({
  target,
  workspaceTabId,
  isScopeCurrent,
  onClose,
  onOpenInTerminal,
  onAddReference,
  onAddFile,
}: {
  target: WorkspaceTreeMenuTarget;
  workspaceTabId: string;
  isScopeCurrent: () => boolean;
  onClose: () => void;
  onOpenInTerminal?: (path: string) => void;
  onAddReference: () => void;
  onAddFile: () => void;
}) {
  const t = useT();
  const closeThen = (action: () => void) => {
    onClose();
    action();
  };

  return (
    <FloatingMenu
      x={target.x}
      y={target.y}
      estimatedHeight={target.isDir ? WORKSPACE_CONTEXT_MENU_REF_HEIGHT : WORKSPACE_CONTEXT_MENU_FILE_HEIGHT}
      className="workspace-tree-menu"
    >
      <FloatingMenuItems
        items={[
          ...(target.isDir
            ? []
            : [{
                icon: <ExternalLink size={14} />,
                label: t("workspace.openWithDefaultApp"),
                onSelect: () => closeThen(() => void app.OpenWorkspacePathForTab(workspaceTabId, target.path).catch(() => {})),
              }]),
          {
            icon: <FolderOpen size={14} />,
            label: t("workspace.revealInFileManager"),
            onSelect: () => closeThen(() => void app.RevealWorkspacePathForTab(workspaceTabId, target.path).catch(() => {})),
          },
          ...(onOpenInTerminal
            ? [{
                icon: <TerminalSquare size={14} />,
                label: t("workspace.openInTerminal"),
                onSelect: () => closeThen(() => onOpenInTerminal(target.path)),
              }]
            : []),
          ...workspacePathCopyMenuItems({
            path: target.path,
            resolveAbsolutePath: () => app.ResolveWorkspacePathForTab(workspaceTabId, target.path),
            isScopeCurrent,
            close: onClose,
            relativeLabel: t("workspace.copyRelativePath"),
            absoluteLabel: t("workspace.copyAbsolutePath"),
          }),
          { separator: true },
          {
            icon: <MessageSquarePlus size={14} />,
            label: target.isDir ? t("workspace.addFolderReferenceToChat") : t("workspace.addFileReferenceToChat"),
            onSelect: onAddReference,
          },
          ...(target.isDir
            ? []
            : [{
                icon: <FileText size={14} />,
                label: t("workspace.addFileContentToChat"),
                onSelect: onAddFile,
              }]),
        ]}
      />
    </FloatingMenu>
  );
}
