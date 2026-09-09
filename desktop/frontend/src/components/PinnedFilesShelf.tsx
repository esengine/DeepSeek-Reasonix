import { useCallback } from "react";
import { AlertTriangle, FileText, Pin, X } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { useToast } from "../lib/toast";
import type { PinnedFileInfo } from "../lib/pinnedContextBridge";

export function PinnedFilesShelf({
  tabId,
  pinnedFiles,
}: {
  tabId: string;
  pinnedFiles?: PinnedFileInfo[];
}) {
  const t = useT();
  const { showToast } = useToast();

  const handleUnpin = useCallback(
    (path: string, e: React.MouseEvent) => {
      e.stopPropagation();
      if (!tabId) return;
      void app.UnpinFileForTab(tabId, path).catch((err: unknown) => {
        showToast(String((err as Error)?.message || err), "error");
      });
    },
    [tabId, showToast],
  );

  const handleOpenFile = useCallback(
    (path: string) => {
      if (!tabId) return;
      void app.OpenWorkspacePathForTab(tabId, path).catch(() => {});
    },
    [tabId],
  );

  if (!pinnedFiles || pinnedFiles.length === 0) {
    return null;
  }

  return (
    <div
      className="pinned-files-shelf flex items-center flex-wrap gap-1.5 px-3 py-1.5 border-b border-border/40 bg-surface-raised/40 text-xs select-none transition-all"
      role="region"
      aria-label={t("pinnedFiles.shelfTitle")}
    >
      <div className="flex items-center gap-1 text-muted font-medium mr-1 text-[11px]">
        <Pin size={12} className="text-accent" />
        <span>{t("pinnedFiles.shelfTitle")}</span>
      </div>
      {pinnedFiles.map((file) => {
        const basename = file.path.split("/").pop() || file.path;
        return (
          <div
            key={file.path}
            onClick={() => handleOpenFile(file.path)}
            className={`group flex items-center gap-1.5 px-2 py-0.5 rounded-full bg-surface hover:bg-surface-hover border text-foreground cursor-pointer transition-colors shadow-2xs ${file.error ? "border-red-500/60" : "border-border/60"}`}
            title={file.error || `${file.path} (${file.sizeBytes} B · ~${file.tokenEstimate} tok)`}
          >
            {file.error ? (
              <AlertTriangle size={11} className="text-red-500" aria-label={file.error} />
            ) : (
              <FileText size={11} className="text-muted-foreground group-hover:text-foreground transition-colors" />
            )}
            <span className="font-mono text-[11px] max-w-[160px] truncate">{basename}</span>
            {file.tokenEstimate > 0 && (
              <span className="text-[10px] text-muted-foreground">
                ~{file.tokenEstimate}t
              </span>
            )}
            <button
              type="button"
              onClick={(e) => handleUnpin(file.path, e)}
              className="p-0.5 hover:bg-surface-raised rounded-full text-muted-foreground hover:text-foreground transition-colors"
              aria-label={`${t("pinnedFiles.unpin")} ${basename}`}
            >
              <X size={11} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
