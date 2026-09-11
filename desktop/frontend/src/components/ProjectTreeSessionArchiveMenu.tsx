import { Archive } from "lucide-react";
import { useT } from "../lib/i18n";
import { ContextMenu, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";

type ProjectTreeSessionArchiveMenuProps = {
  open: boolean;
  point: ContextMenuPoint | null;
  sessionPath: string;
  blocked: boolean;
  busy: boolean;
  confirmed: boolean;
  onConfirm: () => void;
  onTrash: () => void;
  onClose: () => void;
};

export function ProjectTreeSessionArchiveMenu({
  open, point, sessionPath, blocked, busy, confirmed, onConfirm, onTrash, onClose,
}: ProjectTreeSessionArchiveMenuProps) {
  const t = useT();
  const items: ContextMenuItem[] = [{
    key: "trash-session",
    icon: <Archive className={busy ? "project-tree__archive-spinner" : undefined} size={13} />,
    label: t(confirmed ? "history.confirmMoveToTrash" : "history.moveToTrash"),
    disabled: !sessionPath || blocked || busy,
    danger: true,
    onSelect: confirmed ? onTrash : onConfirm,
  }];
  return <ContextMenu open={open} point={point} items={items} minWidth={178} ariaLabel={t("projectTree.topicActions")} onClose={onClose} />;
}
