import { useEffect, useRef } from "react";
import { FileDown, FileImage, FileJson, FileText } from "lucide-react";
import { t } from "../lib/i18n";
import type { TopicbarSessionActionsProps } from "./TopicbarSessionActions";

export function TopicbarExportMenu({ initialFocus, exportSession, onClose }: {
  initialFocus: "first" | "last";
  exportSession: TopicbarSessionActionsProps["exportSession"];
  onClose: (restoreFocus: boolean) => void;
}) {
  const menuRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const items = menuRef.current?.querySelectorAll<HTMLButtonElement>("button");
    if (items) (initialFocus === "last" ? items[items.length - 1] : items[0])?.focus();
  }, [initialFocus]);

  return (
    <div ref={menuRef} className="topicbar__export-menu" role="menu" aria-label={t("topicBar.export")}
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          event.stopPropagation();
          onClose(true);
          return;
        }
        if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
        event.preventDefault();
        event.stopPropagation();
        const items = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>("button"));
        const current = items.indexOf(document.activeElement as HTMLButtonElement);
        const next = event.key === "Home" ? 0 : event.key === "End" ? items.length - 1
          : (current + (event.key === "ArrowDown" ? 1 : -1) + items.length) % items.length;
        items[next]?.focus();
      }}
    >
      {([
        ["markdown", FileText, t("topicBar.exportMarkdown")],
        ["json", FileJson, t("topicBar.exportJson")],
        ["pdf", FileDown, t("topicBar.exportPdf")],
        ["image", FileImage, t("topicBar.exportImage")],
      ] as const).map(([format, Icon, label]) => (
        <button key={format} tabIndex={-1} type="button" role="menuitem" onClick={() => {
          onClose(true);
          exportSession(format);
        }}>
          <Icon size={13} /><span>{label}</span>
        </button>
      ))}
    </div>
  );
}
