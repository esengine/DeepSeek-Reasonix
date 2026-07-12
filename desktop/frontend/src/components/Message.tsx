import { createContext, memo, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { FormEvent, KeyboardEvent as ReactKeyboardEvent } from "react";
import { BrainCircuit, ChevronDown, ChevronRight, FileText, Folder, GitBranch, Image, MessageSquare, Pencil, RotateCcw, ScrollText } from "lucide-react";
import { Markdown } from "./Markdown";
import { CopyButton } from "./CopyButton";
import { ProcessBrainIcon } from "./ProcessCard";
import { ComposerContextCard } from "./ComposerContextCard";
import { formatAttachmentRefForDisplay, formatAttachmentRefForSubmit, parseAttachmentRefsForDisplay, sortDisplayAttachments } from "../lib/attachmentDisplay";
import type { DisplayAttachment } from "../lib/attachmentDisplay";
import { app } from "../lib/bridge";
import { replaySubmitText } from "../lib/editReplay";
import { useT } from "../lib/i18n";
import { ImageViewer } from "./ImageViewer";
import { Tooltip } from "./Tooltip";
import { useGSAPCollapse } from "../lib/useGSAPCollapse";
import { displayReasoningText } from "../lib/reasoningDisplay";
import { stripMemoryCompilerExecution } from "../lib/memoryCompilerDisplay";
import { visibleTranscriptMemoryCitations } from "../lib/memoryCitationVisibility";
import { invocationSegmentsFromMessage, type InvocationMetadataMap } from "../lib/invocationDisplay";
import type { Item, MessageActionScope } from "../lib/useController";
import type { CheckpointMeta, MemoryCitation } from "../lib/types";
import { InvocationBadge } from "./InvocationBadge";

type AssistantItem = Extract<Item, { kind: "assistant" }>;
export type TurnActionMenu = "summary" | "rewind";
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

function mergeDisplayAttachments(existing: DisplayAttachment[], incoming: DisplayAttachment[]): DisplayAttachment[] {
  if (incoming.length === 0) return existing;
  const seen = new Set(existing.map((attachment) => attachment.path));
  const merged = [...existing];
  for (const attachment of incoming) {
    if (seen.has(attachment.path)) continue;
    seen.add(attachment.path);
    merged.push(attachment);
  }
  return merged;
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

function MemoryCitations({ citations }: { citations?: MemoryCitation[] }) {
  const t = useT();
  const bodyRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const clean = visibleTranscriptMemoryCitations(citations)
    .filter((citation) => (citation.source ?? citation.id ?? citation.note ?? "").trim() !== "")
    .slice(0, 5);
  useGSAPCollapse(bodyRef, open);
  if (clean.length === 0) return null;
  return (
    <div className="msg-memory-citations">
      <button
        type="button"
        className="msg-memory-citations__toggle"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <ChevronRight className={`msg-memory-citations__chevron${open ? " msg-memory-citations__chevron--open" : ""}`} size={15} />
        <span>{t("msg.memoryCompilerCitationsCount", { n: clean.length })}</span>
      </button>
      {open && (
        <div ref={bodyRef} className="msg-memory-citations__body">
          {clean.map((citation, index) => {
            const lines = memoryCitationLines(citation, t);
            return (
              <div key={`${citation.id ?? citation.source}-${index}`} className="msg-memory-citations__item">
                <div className="msg-memory-citations__source">
                  <span>{memoryCitationSource(citation)}</span>
                  {lines && <span className="msg-memory-citations__lines">{lines}</span>}
                </div>
                {citation.note && <div className="msg-memory-citations__note">{citation.note}</div>}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function memoryCitationSource(citation: MemoryCitation): string {
  const source = (citation.source || citation.id || "Memory v5").trim();
  if (citation.kind === "compiler_reference" && source === "Memory v5") return "Memory v5 compiler";
  return source;
}

function memoryCitationLines(citation: MemoryCitation, t: ReturnType<typeof useT>): string {
  const start = citation.lineStart ?? 0;
  const end = citation.lineEnd ?? 0;
  if (start <= 0) return "";
  if (end > 0 && end !== start) return t("msg.memoryCitationLineRange", { start, end });
  return t("msg.memoryCitationLine", { line: start });
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
  onEdit,
  editDisabled = false,
}: {
  text: string;
  submitText?: string;
  failed?: boolean;
  turn?: number;
  anchorId?: string;
  id?: string;
  createdAt?: number;
  onEdit?: (turn: number, displayText: string, submitText?: string) => boolean | void | Promise<boolean | void>;
  editDisabled?: boolean;
}) {
  const t = useT();
  const invocationMetadata = useContext(InvocationMetadataContext);
  const imSource = parseImSourceMessage(text);
  const actionText = stripMemoryCompilerExecution(imSource?.text ?? text);
  const hasMemoryCompiler = Boolean(submitText?.includes("<memory-compiler-execution>"));
  const { text: displayText, attachments } = parseAttachmentRefsForDisplay(actionText);
  const invocationSegments = imSource ? [] : invocationSegmentsFromMessage(displayText, submitText, invocationMetadata);
  const hasInvocationSegments = invocationSegments.some((segment) => segment.type === "invocation");
  const orderedAttachments = sortDisplayAttachments(attachments);
  const sourceLabel = imSource ? imSourceLabel(imSource, t) : "";
  const sentAt = createdAt === undefined ? null : messageDate(createdAt);
  const canEdit = turn !== undefined && onEdit !== undefined && !editDisabled;
  const [editing, setEditing] = useState(false);
  const [draftText, setDraftText] = useState(displayText);
  const [draftAttachments, setDraftAttachments] = useState<DisplayAttachment[]>(attachments);
  const [editSubmitting, setEditSubmitting] = useState(false);
  const editRef = useRef<HTMLTextAreaElement>(null);
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

  const pasteBlocks = useMemo(() => parsePastedBlocks(actionText, submitText), [actionText, submitText]);
  const [expandedPasteLabels, setExpandedPasteLabels] = useState<Record<string, boolean>>({});

  type DisplaySegment =
    | { type: "text"; content: string }
    | { type: "paste"; block: PastedBlockInfo };

  const displaySegments = useMemo((): DisplaySegment[] => {
    if (pasteBlocks.length === 0) return [{ type: "text", content: displayText }];
    const segments: DisplaySegment[] = [];
    // Order blocks by their position in the text so cards appear inline.
    const ordered = pasteBlocks
      .map((b) => ({ block: b, pos: displayText.indexOf(b.label) }))
      .filter((x) => x.pos >= 0)
      .sort((a, b) => a.pos - b.pos);
    let remaining = displayText;
    for (const { block } of ordered) {
      const idx = remaining.indexOf(block.label);
      if (idx < 0) continue;
      // Text before the label: strip the trailing newline that separated the
      // label from the preceding line so the card sits tight against the text.
      if (idx > 0) {
        let before = remaining.slice(0, idx);
        before = before.replace(/\n$/, "");
        if (before) segments.push({ type: "text", content: before });
      }
      segments.push({ type: "paste", block });
      remaining = remaining.slice(idx + block.label.length);
    }
    // Strip the leading newline that followed the label.
    remaining = remaining.replace(/^\n/, "");
    if (remaining.trim()) segments.push({ type: "text", content: remaining });
    return segments.length > 0 ? segments : [{ type: "text", content: displayText }];
  }, [displayText, pasteBlocks]);

  const togglePasteExpand = (label: string) => {
    setExpandedPasteLabels((prev) => ({
      ...prev,
      [label]: !prev[label],
    }));
  };
  const orderedDraftAttachments = sortDisplayAttachments(draftAttachments);
  const imagePreviewKey = orderedAttachments
    .concat(orderedDraftAttachments)
    .filter((attachment) => attachment.kind === "image" && attachment.source === "attachment")
    .map((attachment) => attachment.path)
    .join("\n");

  useEffect(() => {
    if (editing) return;
    const parsed = parseAttachmentRefsForDisplay(actionText);
    setDraftText(parsed.text);
    setDraftAttachments(parsed.attachments);
  }, [actionText, editing]);

  useEffect(() => {
    if (!editing) return;
    requestAnimationFrame(() => {
      const node = editRef.current;
      if (!node) return;
      node.focus();
      node.selectionStart = node.selectionEnd = node.value.length;
    });
  }, [editing]);

  const startEdit = () => {
    if (!canEdit) return;
    const parsed = parseAttachmentRefsForDisplay(actionText);
    setDraftText(parsed.text);
    setDraftAttachments(parsed.attachments);
    setEditing(true);
  };

  const cancelEdit = () => {
    const parsed = parseAttachmentRefsForDisplay(actionText);
    setDraftText(parsed.text);
    setDraftAttachments(parsed.attachments);
    setEditing(false);
  };

  const updateDraftText = (value: string) => {
    const parsed = parseAttachmentRefsForDisplay(value);
    if (parsed.attachments.length > 0) {
      setDraftText(parsed.text);
      setDraftAttachments((prev) => mergeDisplayAttachments(prev, parsed.attachments));
      return;
    }
    setDraftText(value);
  };

  const removeDraftAttachment = (path: string) => {
    setDraftAttachments((prev) => prev.filter((attachment) => attachment.path !== path));
  };

  const submitEdit = async (event?: FormEvent) => {
    event?.preventDefault();
    if (!canEdit || editSubmitting) return;
    const parsedDraft = parseAttachmentRefsForDisplay(draftText);
    const nextAttachments = sortDisplayAttachments(mergeDisplayAttachments(draftAttachments, parsedDraft.attachments));
    const bodyText = parsedDraft.text.trim();
    const displayRefs = nextAttachments.map(formatAttachmentRefForDisplay).join(" ");
    const submitRefs = nextAttachments.map(formatAttachmentRefForSubmit).join(" ");
    const next = [bodyText, displayRefs].filter(Boolean).join(bodyText && displayRefs ? " " : "");
    const fallbackSubmit = [bodyText, submitRefs].filter(Boolean).join(bodyText && submitRefs ? " " : "");
    const submit = replaySubmitText(submitText, actionText, next, fallbackSubmit);
    if (!next) return;
    setEditSubmitting(true);
    try {
      const ok = await onEdit?.(turn as number, next, submit);
      if (ok !== false) setEditing(false);
    } finally {
      setEditSubmitting(false);
    }
  };

  const onEditKeyDown = (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      cancelEdit();
      return;
    }
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      void submitEdit();
    }
  };

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
      <div className={`msg__body${editing ? " msg__body--editing" : ""}`}>
        {editing ? (
          <form className="msg-edit" onSubmit={(event) => void submitEdit(event)}>
            {orderedDraftAttachments.length > 0 && (
              <div className="msg-edit__attachments composer-context" aria-label={t("composer.contextItems")}>
                {orderedDraftAttachments.map((attachment) => {
                  const imagePreview = attachment.kind === "image" ? imagePreviews[attachment.path] : undefined;
                  const imageOnly = Boolean(imagePreview) && orderedDraftAttachments.every((item) => item.kind === "image" && imagePreviews[item.path]);
                  return (
                    <ComposerContextCard
                      key={attachment.path}
                      variant={attachment.source === "workspace" ? "workspace" : "attachment"}
                      tooltipLabel={imagePreview ? `${t("imageViewer.clickToPreview")} — ${attachment.path}` : attachment.source === "workspace" ? formatAttachmentRefForSubmit(attachment) : attachment.path}
                      removeLabel={attachment.source === "workspace" ? t("composer.removeReference") : t("composer.removeImage")}
                      removeDisabled={editSubmitting}
                      onRemove={() => removeDraftAttachment(attachment.path)}
                      previewUrl={imagePreview}
                      onImageClick={imagePreview ? () => openImageViewer(attachment.path, attachment.name) : undefined}
                      imageOnly={imageOnly}
                      folder={attachment.kind === "folder"}
                      label={attachment.kind === "folder" ? `${attachment.name}/` : attachment.name}
                      name={attachment.name}
                      meta={attachment.ext || t("msg.fileAttachment")}
                      icon={attachment.kind === "image" ? <Image size={20} /> : undefined}
                    />
                  );
                })}
              </div>
            )}
            <textarea
              ref={editRef}
              className="msg-edit__input"
              value={draftText}
              rows={Math.max(2, Math.min(8, draftText.split(/\r?\n/).length))}
              aria-label={t("common.edit")}
              disabled={editSubmitting}
              onChange={(event) => updateDraftText(event.target.value)}
              onKeyDown={onEditKeyDown}
            />
            <div className="msg-edit__actions">
              <button className="msg-edit__btn" type="button" disabled={editSubmitting} onClick={cancelEdit}>
                {t("common.cancel")}
              </button>
              <button className="msg-edit__btn msg-edit__btn--primary" type="submit" disabled={editSubmitting || (draftText.trim() === "" && draftAttachments.length === 0)}>
                {t("msg.editSend")}
              </button>
            </div>
          </form>
        ) : imSource ? (
          <div className="im-source-card">
            <div className="im-source-card__head">
              <MessageSquare size={14} />
              <span>{t("msg.fromIm", { source: sourceLabel })}</span>
            </div>
            {displayText && <div className="im-source-card__text">{displayText}</div>}
            {(imSource.sender || imSource.chat) && (
              <div className="im-source-card__meta">
                {imSource.sender && <span>{t("msg.imSender", { id: imSource.sender })}</span>}
                {imSource.chat && <span>{imSource.chat}</span>}
              </div>
            )}
          </div>
        ) : (
          <>
            {hasInvocationSegments && pasteBlocks.length === 0 ? (
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
              const expanded = Boolean(expandedPasteLabels[seg.block.label]);
              return (
                <div className="msg-pasted" key={seg.block.label}>
                  <div className="msg-pasted-block">
                    <div className="msg-pasted-head">
                      <FileText size={15} />
                      <span className="msg-pasted-label">{seg.block.label}</span>
                      <div className="msg-pasted-actions">
                        <Tooltip label={t(expanded ? "msg.pastedCollapseTooltip" : "msg.pastedExpandTooltip")}>
                          <button type="button" onClick={() => togglePasteExpand(seg.block.label)}>
                            {expanded ? t("common.collapse") : t("composer.pastedExpand")}
                          </button>
                        </Tooltip>
                      </div>
                    </div>
                    {expanded && (
                      <div className="msg-pasted-expanded">{seg.block.content}</div>
                    )}
                  </div>
                </div>
              );
            })}
          </>
        )}
        {failed && <div className="msg__send-failed">{t("msg.sendFailed")}</div>}
        {orderedAttachments.length > 0 && (
          <div className="msg-attachments" aria-label={t("msg.attachments")}>
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
      {!editing && (
        <div className="msg-meta" role="group" aria-label={t("rewind.label")}>
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
          {onEdit && (
            <button
              className="msg-meta__btn"
              type="button"
              aria-label={t("common.edit")}
              title={t("common.edit")}
              disabled={!canEdit}
              onClick={startEdit}
            >
              <Pencil size={14} />
            </button>
          )}
        </div>
      )}
    </div>
  );
}

export function TurnActions({
  text,
  turn,
  openMenu,
  onOpenMenu,
  onRewind,
  checkpoint,
  actionPending = false,
  rewindDisabled = false,
  hoverMenus = false,
  isLastTurn = false,
}: {
  text: string;
  turn?: number;
  openMenu?: TurnActionMenu | null;
  onOpenMenu?: (menu: TurnActionMenu | null) => void;
  onRewind?: (turn: number, scope: MessageActionScope) => void;
  checkpoint?: CheckpointMeta;
  actionPending?: boolean;
  rewindDisabled?: boolean;
  hoverMenus?: boolean;
  /** true when this is the last user turn — disables "summarize after" */
  isLastTurn?: boolean;
}) {
  const t = useT();
  const [confirmScope, setConfirmScope] = useState<MessageActionScope | null>(null);
  const canAct = onRewind != null && turn != null;
  const actionDisabledReason = (scope: string): string => {
    if (rewindDisabled || actionPending) return t("rewind.disabledRunning");
    if (!checkpoint) return t("rewind.disabledNoCheckpoint");
    if ((scope === "fork" || scope === "summ-from" || scope === "conversation") && !checkpoint.canConversation) {
      return t("rewind.disabledNoBoundary");
    }
    if (scope === "summ-from" && isLastTurn) {
      return t("rewind.disabledNoLater");
    }
    if (scope === "summ-upto") {
      if (!checkpoint.canConversation) return t("rewind.disabledNoBoundary");
      if ((turn ?? 0) <= 0) return t("rewind.disabledNoEarlier");
    }
    if (scope === "code" && !checkpoint.canCode) return t("rewind.disabledNoCode");
    if (scope === "both") {
      if (!checkpoint.canConversation) return t("rewind.disabledNoBoundary");
      if (!checkpoint.canCode) return t("rewind.disabledNoCode");
    }
    return "";
  };
  const actionLabel = (scope: MessageActionScope): string => {
    if (confirmScope !== scope) {
      switch (scope) {
        case "fork":
          return t("rewind.fork");
        case "summ-from":
          return t("rewind.summFrom");
        case "summ-upto":
          return t("rewind.summUpto");
        case "conversation":
          return t("rewind.conversation");
        case "code":
          return t("rewind.code");
        default:
          return t("rewind.both");
      }
    }
    switch (scope) {
      case "fork":
        return t("rewind.confirmFork");
      case "summ-from":
        return t("rewind.confirmSummFrom");
      case "summ-upto":
        return t("rewind.confirmSummUpto");
      case "conversation":
        return t("rewind.confirmConversation");
      case "code":
        return t("rewind.confirmCode");
      default:
        return t("rewind.confirmBoth");
    }
  };
  const actionMeta = (scope: MessageActionScope): string => {
    const total = checkpoint?.fileCount ?? checkpoint?.files?.length ?? 0;
    if ((scope === "code" || scope === "both") && total > 0) {
      const turnCount = checkpoint?.turnFileCount ?? 0;
      if (turnCount > 0 && turnCount < total) {
        return `${t("rewind.filesChanged", { count: total })} (${t("rewind.turnFiles", { count: turnCount })})`;
      }
      return t("rewind.filesChanged", { count: total });
    }
    return "";
  };
  const actionTooltipLabel = (scope: MessageActionScope) => {
    const reason = actionDisabledReason(scope);
    if (reason) return <span>{reason}</span>;
    const files = checkpoint?.files ?? [];
    const total = checkpoint?.fileCount ?? files.length;
    if ((scope === "code" || scope === "both") && total > 0) {
      const hidden = Math.max(0, total - files.length);
      return (
        <div className="rewind__files-tooltip">
          {files.map((file) => (
            <div key={file}>{file.split(/[/\\]/).pop() || file}</div>
          ))}
          {hidden > 0 && <div>+{hidden}</div>}
        </div>
      );
    }
    return undefined;
  };
  const runAction = (scope: MessageActionScope) => {
    setConfirmScope(null);
    onOpenMenu?.(null);
    onRewind?.(turn as number, scope);
  };
  const selectRewind = (scope: MessageActionScope) => {
    if (actionDisabledReason(scope)) return;
    if (confirmScope !== scope) {
      setConfirmScope(scope);
      return;
    }
    runAction(scope);
  };
  const renderAction = (scope: MessageActionScope, danger = false) => {
    const disabledReason = actionDisabledReason(scope);
    const meta = actionMeta(scope);
    const tipLabel = actionTooltipLabel(scope);
    const button = (
      <button
        className={[
          "rewind__menu-item",
          danger ? "rewind__menu-danger" : "",
          confirmScope === scope ? "rewind__menu-confirm" : "",
        ].filter(Boolean).join(" ")}
        type="button"
        disabled={Boolean(disabledReason)}
        {...(tipLabel ? {} : { title: disabledReason || undefined })}
        onClick={() => selectRewind(scope)}
      >
        <span>{actionLabel(scope)}</span>
        {meta && <span className="rewind__menu-meta">{meta}</span>}
      </button>
    );
    return tipLabel ? <Tooltip key={scope} label={tipLabel} side="top" block fill>{button}</Tooltip> : button;
  };
  const forkDisabledReason = canAct ? actionDisabledReason("fork") : "";
  const toggleMenu = (menu: TurnActionMenu) => {
    setConfirmScope(null);
    onOpenMenu?.(openMenu === menu ? null : menu);
  };
  const openHoverMenu = (menu: TurnActionMenu) => {
    if (!hoverMenus || openMenu === menu) return;
    setConfirmScope(null);
    onOpenMenu?.(menu);
  };
  return (
    <div className={`turn-actions${openMenu ? " turn-actions--open" : ""}${hoverMenus ? " turn-actions--hover-menu" : ""}`}>
      <CopyButton text={text} label={t("msg.copy")} />
      {canAct && (
        <>
          <button
            className={`turn-actions__btn${confirmScope === "fork" ? " turn-actions__btn--confirm" : ""}`}
            type="button"
            disabled={Boolean(forkDisabledReason)}
            title={forkDisabledReason || undefined}
            onClick={() => selectRewind("fork")}
          >
            <GitBranch size={13} />
            <span>{actionLabel("fork")}</span>
          </button>
          <div
            className={`turn-actions__group${openMenu === "summary" ? " turn-actions__group--open" : ""}`}
            onMouseEnter={() => openHoverMenu("summary")}
          >
            <button
              className="turn-actions__btn"
              type="button"
              aria-haspopup="menu"
              aria-expanded={openMenu === "summary"}
              onClick={() => toggleMenu("summary")}
            >
              <ScrollText size={13} />
              <span>{t("turnActions.summary")}</span>
              <ChevronDown size={12} />
            </button>
            {openMenu === "summary" && (
              <div className="rewind__menu turn-actions__menu" role="menu">
                {rewindDisabled && <div className="rewind__menu-hint">{t("rewind.disabledRunning")}</div>}
                {!rewindDisabled && !checkpoint && <div className="rewind__menu-hint">{t("rewind.disabledNoCheckpoint")}</div>}
                {renderAction("summ-from")}
                {renderAction("summ-upto")}
              </div>
            )}
          </div>
          <div
            className={`turn-actions__group${openMenu === "rewind" ? " turn-actions__group--open" : ""}`}
            onMouseEnter={() => openHoverMenu("rewind")}
          >
            <button
              className="turn-actions__btn"
              type="button"
              aria-haspopup="menu"
              aria-expanded={openMenu === "rewind"}
              onClick={() => toggleMenu("rewind")}
            >
              <RotateCcw size={13} />
              <span>{t("turnActions.rewind")}</span>
              <ChevronDown size={12} />
            </button>
            {openMenu === "rewind" && (
              <div className="rewind__menu turn-actions__menu" role="menu">
                {rewindDisabled && <div className="rewind__menu-hint">{t("rewind.disabledRunning")}</div>}
                {!rewindDisabled && !checkpoint && <div className="rewind__menu-hint">{t("rewind.disabledNoCheckpoint")}</div>}
                {renderAction("conversation")}
                {renderAction("code")}
                {renderAction("both", true)}
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}

function reasoningDurationLabel(durationMs: number | undefined, t: ReturnType<typeof useT>): string {
  if (typeof durationMs !== "number" || !Number.isFinite(durationMs) || durationMs <= 0) {
    return t("msg.thinkingDone");
  }
  const seconds = Math.max(1, Math.round(durationMs / 1000));
  return t("msg.thinkingDuration", { s: seconds });
}

function ReasoningPanel({
  item,
  defaultExpanded,
  expandWhileStreaming,
  truncateStreamingReasoning,
}: {
  item: AssistantItem;
  defaultExpanded: boolean;
  expandWhileStreaming: boolean;
  truncateStreamingReasoning: boolean;
}) {
  const t = useT();
  const reasoningBodyRef = useRef<HTMLDivElement>(null);
  // Thinking streams in before the answer — show it live while the model is still
  // working, then it stays available behind the toggle once the answer arrives.
  const [reasoningOpen, setReasoningOpen] = useState((expandWhileStreaming && item.streaming) || defaultExpanded);
  const userOverridden = useRef(false);
  const prevStreamingRef = useRef(item.streaming);
  const prevReasoningCompleteRef = useRef(item.reasoningComplete ?? false);
  useGSAPCollapse(reasoningBodyRef, reasoningOpen);

  // Follow the current display mode while streaming unless the user manually
  // toggled this message; auto-close at stream end for untouched messages.
  useEffect(() => {
    const wasStreaming = prevStreamingRef.current;
    const nowStreaming = item.streaming;
    prevStreamingRef.current = nowStreaming;

    const wasRC = prevReasoningCompleteRef.current;
    const nowRC = item.reasoningComplete ?? false;
    prevReasoningCompleteRef.current = nowRC;

    if (nowStreaming) {
      if (!wasStreaming) userOverridden.current = false;
      if (defaultExpanded) {
        setReasoningOpen(true);
      } else if (!userOverridden.current) {
        setReasoningOpen(expandWhileStreaming && !nowRC);
      }
    } else if (nowRC && !wasRC) {
      // Reasoning just finished — auto-close while we wait for text.
      if (!defaultExpanded && !userOverridden.current) {
        setReasoningOpen(false);
      }
    } else if (wasStreaming) {
      // Stream fully ended — auto-close if user didn't interact.
      if (!defaultExpanded && !userOverridden.current) {
        setReasoningOpen(false);
      }
    }
  }, [item.streaming, item.reasoningComplete, defaultExpanded, expandWhileStreaming]);

  const toggleReasoning = () => {
    userOverridden.current = true;
    setReasoningOpen((v) => !v);
  };
  const isReasoningRunning = item.streaming && !item.reasoningComplete;
  const visibleReasoning = reasoningOpen
    ? displayReasoningText(item.reasoning, {
        streaming: item.streaming,
        truncateStreaming: truncateStreamingReasoning,
      })
    : "";
  const label = isReasoningRunning ? t("msg.thinkingRunning") : t("msg.thinking");
  const meta = isReasoningRunning ? "" : reasoningDurationLabel(item.reasoningDurationMs, t);

  return (
    <div className="reasoning">
      <button
        type="button"
        className="reasoning__head"
        data-running={isReasoningRunning ? "" : undefined}
        onClick={toggleReasoning}
        aria-expanded={reasoningOpen}
      >
        <ProcessBrainIcon size={12} />
        <span data-creation-label={t("creation.reasoningLabel")}>{label}</span>
        {meta && <span className="reasoning__meta">{meta}</span>}
        <ChevronRight className={`reasoning__chevron${reasoningOpen ? " reasoning__chevron--open" : ""}`} size={12} />
      </button>
      {reasoningOpen && (
        <div ref={reasoningBodyRef} className="reasoning__body">{visibleReasoning}</div>
      )}
    </div>
  );
}

export const AssistantMessage = memo(function AssistantMessage({
  item,
  defaultExpanded = false,
  expandWhileStreaming = true,
  truncateStreamingReasoning = false,
  creationMode = false,
}: {
  item: AssistantItem;
  defaultExpanded?: boolean;
  /** false in compact mode: completed steps fold away, so auto-open + fold reads as flicker. */
  expandWhileStreaming?: boolean;
  /** Opt-in for compact mode to keep live DeepSeek reasoning from growing an unbounded DOM. */
  truncateStreamingReasoning?: boolean;
  creationMode?: boolean;
}) {
  const hasText = item.streaming || item.text.trim() !== "";
  const processOnly = Boolean(item.reasoning) && !hasText;
  const processWithText = Boolean(item.reasoning) && hasText;
  return (
    <div className={`msg msg--assistant${processOnly ? " msg--process-only" : ""}${processWithText ? " msg--process-with-text" : ""}`} data-history-restore={item.id.startsWith("h") ? "" : undefined} data-entrance={item.id}>
      {item.reasoning && (
        <ReasoningPanel
          item={item}
          defaultExpanded={defaultExpanded}
          expandWhileStreaming={expandWhileStreaming}
          truncateStreamingReasoning={truncateStreamingReasoning}
        />
      )}
      {hasText && (
        <div className="msg__body">
          <Markdown text={item.text} plainStatusBlocks={creationMode} />
        </div>
      )}
      <MemoryCitations citations={item.memoryCitations} />
    </div>
  );
});
