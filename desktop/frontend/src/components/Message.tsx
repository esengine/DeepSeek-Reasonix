import { memo, useState } from "react";
import { ChevronDown, GitBranch, RotateCcw, ScrollText } from "lucide-react";
import { Markdown } from "./Markdown";
import { CopyButton } from "./CopyButton";
import { ProcessBrainIcon, ProcessCard, ProcessStatusIcon } from "./ProcessCard";
import { useT } from "../lib/i18n";
import type { Item, MessageActionScope } from "../lib/useController";
import type { CheckpointMeta } from "../lib/types";

type AssistantItem = Extract<Item, { kind: "assistant" }>;
export type TurnActionMenu = "summary" | "rewind";

export function UserMessage({
  text,
  turn,
  anchorId,
}: {
  text: string;
  turn?: number;
  anchorId?: string;
}) {
  const displayText = text.replace(/@\.reasonix\/attachments\/[^\s]+/g, "[image]");
  return (
    <div className="msg msg--user" id={anchorId} data-question-anchor={anchorId} data-turn={turn}>
      <div className="msg__body">
        <div className="msg__text">{displayText}</div>
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
}: {
  item: AssistantItem;
}) {
  const t = useT();
  const hasText = item.streaming || item.text.trim() !== "";
  const processOnly = Boolean(item.reasoning) && !hasText;
  const processWithText = Boolean(item.reasoning) && hasText;
  return (
    <div className={`msg msg--assistant${processOnly ? " msg--process-only" : ""}${processWithText ? " msg--process-with-text" : ""}`}>
      {item.reasoning && (
        <ProcessCard
          tone="violet"
          icon={<ProcessBrainIcon size={12} />}
          kind="reasoning"
          name={t("msg.thinking")}
          meta={
            <>
              <ProcessStatusIcon state={item.streaming ? "running" : "done"} label={item.streaming ? t("msg.thinkingRunning") : t("msg.thinkingDone")} />
              <span>{item.streaming ? t("msg.thinkingRunning") : t("msg.thinkingDone")}</span>
            </>
          }
          defaultOpen={item.streaming}
        >
          <div className="reasoning__body">{item.reasoning}</div>
        </ProcessCard>
      )}
      {hasText && (
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
      )}
    </div>
  );
});
