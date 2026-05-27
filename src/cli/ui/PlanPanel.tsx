/**
 * PlanPanel — unified right-side panel for all plan-related interaction.
 *
 * Replaces the previous modal-overlay pattern (PlanConfirm / PlanRefineInput /
 * PlanCheckpointConfirm / PlanReviseConfirm / PlanReviseEditor each replacing
 * the ComposerArea) with a persistent side panel that shows plan content while
 * keeping the conversation + composer visible on the left.
 *
 * Render priority (highest wins):
 *   1. stagedInput        → refining / approving / rejecting text input
 *   2. pendingCheckpoint  → checkpoint confirmation
 *   3. pendingRevision    → revision diff + accept/reject
 *   4. pendingReviseEditor→ step-level revise editor
 *   5. pendingPlan        → plan approval picker
 *   6. executing          → live step progress
 */

import { Box, type Color, Text } from "ink";
import type React from "react";
import { useMemo, useState } from "react";
import { t } from "../../i18n/index.js";
import type { PlanStep, StepCompletion } from "../../tools/plan.js";
import type { CheckpointChoice } from "./PlanCheckpointConfirm.js";
import type { PlanConfirmChoice } from "./PlanConfirm.js";
import type { ReviseChoice } from "./PlanReviseConfirm.js";
import { PlanStepList, type StepStatus } from "./PlanStepList.js";
import { SingleSelect } from "./Select.js";
import { useKeystroke } from "./keystroke-context.js";
import { MarkdownView } from "./markdown-view.js";
import { extractOpenQuestionsSection } from "./plan-open-questions.js";
import { Card } from "./primitives/Card.js";
import { CardHeader } from "./primitives/CardHeader.js";
import { PULSE_DIAMOND, Pulse } from "./primitives/Pulse.js";
import { useThemeTokens } from "./theme/context.js";
import { CARD, FG, TONE } from "./theme/tokens.js";
import { useTick } from "./ticker.js";

// ── Types ───────────────────────────────────────────────────────────

export type PlanPanelMode =
  | "idle"
  | "pending"
  | "refining"
  | "approving"
  | "rejecting"
  | "executing"
  | "checkpoint"
  | "revising"
  | "revise-editor";

export interface PlanPanelProps {
  // ── Plan content ──
  planBody: string | null;
  planSummary: string | null;
  planSteps: PlanStep[] | null;
  completedStepIds: Set<string>;

  // ── Mode triggers ──
  pendingPlan: string | null;
  stagedInput: {
    plan: string;
    mode: "refine" | "approve" | "reject";
    questions?: string;
  } | null;
  pendingCheckpoint: {
    stepId: string;
    title?: string;
    completed: number;
    total: number;
  } | null;
  pendingRevision: {
    reason: string;
    remainingSteps: PlanStep[];
    summary?: string;
  } | null;
  pendingReviseEditor: string | null;

  // ── Executing state ──
  isExecuting: boolean;

  // ── Handlers ──
  onPlanConfirm: (choice: PlanConfirmChoice) => void;
  onStagedInputSubmit: (feedback: string) => void;
  onStagedInputCancel: () => void;
  onCheckpointChoose: (choice: CheckpointChoice) => void;
  onReviseConfirm: (choice: ReviseChoice) => void;
}

// ── Component ───────────────────────────────────────────────────────

export function PlanPanel(props: PlanPanelProps): React.ReactElement | null {
  const mode = useMemo((): PlanPanelMode => {
    if (props.stagedInput) {
      switch (props.stagedInput.mode) {
        case "refine":
          return "refining";
        case "approve":
          return "approving";
        case "reject":
          return "rejecting";
      }
    }
    if (props.pendingCheckpoint) return "checkpoint";
    if (props.pendingRevision) return "revising";
    if (props.pendingReviseEditor) return "revise-editor";
    if (props.pendingPlan) return "pending";
    if (props.isExecuting) return "executing";
    return "idle";
  }, [
    props.stagedInput,
    props.pendingCheckpoint,
    props.pendingRevision,
    props.pendingReviseEditor,
    props.pendingPlan,
    props.isExecuting,
  ]);

  // Keep the panel mounted during execution so progress updates are visible
  if (mode === "idle" && !props.isExecuting) return null;

  const planBody = props.pendingPlan ?? props.planBody ?? "";
  const planSummary = props.planSummary ?? summarizePlan(planBody, props.planSteps);
  const planSteps = props.planSteps;
  const openQuestions = planBody ? extractOpenQuestionsSection(planBody) : null;

  return (
    <Box flexDirection="column" flexGrow={1} overflow="hidden" paddingLeft={1}>
      {/* Panel header */}
      <Box flexDirection="column" flexShrink={0} marginBottom={1}>
        <PanelHeader
          mode={mode}
          summary={planSummary}
          steps={planSteps}
          completedStepIds={props.completedStepIds}
          pendingCheckpoint={props.pendingCheckpoint}
        />
      </Box>

      {/* Panel body */}
      <Box flexDirection="column" flexGrow={1} overflow="hidden">
        {mode === "pending" ||
        mode === "refining" ||
        mode === "approving" ||
        mode === "rejecting" ? (
          <PlanBodyReadOnly
            planBody={props.stagedInput?.plan ?? props.pendingPlan ?? ""}
            detailExpanded={true}
          />
        ) : mode === "executing" ? (
          <PlanBodyReadOnly planBody={planBody} detailExpanded={true} />
        ) : mode === "checkpoint" && planSteps ? (
          <CheckpointBody
            stepId={props.pendingCheckpoint!.stepId}
            title={props.pendingCheckpoint!.title}
            completed={props.pendingCheckpoint!.completed}
            total={props.pendingCheckpoint!.total}
            steps={planSteps}
            completedStepIds={props.completedStepIds}
          />
        ) : mode === "revising" ? (
          <RevisionBody
            reason={props.pendingRevision!.reason}
            oldRemaining={(planSteps ?? []).filter((s) => !props.completedStepIds.has(s.id))}
            newRemaining={props.pendingRevision!.remainingSteps}
            summary={props.pendingRevision!.summary}
          />
        ) : null}
      </Box>

      {/* Panel footer — contextual actions */}
      <Box flexDirection="column" flexShrink={0} marginTop={1}>
        {mode === "pending" ? (
          <PendingActions hasOpenQuestions={!!openQuestions} onChoose={props.onPlanConfirm} />
        ) : mode === "refining" || mode === "approving" || mode === "rejecting" ? (
          <StagedInputActions
            mode={props.stagedInput!.mode}
            questions={props.stagedInput!.questions}
            onSubmit={props.onStagedInputSubmit}
            onCancel={props.onStagedInputCancel}
          />
        ) : mode === "checkpoint" ? (
          <CheckpointActions onChoose={props.onCheckpointChoose} />
        ) : mode === "revising" ? (
          <RevisionActions onChoose={props.onReviseConfirm} />
        ) : null}
      </Box>
    </Box>
  );
}

// ── Sub-components ──────────────────────────────────────────────────

function PanelHeader({
  mode,
  summary,
  steps,
  completedStepIds,
  pendingCheckpoint,
}: {
  mode: PlanPanelMode;
  summary: string;
  steps: PlanStep[] | null;
  completedStepIds: Set<string>;
  pendingCheckpoint: PlanPanelProps["pendingCheckpoint"];
}) {
  const { fg, tone, toneActive } = useThemeTokens();

  const headerMeta = useMemo(() => {
    if (mode === "executing" && steps) {
      const done = steps.filter((s) => completedStepIds.has(s.id)).length;
      return `${done}/${steps.length} ${t("cardLabels.done")}`;
    }
    if (mode === "checkpoint" && pendingCheckpoint) {
      return `${pendingCheckpoint.completed}/${pendingCheckpoint.total}`;
    }
    if (mode === "pending" && steps) {
      return `${steps.length} ${t("planFlow.stepList.counter", { total: steps.length })}`;
    }
    return null;
  }, [mode, steps, completedStepIds, pendingCheckpoint]);

  const hasRunning =
    mode === "executing" && steps ? steps.some((s) => !completedStepIds.has(s.id)) : false;

  const headerTone = hasRunning ? toneActive.accent : tone.accent;
  const titleMap: Record<PlanPanelMode, string> = {
    pending: t("planFlow.approveCardTitle"),
    refining: t("planFlow.modes.refine.title"),
    approving: t("planFlow.modes.approve.title"),
    rejecting: t("planFlow.modes.reject.title"),
    executing: t("cardLabels.plan"),
    checkpoint: t("planFlow.checkpoint.title"),
    revising: t("planReviseConfirm.title"),
    "revise-editor": t("planFlow.reviseTitle"),
    idle: "",
  };

  return (
    <Card tone={headerTone}>
      <CardHeader
        glyph={
          hasRunning ? <Pulse active frames={PULSE_DIAMOND} settled="◆" color={headerTone} /> : "⊞"
        }
        tone={headerTone}
        title={titleMap[mode]}
        meta={headerMeta ? [headerMeta] : undefined}
      />
      <Box paddingX={1}>
        <Text color={FG.body} wrap="truncate-end">
          {summary}
        </Text>
      </Box>
      {/* Step list for pending / checkpoint / executing modes */}
      {(mode === "pending" || mode === "checkpoint" || mode === "executing") &&
      steps &&
      steps.length > 0 ? (
        <Box paddingX={1} flexDirection="column">
          <PlanStepList
            steps={steps}
            statuses={buildStatusMap(
              steps,
              completedStepIds,
              mode === "checkpoint" ? pendingCheckpoint?.stepId : undefined,
            )}
            focusStepId={mode === "checkpoint" ? pendingCheckpoint?.stepId : undefined}
          />
        </Box>
      ) : null}
    </Card>
  );
}

function PlanBodyReadOnly({
  planBody,
  detailExpanded,
}: {
  planBody: string;
  detailExpanded: boolean;
}) {
  const planLines = useMemo(() => planBody.split("\n"), [planBody]);
  const [offset, setOffset] = useState(0);

  useKeystroke((ev) => {
    if (ev.pageUp) setOffset((n) => Math.max(0, n - 10));
    else if (ev.pageDown) setOffset((n) => n + 10);
    else if (ev.upArrow || ev.mouseScrollUp) setOffset((n) => Math.max(0, n - 1));
    else if (ev.downArrow || ev.mouseScrollDown) setOffset((n) => n + 1);
  });

  if (!planBody.trim() || !detailExpanded) {
    return (
      <Box paddingX={1}>
        <Text color={FG.faint}>{t("planFlow.noPlanSummary")}</Text>
      </Box>
    );
  }

  const visibleLines = planLines.slice(offset, offset + 50);

  return (
    <Box flexDirection="column" overflow="hidden" flexGrow={1}>
      <Box paddingX={1}>
        <Text color={FG.faint}>
          {t("planFlow.detailWindow", {
            start: offset + 1,
            end: offset + visibleLines.length,
            total: planLines.length,
          })}
        </Text>
      </Box>
      <Box flexDirection="column" overflow="hidden">
        {visibleLines.map((line, i) => (
          <Text key={`plan-body-${offset + i}`} wrap="truncate-end">
            {line.length > 0 ? line : " "}
          </Text>
        ))}
      </Box>
    </Box>
  );
}

// ── Footer action components ────────────────────────────────────────

function PendingActions({
  hasOpenQuestions,
  onChoose,
}: {
  hasOpenQuestions: boolean;
  onChoose: (choice: PlanConfirmChoice) => void;
}) {
  return (
    <SingleSelect
      initialValue={hasOpenQuestions ? "refine" : "approve"}
      items={[
        {
          value: "approve",
          label: t("planFlow.picker.accept"),
          hint: t("planFlow.picker.acceptHint"),
        },
        {
          value: "refine",
          label: t("planFlow.picker.refine"),
          hint: t("planFlow.picker.refineHint"),
        },
        {
          value: "revise",
          label: t("planFlow.picker.revise"),
          hint: t("planFlow.picker.reviseHint"),
        },
        {
          value: "cancel",
          label: t("planFlow.picker.reject"),
          hint: t("planFlow.picker.rejectHint"),
        },
      ]}
      onSubmit={(v) => onChoose(v as PlanConfirmChoice)}
      onCancel={() => onChoose("cancel")}
      inlineHints
    />
  );
}

function StagedInputActions({
  mode,
  questions,
  onSubmit,
  onCancel,
}: {
  mode: string;
  questions?: string;
  onSubmit: (feedback: string) => void;
  onCancel: () => void;
}) {
  const [value, setValue] = useState("");
  const tick = useTick();
  const cursorOn = Math.floor(tick / 4) % 2 === 0;

  const modeVisuals: Record<string, { glyph: string; cursorColor: Color }> = {
    approve: { glyph: "◇", cursorColor: CARD.user.color },
    refine: { glyph: "✎", cursorColor: CARD.warn.color },
    reject: { glyph: "✗", cursorColor: CARD.error.color },
  };
  const v = modeVisuals[mode] ?? { glyph: "›", cursorColor: CARD.plan.color };

  useKeystroke((ev) => {
    if (ev.paste) {
      setValue((prev) => prev + ev.input.replace(/\r?\n/g, " "));
      return;
    }
    if (ev.escape) {
      onCancel();
      return;
    }
    if (ev.return) {
      onSubmit(value.trim());
      return;
    }
    if (ev.backspace || ev.delete) {
      setValue((prev) => prev.slice(0, -1));
      return;
    }
    if (ev.input && !ev.ctrl && !ev.meta) {
      setValue((prev) => prev + ev.input);
    }
  });

  return (
    <Box flexDirection="column">
      {questions ? (
        <Box marginBottom={1} flexDirection="column">
          <Text color={TONE.warn} bold>
            {t("planFlow.refineQuestionsHeading")}
          </Text>
          <MarkdownView text={questions} />
        </Box>
      ) : null}
      <Box marginBottom={1}>
        <Text color={FG.sub}>{t(`planFlow.modes.${mode}.hint`)}</Text>
      </Box>
      <Box>
        <Text color={v.cursorColor} bold>
          {"› "}
        </Text>
        <Text>{value}</Text>
        <Text color={v.cursorColor} bold>
          {cursorOn ? "▍" : " "}
        </Text>
      </Box>
      <Box marginTop={1}>
        <Text color={FG.faint}>{t("planFlow.refineFooter")}</Text>
      </Box>
    </Box>
  );
}

function CheckpointActions({ onChoose }: { onChoose: (choice: CheckpointChoice) => void }) {
  return (
    <SingleSelect
      initialValue="continue"
      items={[
        {
          value: "continue",
          label: t("planFlow.checkpoint.continue"),
          hint: t("planFlow.checkpoint.continueHint"),
        },
        {
          value: "revise",
          label: t("planFlow.checkpoint.revise"),
          hint: t("planFlow.checkpoint.reviseHint"),
        },
        {
          value: "stop",
          label: t("planFlow.checkpoint.stop"),
          hint: t("planFlow.checkpoint.stopHint"),
        },
      ]}
      onSubmit={(v) => onChoose(v as CheckpointChoice)}
      onCancel={() => onChoose("stop")}
    />
  );
}

function RevisionActions({ onChoose }: { onChoose: (choice: ReviseChoice) => void }) {
  return (
    <SingleSelect
      initialValue="accept"
      items={[
        {
          value: "accept",
          label: t("planReviseConfirm.acceptLabel"),
          hint: t("planReviseConfirm.acceptHint"),
        },
        {
          value: "reject",
          label: t("planReviseConfirm.rejectLabel"),
          hint: t("planReviseConfirm.rejectHint"),
        },
      ]}
      onSubmit={(v) => onChoose(v as ReviseChoice)}
      onCancel={() => onChoose("reject")}
    />
  );
}

// ── Body sub-views ───────────────────────────────────────────────────

function CheckpointBody({
  stepId,
  title,
  completed,
  total,
  steps,
  completedStepIds,
}: {
  stepId: string;
  title?: string;
  completed: number;
  total: number;
  steps: PlanStep[];
  completedStepIds: Set<string>;
}) {
  const label = title ? `${stepId} · ${title}` : stepId;
  const counter = total > 0 ? `${completed}/${total}` : "";
  return (
    <Box flexDirection="column" paddingX={1}>
      <Text color={FG.sub}>{counter ? `${counter}  ·  ${label}` : label}</Text>
      <Box marginTop={1} flexDirection="column">
        <PlanStepList
          steps={steps}
          statuses={buildStatusMap(steps, completedStepIds, stepId)}
          focusStepId={stepId}
        />
      </Box>
    </Box>
  );
}

function RevisionBody({
  reason,
  oldRemaining,
  newRemaining,
  summary,
}: {
  reason: string;
  oldRemaining: PlanStep[];
  newRemaining: PlanStep[];
  summary?: string;
}) {
  const rows = computeRevisionDiff(oldRemaining, newRemaining);
  return (
    <Box flexDirection="column" paddingX={1} overflow="hidden">
      <Text>{reason}</Text>
      {summary ? (
        <Box marginY={1}>
          <Text dim>{t("planReviseConfirm.updatedSummary", { summary })}</Text>
        </Box>
      ) : null}
      <Box flexDirection="column" marginTop={1}>
        {rows.map((row) => {
          const risk = riskDots(row.step.risk);
          const prefix = row.kind === "removed" ? "−" : row.kind === "added" ? "+" : " ";
          const prefixColor: Color =
            row.kind === "removed" ? "#f87171" : row.kind === "added" ? "#4ade80" : "#94a3b8";
          const dim = row.kind === "kept";
          const strike = row.kind === "removed";
          return (
            <Box key={`${row.kind}-${row.step.id}`}>
              <Text color={prefixColor} bold>
                {`${prefix} `}
              </Text>
              <Text color={risk.color} bold dim={dim}>
                {risk.dots}
              </Text>
              <Text dim={dim} strikethrough={strike}>
                {` ${row.step.id} · ${row.step.title}`}
              </Text>
            </Box>
          );
        })}
      </Box>
    </Box>
  );
}

// ── Helpers ──────────────────────────────────────────────────────────

function summarizePlan(planBody: string, steps: PlanStep[] | null): string {
  const firstTextLine = planBody
    .split("\n")
    .map((line) => line.trim())
    .find((line) => line.length > 0 && !/^#{1,6}\s*$/.test(line));
  if (firstTextLine) return firstTextLine.replace(/^#{1,6}\s+/, "").slice(0, 160);
  if (steps && steps.length > 0) return steps[0]?.title ?? "";
  return "";
}

function buildStatusMap(
  steps: PlanStep[],
  completedStepIds: Set<string>,
  currentStepId?: string,
): Map<string, StepStatus> {
  const map = new Map<string, StepStatus>();
  for (const step of steps) {
    if (completedStepIds.has(step.id)) {
      map.set(step.id, "done");
    } else {
      map.set(step.id, "pending");
    }
  }
  // Mark current step as running in checkpoint mode
  if (currentStepId && map.has(currentStepId)) {
    map.set(currentStepId, "running");
  }
  return map;
}

interface DiffRow {
  kind: "kept" | "removed" | "added";
  step: PlanStep;
}

function computeRevisionDiff(oldSteps: PlanStep[], newSteps: PlanStep[]): DiffRow[] {
  const oldIds = new Set(oldSteps.map((s) => s.id));
  const newIds = new Set(newSteps.map((s) => s.id));
  const rows: DiffRow[] = [];
  for (const s of oldSteps) {
    if (!newIds.has(s.id)) rows.push({ kind: "removed", step: s });
  }
  for (const s of newSteps) {
    rows.push({ kind: oldIds.has(s.id) ? "kept" : "added", step: s });
  }
  return rows;
}

function riskDots(risk: PlanStep["risk"]): { dots: string; color: Color } {
  switch (risk) {
    case "high":
      return { dots: "●●●", color: "#f87171" };
    case "med":
      return { dots: "●● ", color: "#fbbf24" };
    case "low":
      return { dots: "●  ", color: "#4ade80" };
    default:
      return { dots: "   ", color: "#94a3b8" };
  }
}
