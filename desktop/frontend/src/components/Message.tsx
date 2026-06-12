import { memo, useEffect, useState } from "react";
import { ChevronDown, ChevronRight, FileText, Folder, GitBranch, Image, MessageSquare, RotateCcw, ScrollText } from "lucide-react";
import { Markdown } from "./Markdown";
import { CopyButton } from "./CopyButton";
import { ProcessBrainIcon } from "./ProcessCard";
import { parseAttachmentRefsForDisplay, sortDisplayAttachments } from "../lib/attachmentDisplay";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { Item, MessageActionScope } from "../lib/useController";
import type { CheckpointMeta } from "../lib/types";

type AssistantItem = Extract<Item, { kind: "assistant" }>;
export type TurnActionMenu = "summary" | "rewind";
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

export function UserMessage({
  text,
  failed,
  turn,
  anchorId,
  id,
}: {
  text: string;
  failed?: boolean;
  turn?: number;
  anchorId?: string;
  id?: string;
}) {
  const t = useT();
  const imSource = parseImSourceMessage(text);
  const { text: displayText, attachments } = parseAttachmentRefsForDisplay(imSource?.text ?? text);
  const orderedAttachments = sortDisplayAttachments(attachments);
  const sourceLabel = imSource ? imSourceLabel(imSource, t) : "";
  const [imagePreviews, setImagePreviews] = useState<Record<string, string>>({});
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
    >
      <div className="msg__body">
        {imSource ? (
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
          displayText && <div className="msg__text">{displayText}</div>
        )}
        {failed && <div className="msg__send-failed">{t("msg.sendFailed")}</div>}
        {orderedAttachments.length > 0 && (
          <div className="msg-attachments" aria-label={t("msg.attachments")}>
            {orderedAttachments.map((attachment, index) => (
              <div className={`msg-attachment msg-attachment--${attachment.kind}`} key={`${attachment.path}:${index}`} title={attachment.path}>
                <span className={`msg-attachment__icon msg-attachment__icon--${attachment.kind}`} aria-hidden="true">
                  {attachment.kind === "image" && imagePreviews[attachment.path] ? <img src={imagePreviews[attachment.path]} alt="" draggable={false} /> : attachmentIcon(attachment.kind)}
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
            ))}
          </div>
        )}
      </div>
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
}: {
  text: string;
  turn?: number;
  openMenu?: TurnActionMenu | null;
  onOpenMenu?: (menu: TurnActionMenu | null) => void;
  onRewind?: (turn: number, scope: MessageActionScope) => void;
  checkpoint?: CheckpointMeta;
  actionPending?: boolean;
  rewindDisabled?: boolean;
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
    if ((scope === "code" || scope === "both") && checkpoint?.files?.length) {
      return t("rewind.filesChanged", { count: checkpoint.files.length });
    }
    return "";
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
    return (
      <button
        className={[
          "rewind__menu-item",
          danger ? "rewind__menu-danger" : "",
          confirmScope === scope ? "rewind__menu-confirm" : "",
        ].filter(Boolean).join(" ")}
        type="button"
        disabled={Boolean(disabledReason)}
        title={disabledReason || undefined}
        onClick={() => selectRewind(scope)}
      >
        <span>{actionLabel(scope)}</span>
        {meta && <span className="rewind__menu-meta">{meta}</span>}
      </button>
    );
  };
  const forkDisabledReason = canAct ? actionDisabledReason("fork") : "";
  const toggleMenu = (menu: TurnActionMenu) => {
    setConfirmScope(null);
    onOpenMenu?.(openMenu === menu ? null : menu);
  };

  return (
    <div className="turn-actions">
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
          <div className={`turn-actions__group${openMenu === "summary" ? " turn-actions__group--open" : ""}`}>
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
          <div className={`turn-actions__group${openMenu === "rewind" ? " turn-actions__group--open" : ""}`}>
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

export const AssistantMessage = memo(function AssistantMessage({
  item,
  defaultExpanded = false,
}: {
  item: AssistantItem;
  defaultExpanded?: boolean;
}) {
  const t = useT();
  // Thinking streams in before the answer — show it live while the model is still
  // working, then it stays available behind the toggle once the answer arrives.
  const [reasoningOpen, setReasoningOpen] = useState(item.streaming || defaultExpanded);
  const hasText = item.streaming || item.text.trim() !== "";
  const processOnly = Boolean(item.reasoning) && !hasText;
  const processWithText = Boolean(item.reasoning) && hasText;
  return (
    <div className={`msg msg--assistant${processOnly ? " msg--process-only" : ""}${processWithText ? " msg--process-with-text" : ""}`} data-history-restore={item.id.startsWith("h") ? "" : undefined}>
      {item.reasoning && (
        <div className="reasoning">
          <button
            type="button"
            className="reasoning__head"
            data-running={item.streaming ? "" : undefined}
            onClick={() => setReasoningOpen((v) => !v)}
            aria-expanded={reasoningOpen}
          >
            <ProcessBrainIcon size={12} />
            <span>{t("msg.thinking")}</span>
            <span className="reasoning__meta">{item.streaming ? t("msg.thinkingRunning") : t("msg.thinkingDone")}</span>
            <ChevronRight className={`reasoning__chevron${reasoningOpen ? " reasoning__chevron--open" : ""}`} size={12} />
          </button>
          {reasoningOpen && (
            <div className="reasoning__body">{item.reasoning}</div>
          )}
        </div>
      )}
      {hasText && (
        <div className="msg__body">
          <Markdown text={item.text} showCursor={item.streaming} />
        </div>
      )}
    </div>
  );
});
