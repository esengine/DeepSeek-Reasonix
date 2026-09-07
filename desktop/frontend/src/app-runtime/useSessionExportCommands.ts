import { useEffect } from "react";
import { app } from "../lib/bridge";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { safeFilename } from "../lib/sessionTitles";
import { applyThemeScene } from "../lib/themePack";
import { useOverlayStore } from "../store/overlays";
import type { Translator } from "../lib/i18n";
import type { Item, LiveStream } from "../lib/useController";

export type SessionExportFormat = "markdown" | "json" | "pdf" | "image";

/**
 * Owns the session export commands (markdown/json/pdf/image file pickers and
 * writers), the export popover outside-click close and the theme scene that
 * switches between the empty home and the content task scene. Each command
 * captures the session title/items/live snapshot of the render that published
 * it; the renderer chunks stay lazy behind the file dialog.
 */
export function useSessionExportCommands(input: {
  sessionTitle: string;
  items: readonly Item[];
  live: LiveStream | undefined;
  hasContent: boolean;
  t: Translator;
  showToast: (message: string, kind: "info" | "warn" | "error", options?: { durationMs?: number }) => void;
}) {
  const { sessionTitle, items, live, hasContent, t, showToast } = input;
  const topicExportOpen = useOverlayStore((state) => state.topicExportOpen);
  const setTopicExportOpen = useOverlayStore((state) => state.setTopicExportOpen);

  // Theme pack scene: home when the session is empty, task once content exists.
  useEffect(() => {
    applyThemeScene(hasContent ? "task" : "home");
  }, [hasContent]);

  useEffect(() => {
    if (!topicExportOpen) return;
    const onDown = (event: MouseEvent) => {
      const target = event.target as Element | null;
      if (!target?.closest(".topicbar__export")) setTopicExportOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [setTopicExportOpen, topicExportOpen]);

  const getSessionMarkdown = useCommittedCommand(async () => (await import("../lib/sessionExportData")).sessionItemsToMarkdown(sessionTitle, Array.from(items), live));
  const getSessionJson = useCommittedCommand(async () => (await import("../lib/sessionExportData")).sessionItemsToJson(sessionTitle, Array.from(items), live));

  const exportSession = useCommittedCommand(async (format: SessionExportFormat) => {
    const base = safeFilename(sessionTitle);
    setTopicExportOpen(false);
    try {
      if (format === "json") {
        const path = await app.PickExportFile(`${base}.json`, "application/json");
        if (path) {
          await app.SaveExportFile(path, await getSessionJson(), false);
          showToast(t("topicBar.exportSuccess", { count: 1 }), "info");
        }
      } else if (format === "pdf") {
        const path = await app.PickExportFile(`${base}.pdf`, "application/pdf");
        if (!path) return;
        const { blobToBase64, renderSessionPdfBlob } = await import("../lib/sessionExport");
        const blob = await renderSessionPdfBlob(await getSessionMarkdown(), sessionTitle);
        await app.SaveExportFile(path, await blobToBase64(blob), true);
        showToast(t("topicBar.exportSuccess", { count: 1 }), "info");
      } else if (format === "image") {
        const path = await app.PickExportFile(`${base}.png`, "image/png");
        if (!path) return;
        const { renderSessionImageBase64Payloads } = await import("../lib/sessionExport");
        const payloads = await renderSessionImageBase64Payloads(await getSessionMarkdown());
        await app.SaveExportImageFiles(path, payloads);
        showToast(
          payloads.length > 1
            ? t("topicBar.exportImageParts", { count: payloads.length })
            : t("topicBar.exportSuccess", { count: 1 }),
          "info",
        );
      } else {
        const path = await app.PickExportFile(`${base}.md`, "text/markdown");
        if (path) {
          await app.SaveExportFile(path, await getSessionMarkdown(), false);
          showToast(t("topicBar.exportSuccess", { count: 1 }), "info");
        }
      }
    } catch (err) {
      console.error("Failed to export session", err);
      showToast(
        t("topicBar.exportFailed", { error: err instanceof Error ? err.message : String(err) }),
        "error",
        { durationMs: 8000 },
      );
    }
  });

  return { getSessionMarkdown, getSessionJson, exportSession };
}
