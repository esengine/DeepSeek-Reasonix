import { Copy } from "lucide-react";
import type { FloatingMenuItem } from "../components/FloatingMenu";
import { writeClipboardText } from "./clipboard";

export const WORKSPACE_CONTEXT_MENU_FILE_HEIGHT = 204;
export const WORKSPACE_CONTEXT_MENU_REF_HEIGHT = 160;

export function workspacePathCopyMenuItems({
  path,
  resolveAbsolutePath,
  isScopeCurrent,
  close,
  relativeLabel,
  absoluteLabel,
}: {
  path: string;
  resolveAbsolutePath: () => Promise<string>;
  isScopeCurrent: () => boolean;
  close: () => void;
  relativeLabel: string;
  absoluteLabel: string;
}): FloatingMenuItem[] {
  return [
    {
      icon: <Copy size={14} />,
      label: relativeLabel,
      onSelect: () => {
        const relativePath = path.replace(/\/+$/, "");
        close();
        if (relativePath) void writeClipboardText(relativePath);
      },
    },
    {
      icon: <Copy size={14} />,
      label: absoluteLabel,
      onSelect: () => {
        close();
        void resolveAbsolutePath()
          .then((absolutePath) => {
            if (absolutePath && isScopeCurrent()) return writeClipboardText(absolutePath);
          })
          .catch(() => {
            // The path may disappear between opening the menu and selecting it.
          });
      },
    },
  ];
}
