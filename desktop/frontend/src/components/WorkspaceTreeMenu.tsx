import { useEffect, useState } from "react";
import { ExternalLink, FileText, FolderOpen, MessageSquarePlus, Pin, PinOff, TerminalSquare } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { useToast } from "../lib/toast";
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
  const { showToast } = useToast();
  const [isPinned, setIsPinned] = useState(false);

  useEffect(() => {
    if (target.isDir || !workspaceTabId) return;
    let active = true;
    app.GetPinnedFilesForTab(workspaceTabId)
      .then((files) => {
        if (!active || !Array.isArray(files)) return;
        const normalizedTarget = target.path.replace(/^[/\\]+/, "").replace(/\\/g, "/");
        const found = files.some(
          (f) => f.path.replace(/^[/\\]+/, "").replace(/\\/g, "/") === normalizedTarget
        );
        setIsPinned(found);
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, [workspaceTabId, target.path, target.isDir]);

  const closeThen = (action: () => void) => {
    onClose();
    action();
  };

  return (
    <FloatingMenu
      x={target.x}
      y={target.y}
      estimatedHeight={target.isDir ? WORKSPACE_CONTEXT_MENU_REF_HEIGHT : WORKSPACE_CONTEXT_MENU_FILE_HEIGHT + 32}
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
            : [
                {
                  icon: <FileText size={14} />,
                  label: t("workspace.addFileContentToChat"),
                  onSelect: onAddFile,
                },
                {
                  icon: isPinned ? <PinOff size={14} /> : <Pin size={14} />,
                  label: isPinned ? t("workspace.unpinFileFromContext") : t("workspace.pinFileToContext"),
                  onSelect: () =>
                    closeThen(() => {
                      if (isPinned) {
                        void app.UnpinFileForTab(workspaceTabId, target.path).catch((err: unknown) => {
                          showToast(String((err as Error)?.message || err), "error");
                        });
                      } else {
                        void app.PinFileForTab(workspaceTabId, target.path).catch((err: unknown) => {
                          showToast(String((err as Error)?.message || err), "error");
                        });
                      }
                    }),
                },
              ]),
        ]}
      />
    </FloatingMenu>
  );
}
