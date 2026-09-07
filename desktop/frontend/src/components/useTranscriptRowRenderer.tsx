import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import type { CheckpointMeta, WireCompletionSummary } from "../lib/types";
import type { ToolItem, TranscriptRow } from "../lib/transcriptRows";
import { assistantAnswerOnly } from "../lib/transcriptLiveTurn";
import { questionAnchorId } from "../lib/transcriptGrouping";
import { isSteerNoticeText } from "../lib/useController";
import { useT } from "../lib/i18n";
import { TurnActions, UserMessage } from "./Message";
import { ProcessFoldHeader } from "./ProcessFoldHeader";
import { InlineAssistantReasoning } from "./InlineAssistantReasoning";
import { ToolCard } from "./ToolCard";
import { ReadOnlyBatch } from "./ReadOnlyBatch";
import { ToolGroup } from "./ToolGroup";
import { CompactionCard, NoticeCard, PhaseCard, SteerCard } from "./TranscriptCards";
import { ExtensionCard } from "./ExtensionCard";
import { LiveAssistantMessage } from "./LiveAssistantMessage";

type OpenTurnAction = { turn: number; menu: "summary" | "rewind" | "fork" };

export function useTranscriptRowRenderer({
  tabId,
  checkpoints,
  subcallsByParent,
  creationMode,
  running,
  actionPending,
  rewindDisabled,
  actionHoverMenus,
  turnStartAt,
  lastTurn,
  onFoldToggle,
  onReasoningManualOpen,
  onPrompt,
  onDeliveryContinue,
  onAcceptDelivery,
  onOpenChanges,
  onOpenVerification,
  onEditPrompt,
  onRewind,
}: {
  tabId?: string;
  checkpoints: readonly CheckpointMeta[];
  subcallsByParent: ReadonlyMap<string, ToolItem[]>;
  creationMode: boolean;
  running: boolean;
  actionPending: boolean;
  rewindDisabled: boolean;
  actionHoverMenus: boolean;
  turnStartAt?: number;
  lastTurn: number | undefined;
  onFoldToggle: (segmentKey: string, open: boolean) => void;
  onReasoningManualOpen: (segmentKey: string) => void;
  onPrompt: (text: string) => void;
  onDeliveryContinue?: () => void;
  onAcceptDelivery?: () => void;
  onOpenChanges?: () => void;
  onOpenVerification?: (summary: WireCompletionSummary) => void;
  onEditPrompt?: (turn: number, displayText: string, submitText?: string) => boolean | void | Promise<boolean | void>;
  onRewind?: (turn: number, scope: string) => void;
}): (row: TranscriptRow) => ReactNode {
  const t = useT();
  const [openAction, setOpenAction] = useState<OpenTurnAction | null>(null);
  const checkpointsByTurn = useMemo(() => new Map(checkpoints.map((checkpoint) => [checkpoint.turn, checkpoint])), [checkpoints]);
  useEffect(() => {
    if (!openAction) return;
    const close = (event: MouseEvent) => {
      const target = event.target instanceof Element ? event.target : null;
      if (!target?.closest(".turn-actions")) setOpenAction(null);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [openAction]);

  return useCallback((row: TranscriptRow): ReactNode => {
    switch (row.kind) {
      case "older-history": return null;
      case "user": {
        const checkpoint = row.turn == null ? undefined : checkpointsByTurn.get(row.turn);
        return <UserMessage
          id={row.item.id} text={row.item.text} submitText={row.item.submitText}
          failed={row.item.failed} createdAt={row.item.createdAt} turn={row.turn}
          anchorId={questionAnchorId(row.item.id)} onEdit={onEditPrompt}
          editDisabled={rewindDisabled || !checkpoint?.canConversation}
        />;
      }
      case "process-header": return <ProcessFoldHeader
        segment={row.segment} open={row.open}
        onToggle={() => onFoldToggle(row.segment.key, row.open)}
        turnStartAt={row.segment.turnActive ? turnStartAt : undefined}
      />;
      case "reasoning": return <div className="turn-collapse__body"><InlineAssistantReasoning
        item={row.item} autoFollowActive={row.autoFollowActive}
        onManualOpen={() => onReasoningManualOpen(row.segmentKey)}
      /></div>;
      case "tool": return <div className="turn-collapse__body"><ToolCard item={row.item} subcalls={subcallsByParent.get(row.item.id)} tabId={tabId} /></div>;
      case "tool-batch": return <div className="turn-collapse__body"><ReadOnlyBatch items={[...row.items]} subcalls={subcallsByParent} tabId={tabId} /></div>;
      case "tool-group": return <div className="turn-collapse__body"><ToolGroup kind={row.groupKind} items={[...row.items]} subcalls={subcallsByParent} tabId={tabId} /></div>;
      case "phase": return <div className="turn-collapse__body"><PhaseCard id={row.item.id} text={row.item.text} /></div>;
      case "process-notice": return <div className="turn-collapse__body"><NoticeCard item={row.item} /></div>;
      case "compaction": return <div className="turn-collapse__body"><CompactionCard item={row.item} /></div>;
      case "answer": return <LiveAssistantMessage item={assistantAnswerOnly(row.item)} creationMode={creationMode} />;
      case "notice": {
        if (isSteerNoticeText(row.item.text)) return <SteerCard id={row.item.id} text={row.item.text} />;
        const action = row.item.action === "continue_delivery"
          ? (onDeliveryContinue ?? (() => onPrompt(t("notice.deliveryIncompleteContinuePrompt"))))
          : row.item.action === "open_changes" ? onOpenChanges : undefined;
        return <NoticeCard
          item={row.item} actionDisabled={running} onAction={action}
          onOpenVerification={row.item.variant === "completion" ? onOpenVerification : undefined}
          onAccept={row.item.action === "continue_delivery" ? onAcceptDelivery : undefined}
        />;
      }
      case "extension": return <ExtensionCard item={row.item} tabId={tabId} />;
      case "turn-actions": {
        const openMenu = openAction?.turn === row.turn ? openAction.menu : null;
        return <TurnActions
          text={row.text} turn={row.turn} openMenu={openMenu}
          onOpenMenu={(menu) => setOpenAction(menu ? { turn: row.turn, menu } : null)}
          checkpoint={checkpointsByTurn.get(row.turn)} actionPending={actionPending}
          rewindDisabled={rewindDisabled} hoverMenus={actionHoverMenus}
          isLastTurn={row.turn === lastTurn}
          onRewind={(turn, scope) => { onRewind?.(turn, scope); setOpenAction(null); }}
        />;
      }
    }
  }, [
    actionHoverMenus, actionPending, checkpointsByTurn, creationMode, lastTurn,
    onAcceptDelivery, onDeliveryContinue, onEditPrompt, onFoldToggle, onOpenChanges,
    onOpenVerification, onPrompt, onReasoningManualOpen, onRewind, openAction,
    rewindDisabled, running, subcallsByParent, t, tabId, turnStartAt,
  ]);
}
