import { memo, useState } from "react";
import { ChevronRight, MoreHorizontal } from "lucide-react";
import { Markdown } from "./Markdown";
import { CopyButton } from "./CopyButton";
import { Tooltip } from "./Tooltip";
import { useT } from "../lib/i18n";
import type { Item } from "../lib/useController";
import type { CheckpointMeta } from "../lib/types";

type AssistantItem = Extract<Item, { kind: "assistant" }>;

export function UserMessage({
  text,
  turn,
  anchorId,
  open,
  onToggle,
  onRewind,
  checkpoint,
  actionPending = false,
  rewindDisabled = false,
}: {
  text: string;
  turn?: number;
  anchorId?: string;
  open?: boolean; // whether this message's rewind menu is the open one (lifted to Transcript)
  onToggle?: () => void;
  onRewind?: (turn: number, scope: string) => void;
  checkpoint?: CheckpointMeta;
  actionPending?: boolean;
  rewindDisabled?: boolean;
}) {
  const t = useT();
  const [confirmScope, setConfirmScope] = useState<string | null>(null);
  const canRewind = onRewind != null && turn != null;
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
  const actionLabel = (scope: string): string => {
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
  const actionMeta = (scope: string): string => {
    if ((scope === "code" || scope === "both") && checkpoint?.files?.length) {
      return t("rewind.filesChanged", { count: checkpoint.files.length });
    }
    return "";
  };
  const runAction = (scope: string) => {
    setConfirmScope(null);
    onRewind?.(turn as number, scope);
  };
  const selectRewind = (scope: string) => {
    if (actionDisabledReason(scope)) return;
    if (confirmScope !== scope) {
      setConfirmScope(scope);
      return;
    }
    runAction(scope);
  };
  const renderAction = (scope: string, danger = false) => {
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
  const displayText = text.replace(/@\.reasonix\/attachments\/[^\s]+/g, "[image]");
  return (
    <div className="msg msg--user" id={anchorId} data-question-anchor={anchorId} data-turn={turn}>
      <span className="msg__caret">›</span>
      <div className="msg__text">{displayText}</div>
      {canRewind && (
        <div className={`rewind${open ? " rewind--open" : ""}`}>
          <Tooltip label={t("rewind.label")}>
            <button
              className="rewind__btn"
              type="button"
              aria-label={t("rewind.label")}
              aria-expanded={Boolean(open)}
              onClick={() => {
                setConfirmScope(null);
                onToggle?.();
              }}
            >
              <MoreHorizontal size={15} />
            </button>
          </Tooltip>
          {open && (
            <div className="rewind__menu">
              {rewindDisabled && <div className="rewind__menu-hint">{t("rewind.disabledRunning")}</div>}
              {!rewindDisabled && !checkpoint && <div className="rewind__menu-hint">{t("rewind.disabledNoCheckpoint")}</div>}
              {renderAction("fork")}
              <div className="rewind__menu-separator" />
              {renderAction("summ-from")}
              {renderAction("summ-upto")}
              <div className="rewind__menu-separator" />
              {renderAction("conversation")}
              {renderAction("code")}
              {renderAction("both", true)}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// memo: an unchanged message keeps a stable `item` ref across a streaming turn's
// per-token re-renders, so only the live bubble re-parses markdown, not the whole
// backlog.
export const AssistantMessage = memo(function AssistantMessage({ item }: { item: AssistantItem }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  return (
    <div className="msg msg--assistant">
      {item.reasoning && (
        <div className="reasoning">
          <button className="reasoning__toggle" onClick={() => setOpen((v) => !v)}>
            <ChevronRight
              className={`reasoning__chevron ${open ? "reasoning__chevron--open" : ""}`}
              size={12}
            />
            {t("msg.thinking")}
          </button>
          {open && <div className="reasoning__body">{item.reasoning}</div>}
        </div>
      )}
      <div className="msg__body">
        {item.streaming ? (
          // While streaming, render raw text (stable, monospace-free) instead of
          // re-parsing markdown on every token — partial markdown reflows the
          // layout and makes the view jitter. Markdown renders once, on completion.
          <div className="msg__stream">
            {item.text}
            <span className="cursor" />
          </div>
        ) : (
          <Markdown text={item.text} />
        )}
      </div>
      {!item.streaming && item.text && (
        <div className="msg__actions">
          <CopyButton text={item.text} label={t("msg.copy")} />
        </div>
      )}
    </div>
  );
});
