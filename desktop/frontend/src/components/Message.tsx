import { createContext, lazy, memo, Suspense, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { BrainCircuit, FileText, Folder, Image, MessageSquare } from "lucide-react";
import { Markdown } from "./Markdown";
import { CopyButton } from "./CopyButton";
import { parseAttachmentRefsForDisplay, sortDisplayAttachments } from "../lib/attachmentDisplay";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { ImageViewer } from "./ImageViewer";
import { Tooltip } from "./Tooltip";
import { stripMemoryCompilerExecution } from "../lib/memoryCompilerDisplay";
import { invocationSegmentsFromMessage, type InvocationMetadataMap } from "../lib/invocationDisplay";
import type { Item } from "../lib/useController";
import { InvocationBadge } from "./InvocationBadge";
import { CodeViewer } from "./CodeViewer";
import { formatSelectionLabels, languageFor, parseSelectedTextContext, stripSelectionLabels } from "../lib/selectedTextContext";
import type { PresentedFileView } from "../lib/chatViewSource";
import { PresentedFileLinkProvider } from "./PresentedFileLinkContext";

const MemoryCitations = lazy(() => import("./MemoryCitations").then((module) => ({ default: module.MemoryCitations })));
const SearchSourcesPanel = lazy(() => import("./SearchSourcesPanel").then((module) => ({ default: module.SearchSourcesPanel }))); type AssistantItem = Extract<Item, { kind: "assistant" }>;
export const InvocationMetadataContext = createContext<InvocationMetadataMap>({});
type ImSourceMessage = {
  provider: string;
  label: string;
  sender: string;
  chat: string;
  text: string;
};

const IM_SOURCE_START = "[[reasonix-im]]";
const IM_SOURCE_END = "[[/reasonix-im]]";

function parseImSourceMessage(text: string): ImSourceMessage | null {
  // Display-only metadata: keep IM sender/chat details out of model prompts.
  if (!text.startsWith(IM_SOURCE_START)) return null;
  const end = text.indexOf(IM_SOURCE_END);
  if (end < 0) return null;
  const metaBlock = text.slice(IM_SOURCE_START.length, end).trim();
  const body = text.slice(end + IM_SOURCE_END.length).replace(/^\r?\n/, "");
  const meta: Record<string, string> = {};
  for (const line of metaBlock.split(/\r?\n/)) {
    const index = line.indexOf("=");
    if (index <= 0) continue;
    const key = line.slice(0, index).trim().toLowerCase();
    const value = line.slice(index + 1).trim();
    if (key) meta[key] = value;
  }
  return {
    provider: meta.provider || "",
    label: meta.label || "",
    sender: meta.sender || meta.senderid || "",
    chat: meta.chat || meta.chat_type || "",
    text: body,
  };
}

function imSourceLabel(source: ImSourceMessage, t: ReturnType<typeof useT>): string {
  if (source.label.trim()) return source.label.trim();
  const provider = source.provider.trim().toLowerCase();
  if (provider === "lark") return "Lark";
  if (provider === "weixin" || provider === "wechat") return t("settings.botWeixin");
  return t("settings.botFeishu");
}

function attachmentIcon(kind: "image" | "file" | "folder") {
  if (kind === "image") return <Image size={15} />;
  if (kind === "folder") return <Folder size={15} />;
  return <FileText size={15} />;
}

type PastedBlockInfo = {
  label: string;
  content: string;
};

const PASTE_LABEL_RE = /\[(?:已粘贴文本|已貼上文字|Pasted text) #\d+ · \d+ (?:行|lines)\]/g;

export function parsePastedBlocks(text: string, submitText?: string): PastedBlockInfo[] {
  const labels = text.match(PASTE_LABEL_RE);
  if (!labels || labels.length === 0 || !submitText) return [];
  const unique = [...new Set(labels)];
  const blocks: PastedBlockInfo[] = [];
  for (const label of unique) {
    const beginMarker = `--- Begin ${label} ---`;
    const endMarker = `--- End ${label} ---`;
    const beginIdx = submitText.indexOf(beginMarker);
    const endIdx = submitText.indexOf(endMarker);
    if (beginIdx < 0 || endIdx <= beginIdx) continue;
    const contentStart = beginIdx + beginMarker.length;
    const content = submitText.slice(contentStart, endIdx).replace(/^\r?\n/, "");
    blocks.push({ label, content });
  }
  return blocks;
}

export type SelectedTextBlockInfo = {
  label: string;
  content: string;
  path?: string;
  start: number;
  end: number;
  kind: "chat" | "code" | "terminal";
};

export function parseSelectedTextBlocks(text: string, submitText?: string): SelectedTextBlockInfo[] {
  const entries = parseSelectedTextContext(submitText);
  if (entries.length === 0) return [];
  const suffix = formatSelectionLabels(entries);
  if (!suffix || !text.endsWith(suffix)) return [];

  // Composer owns the exact trailing label suffix. Deriving it from the JSON
  // entries avoids consuming label-shaped or unterminated authored prose.
  let start = text.length - suffix.length;
  return entries.map((entry) => {
    const label = formatSelectionLabels([entry]);
    const kind = entry.path ? "code" : entry.source === "terminal" ? "terminal" : "chat";
    const block = {
      label,
      content: entry.text,
      path: entry.path,
      start,
      end: start + label.length,
      kind,
    } satisfies SelectedTextBlockInfo;
    start = block.end + 1;
    return block;
  });
}

function messageDate(value?: number): Date {
  return new Date(typeof value === "number" && Number.isFinite(value) && value > 0 ? value : Date.now());
}

function formatMessageTime(date: Date): string {
  const hours = String(date.getHours()).padStart(2, "0");
  const minutes = String(date.getMinutes()).padStart(2, "0");
  return `${hours}:${minutes}`;
}

export function UserMessage({
  text,
  submitText,
  failed,
  turn,
  anchorId,
  id,
  createdAt,
}: {
  text: string;
  submitText?: string;
  failed?: boolean;
  turn?: number;
  anchorId?: string;
  id?: string;
  createdAt?: number;
}) {
  const t = useT();
  const invocationMetadata = useContext(InvocationMetadataContext);
  const imSource = parseImSourceMessage(text);
  const actionText = stripMemoryCompilerExecution(imSource?.text ?? text);
  const hasMemoryCompiler = Boolean(submitText?.includes("<memory-compiler-execution>"));
  const selectedTextEntries = useMemo(() => parseSelectedTextContext(submitText), [submitText]);
  const editableActionText = stripSelectionLabels(actionText, selectedTextEntries);
  const { text: editableDisplayText, attachments } = parseAttachmentRefsForDisplay(editableActionText);
  const selectionLabels = formatSelectionLabels(selectedTextEntries);
  const displayText = [editableDisplayText, selectionLabels].filter(Boolean).join(editableDisplayText && selectionLabels ? " " : "");
  const invocationSegments = imSource ? [] : invocationSegmentsFromMessage(displayText, submitText, invocationMetadata);
  const hasInvocationSegments = invocationSegments.some((segment) => segment.type === "invocation");
  const orderedAttachments = sortDisplayAttachments(attachments);
  const sourceLabel = imSource ? imSourceLabel(imSource, t) : "";
  const sentAt = createdAt === undefined ? null : messageDate(createdAt);
  const [imagePreviews, setImagePreviews] = useState<Record<string, string>>({});
  const [imageViewer, setImageViewer] = useState<{ open: boolean; url: string; name: string }>({ open: false, url: "", name: "" });
  const openImageViewer = useCallback(async (path: string, name: string) => {
    let url = imagePreviews[path];
    if (!url) {
      try {
        url = await app.AttachmentDataURL(path);
        setImagePreviews((prev) => (prev[path] ? prev : { ...prev, [path]: url }));
      } catch {
        return;
      }
    }
    setImageViewer({ open: true, url, name });
  }, [imagePreviews]);

  const closeImageViewer = useCallback(() => {
    setImageViewer((prev) => (prev.open ? { ...prev, open: false } : prev));
  }, []);

  const pasteBlocks = useMemo(() => parsePastedBlocks(displayText, submitText), [displayText, submitText]);
  const selectedTextBlocks = useMemo(() => parseSelectedTextBlocks(displayText, submitText), [displayText, submitText]);
  const [expandedBlockKeys, setExpandedBlockKeys] = useState<Record<string, boolean>>({});

  type DisplaySegment =
    | { type: "text"; content: string }
    | { type: "block"; key: string; block: PastedBlockInfo; kind: "paste" }
    | { type: "block"; key: string; block: SelectedTextBlockInfo; kind: "chat" | "code" | "terminal" };

  const displaySegments = useMemo((): DisplaySegment[] => {
    if (pasteBlocks.length === 0 && selectedTextBlocks.length === 0) return [{ type: "text", content: displayText }];
    const segments: DisplaySegment[] = [];
    const ordered: Array<
      | { block: PastedBlockInfo; start: number; end: number; kind: "paste" }
      | { block: SelectedTextBlockInfo; start: number; end: number; kind: "chat" | "code" | "terminal" }
    > = [
      ...pasteBlocks.map((block) => {
        const start = displayText.indexOf(block.label);
        return { block, start, end: start + block.label.length, kind: "paste" as const };
      }),
      ...selectedTextBlocks.map((block) => ({ block, start: block.start, end: block.end, kind: block.kind })),
    ].filter((block) => block.start >= 0).sort((a, b) => a.start - b.start);
    let cursor = 0;
    for (const item of ordered) {
      if (item.start < cursor) continue;
      // Text before the label: strip the trailing newline that separated the
      // label from the preceding line so the card sits tight against the text.
      if (item.start > cursor) {
        let before = displayText.slice(cursor, item.start);
        before = before.replace(/\n$/, "");
        if (before) segments.push({ type: "text", content: before });
      }
      const key = `${item.kind}:${item.start}:${item.block.label}`;
      if (item.kind === "paste") {
        segments.push({ type: "block", key, block: item.block, kind: item.kind });
      } else {
        segments.push({ type: "block", key, block: item.block, kind: item.kind });
      }
      cursor = item.end;
    }
    // Strip the leading newline that followed the label.
    const remaining = displayText.slice(cursor).replace(/^\n/, "");
    if (remaining.trim()) segments.push({ type: "text", content: remaining });
    return segments.length > 0 ? segments : [{ type: "text", content: displayText }];
  }, [displayText, pasteBlocks, selectedTextBlocks]);

  const toggleBlockExpand = (key: string) => {
    setExpandedBlockKeys((prev) => ({
      ...prev,
      [key]: !prev[key],
    }));
  };
  const imagePreviewKey = orderedAttachments
    .filter((attachment) => attachment.kind === "image" && attachment.source === "attachment")
    .map((attachment) => attachment.path)
    .join("\n");

  useEffect(() => {
    const paths = imagePreviewKey ? imagePreviewKey.split("\n") : [];
    if (paths.length === 0) return;
    let cancelled = false;
    for (const path of paths) {
      if (imagePreviews[path]) continue;
      app.AttachmentDataURL(path)
        .then((url) => {
          if (cancelled) return;
          setImagePreviews((prev) => (prev[path] ? prev : { ...prev, [path]: url }));
        })
        .catch(() => {});
    }
    return () => {
      cancelled = true;
    };
  }, [imagePreviewKey]);
  return (
    <div
      className={`msg msg--user${imSource ? " msg--im-source" : ""}${failed ? " msg--user-failed" : ""}`}
      id={anchorId}
      data-question-anchor={anchorId}
      data-turn={turn}
      data-im-source={imSource?.provider || undefined}
      data-history-restore={id && id.startsWith("h") ? "" : undefined}
      data-entrance={id || undefined}
    >
      <div className="msg__body" data-transcript-selectable="message">
        {imSource ? (
          <div className="im-source-card">
            <div className="im-source-card__head" data-transcript-selection-ignore>
              <MessageSquare size={14} />
              <span>{t("msg.fromIm", { source: sourceLabel })}</span>
            </div>
            {displayText && <div className="im-source-card__text">{displayText}</div>}
            {(imSource.sender || imSource.chat) && (
              <div className="im-source-card__meta" data-transcript-selection-ignore>
                {imSource.sender && <span>{t("msg.imSender", { id: imSource.sender })}</span>}
                {imSource.chat && <span>{imSource.chat}</span>}
              </div>
            )}
          </div>
        ) : (
          <>
            {hasInvocationSegments && pasteBlocks.length === 0 && selectedTextBlocks.length === 0 ? (
              <div className="msg__text msg__rich-text">
                {invocationSegments.map((segment, index) => segment.type === "text"
                  ? <span key={`text:${segment.start}:${index}`}>{segment.content}</span>
                  : (
                    <InvocationBadge
                      key={`invocation:${segment.invocation.name}:${segment.offset}:${index}`}
                      invocation={segment.invocation}
                      kind={segment.invocation.kind}
                      variant="message"
                    />
                  ))}
              </div>
            ) : displaySegments.map((seg, i) => {
              if (seg.type === "text") {
                return seg.content ? <div className="msg__text" key={`s${i}`}>{seg.content}</div> : null;
              }
              const expanded = Boolean(expandedBlockKeys[seg.key]);
              return (
                <div className="msg-pasted" key={seg.key}>
                  <div className="msg-pasted-block">
                    <div className="msg-pasted-head" data-transcript-selection-ignore>
                      {seg.kind === "code" ? <FileText size={15} /> : <MessageSquare size={15} />}
                      <span className="msg-pasted-label">{seg.block.label}</span>
                      <div className="msg-pasted-actions">
                        <Tooltip label={t(expanded ? "msg.pastedCollapseTooltip" : "msg.pastedExpandTooltip")}>
                          <button type="button" onClick={() => toggleBlockExpand(seg.key)}>
                            {expanded ? t("common.collapse") : t("composer.pastedExpand")}
                          </button>
                        </Tooltip>
                      </div>
                    </div>
                    {expanded && (
                      <div className="msg-pasted-expanded">
                        {seg.kind === "chat"
                          ? <Markdown text={seg.block.content} />
                          : seg.kind === "code" || seg.kind === "terminal"
                            ? <CodeViewer value={seg.block.content} language={seg.kind === "terminal" ? "console" : languageFor(seg.block.path ?? "")} maxHeight={360} />
                            : seg.block.content}
                      </div>
                    )}
                  </div>
                </div>
              );
            })}
          </>
        )}
        {failed && <div className="msg__send-failed" data-transcript-selection-ignore>{t("msg.sendFailed")}</div>}
        {orderedAttachments.length > 0 && (
          <div className="msg-attachments" aria-label={t("msg.attachments")} data-transcript-selection-ignore>
            {orderedAttachments.map((attachment, index) => {
              const isImage = attachment.kind === "image";
              const el = (
                <div
                  className={`msg-attachment msg-attachment--${attachment.kind}`}
                  key={isImage ? undefined : `${attachment.path}:${index}`}
                  title={isImage ? undefined : attachment.path}
                  onClick={isImage ? () => openImageViewer(attachment.path, attachment.name) : undefined}
                  role={isImage ? "button" : undefined}
                  tabIndex={isImage ? 0 : undefined}
                  onKeyDown={isImage ? (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); openImageViewer(attachment.path, attachment.name); } } : undefined}
                >
                  <span className={`msg-attachment__icon msg-attachment__icon--${attachment.kind}`} aria-hidden="true">
                    {isImage && imagePreviews[attachment.path] ? <img src={imagePreviews[attachment.path]} alt="" draggable={false} /> : attachmentIcon(attachment.kind)}
                  </span>
                  <span className="msg-attachment__main">
                    <span className="msg-attachment__name">{attachment.name}</span>
                    <span className="msg-attachment__meta">
                      {attachment.kind === "folder"
                        ? t("msg.folderReference")
                        : `${attachment.ext || t("msg.fileAttachment")} · ${attachment.source === "workspace" ? t("msg.workspaceReference") : attachment.kind === "image" ? t("msg.imageAttachment") : t("msg.fileAttachment")}`}
                    </span>
                  </span>
                </div>
              );
              if (isImage) {
                return (
                  <Tooltip key={`${attachment.path}:${index}`} label={t("imageViewer.clickToPreview")} block>
                    {el}
                  </Tooltip>
                );
              }
              return el;
            })}
            <ImageViewer
              open={imageViewer.open}
              imageUrl={imageViewer.url}
              imageName={imageViewer.name}
              onClose={closeImageViewer}
            />
          </div>
        )}
      </div>
      <div className="msg-meta" role="group" aria-label={t("msg.copy")}>
          {sentAt && (
            <time className="msg-meta__time" dateTime={sentAt.toISOString()} title={sentAt.toLocaleString()}>
              {formatMessageTime(sentAt)}
            </time>
          )}
          {hasMemoryCompiler && (
            <span className="msg-meta__indicator" title={t("msg.memoryCompilerApplied")} aria-hidden="true">
              <BrainCircuit size={14} />
            </span>
          )}
          <CopyButton text={actionText} label={t("msg.copy")} showInlineLabel={false} className="msg-meta__btn msg-meta__copy" />
      </div>
    </div>
  );
}

export const AssistantMessage = memo(function AssistantMessage({ item, presentedFiles = [], tabId, hostId }: { item: AssistantItem; presentedFiles?: readonly PresentedFileView[]; tabId?: string; hostId?: string }) {
  const hasText = item.streaming || item.text.trim() !== "";
  const hasFootnotes = Boolean(item.searchSources?.length);
  return (
    <div className="msg msg--assistant" data-history-restore={item.id.startsWith("h") ? "" : undefined} data-entrance={item.id}>
      {(hasText || hasFootnotes) && (
        <div className="msg__body" data-transcript-selectable="message">
          {hasText && <PresentedFileLinkProvider files={presentedFiles} tabId={tabId} hostId={hostId}>
            <Markdown text={item.text} streaming={item.streaming} cacheKey={item.id} wasStreamed={item.wasStreamed} />
          </PresentedFileLinkProvider>}
          <Suspense fallback={null}><SearchSourcesPanel sources={item.searchSources} /></Suspense>
        </div>
      )}
      {Boolean(item.memoryCitations?.length) && <Suspense fallback={null}><MemoryCitations citations={item.memoryCitations} /></Suspense>}
    </div>
  );
});
