import { useCallback, useEffect, useState } from "react";
import { FileText, Folder, Image } from "lucide-react";
import { app } from "../lib/bridge";
import { sortDisplayAttachments, type DisplayAttachment } from "../lib/attachmentDisplay";
import { useT } from "../lib/i18n";
import { ImageViewer } from "./ImageViewer";
import { Tooltip } from "./Tooltip";

function loadPreview(tabId: string, attachment: DisplayAttachment): Promise<string> {
  const { path } = attachment;
  if (path.startsWith("draft:") && typeof app.ReadDraftImageForTab === "function") {
    return app.ReadDraftImageForTab(tabId, path.slice("draft:".length));
  }
  if (path.startsWith("attachment:") && typeof app.ReadSessionAttachmentForTab === "function") {
    const digest = path.slice("attachment:".length).slice(0, 64);
    const mime = attachment.mime || (attachment.ext === "JPG" || attachment.ext === "JPEG" ? "image/jpeg" : `image/${attachment.ext.toLowerCase() || "png"}`);
    return import("../lib/sessionAttachmentRead").then(module => module.readSessionAttachmentDataURL(
      tabId,
      digest,
      mime,
      (id, nextDigest, offset) => app.ReadSessionAttachmentForTab!(id, nextDigest, offset),
    ));
  }
  return app.AttachmentDataURLForTab(tabId, path);
}

function attachmentIcon(kind: DisplayAttachment["kind"]) {
  if (kind === "image") return <Image size={15} />;
  if (kind === "folder") return <Folder size={15} />;
  return <FileText size={15} />;
}

export function MessageAttachments({ attachments, tabId = "" }: {
  attachments: DisplayAttachment[];
  tabId?: string;
}) {
  const t = useT();
  const ordered = sortDisplayAttachments(attachments);
  const [previews, setPreviews] = useState<Record<string, string>>({});
  const [viewer, setViewer] = useState({ open: false, url: "", name: "" });
  const openViewer = useCallback(async (attachment: DisplayAttachment) => {
    let url = previews[attachment.path];
    if (!url) {
      try {
        url = await loadPreview(tabId, attachment);
        setPreviews(previous => previous[attachment.path] ? previous : { ...previous, [attachment.path]: url });
      } catch { return; }
    }
    setViewer({ open: true, url, name: attachment.name });
  }, [previews, tabId]);

  const previewKey = ordered.filter(item => item.kind === "image" && item.source === "attachment").map(item => item.path).join("\n");
  useEffect(() => {
    let cancelled = false;
    for (const path of previewKey ? previewKey.split("\n") : []) {
      if (previews[path]) continue;
      const attachment = ordered.find(item => item.path === path);
      if (!attachment) continue;
      loadPreview(tabId, attachment).then(url => {
        if (!cancelled) setPreviews(previous => previous[path] ? previous : { ...previous, [path]: url });
      }).catch(() => {});
    }
    return () => { cancelled = true; };
  }, [previewKey, tabId]);

  return (
    <div className="msg-attachments" aria-label={t("msg.attachments")} data-transcript-selection-ignore>
      {ordered.map((attachment, index) => {
        const image = attachment.kind === "image";
        const card = (
          <div
            className={`msg-attachment msg-attachment--${attachment.kind}`}
            key={image ? undefined : `${attachment.path}:${index}`}
            title={image ? undefined : attachment.path}
            onClick={image ? () => openViewer(attachment) : undefined}
            role={image ? "button" : undefined}
            tabIndex={image ? 0 : undefined}
            onKeyDown={image ? event => {
              if (event.key === "Enter" || event.key === " ") { event.preventDefault(); openViewer(attachment); }
            } : undefined}
          >
            <span className={`msg-attachment__icon msg-attachment__icon--${attachment.kind}`} aria-hidden="true">
              {image && previews[attachment.path] ? <img src={previews[attachment.path]} alt="" draggable={false} /> : attachmentIcon(attachment.kind)}
            </span>
            <span className="msg-attachment__main">
              <span className="msg-attachment__name">{attachment.name}</span>
              <span className="msg-attachment__meta">
                {attachment.kind === "folder"
                  ? t("msg.folderReference")
                  : `${attachment.ext || t("msg.fileAttachment")} · ${attachment.source === "workspace" ? t("msg.workspaceReference") : image ? t("msg.imageAttachment") : t("msg.fileAttachment")}`}
              </span>
            </span>
          </div>
        );
        return image ? <Tooltip key={`${attachment.path}:${index}`} label={t("imageViewer.clickToPreview")} block>{card}</Tooltip> : card;
      })}
      <ImageViewer open={viewer.open} imageUrl={viewer.url} imageName={viewer.name} onClose={() => setViewer(value => value.open ? { ...value, open: false } : value)} />
    </div>
  );
}
