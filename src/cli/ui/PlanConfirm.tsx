import { Box, Text, useStdout } from "ink";
import React, { useMemo, useState } from "react";
import { t } from "../../i18n/index.js";
import type { PlanStep } from "../../tools/plan.js";
import { PlanStepList } from "./PlanStepList.js";
import { SingleSelect } from "./Select.js";
import { ApprovalCard } from "./cards/ApprovalCard.js";
import { useKeystroke } from "./keystroke-context.js";
import { useReserveRows, useTotalRows } from "./layout/viewport-budget.js";
import { MarkdownView } from "./markdown-view.js";
import { extractOpenQuestionsSection } from "./plan-open-questions.js";
import type { KeyEvent } from "./stdin-reader.js";
import { CARD, FG, TONE } from "./theme/tokens.js";

export type PlanConfirmChoice = "approve" | "refine" | "revise" | "cancel";

export interface PlanConfirmProps {
  plan: string;
  steps?: PlanStep[];
  summary?: string;
  onChoose: (choice: PlanConfirmChoice) => void;
  projectRoot?: string;
}

const DEFAULT_DETAIL_LINES = 12;
const MIN_DETAIL_LINES = 6;
/** Header + step list + dividers + footer hints + picker — what's left over for the detail window when expanded. */
const EXPANDED_MODAL_OVERHEAD_ROWS = 12;
/** Card stripe + outer dividers — rows the modal needs even when the detail window owns everything else. */
const EXPANDED_DETAIL_CHROME_ROWS = 4;

function PlanConfirmInner({ plan, steps, summary, onChoose }: PlanConfirmProps) {
  const { stdout } = useStdout();
  const totalRows = useTotalRows();
  const [expanded, setExpanded] = useState(false);
  const [detailOffset, setDetailOffset] = useState(0);
  const stepRows = steps?.length ?? 0;
  const hasSteps = stepRows > 0;
  const openQuestions = extractOpenQuestionsSection(plan);
  const planLines = useMemo(() => plan.split("\n"), [plan]);
  const effectiveSummary = useMemo(
    () => summarizePlan(plan, summary, steps),
    [plan, summary, steps],
  );

  const oqRows = openQuestions ? Math.min(openQuestions.split("\n").length, 8) : 0;
  const modalRows = useReserveRows("modal", {
    min: 10,
    max: expanded
      ? Math.max(10, totalRows - EXPANDED_DETAIL_CHROME_ROWS)
      : Math.max(16, Math.min(32, (hasSteps ? stepRows + 2 : 2) + oqRows + 14)),
  });
  const detailViewRows = expanded
    ? Math.max(10, modalRows - EXPANDED_MODAL_OVERHEAD_ROWS)
    : Math.max(
        MIN_DETAIL_LINES,
        Math.min(18, Math.floor(((stdout?.rows ?? 32) - 14) / 2) || DEFAULT_DETAIL_LINES),
      );
  const maxDetailOffset = Math.max(0, planLines.length - detailViewRows);
  const clampedDetailOffset = Math.min(detailOffset, maxDetailOffset);
  const rawSliceStart = clampedDetailOffset;
  const rawSliceEnd = Math.min(planLines.length, rawSliceStart + detailViewRows);
  const { displayStart, displayEnd } = (() => {
    let start = rawSliceStart;
    let end = rawSliceEnd;
    while (start < end && planLines[start]?.trim() === "" && end < planLines.length) {
      start += 1;
      end += 1;
    }
    return { displayStart: start, displayEnd: end };
  })();
  const visiblePlanLines = planLines.slice(displayStart, displayEnd);
  const detailOverflow = planLines.length > detailViewRows;
  const showDetailScrollHint = expanded && plan.trim().length > 0 && detailOverflow;

  const detailOwnsScrollKey = expanded && detailOverflow;
  const isDetailScrollKey = (ev: KeyEvent) =>
    detailOwnsScrollKey &&
    !!(
      ev.pageUp ||
      ev.pageDown ||
      ev.home ||
      ev.end ||
      ev.mouseScrollUp ||
      ev.mouseScrollDown ||
      ev.upArrow ||
      ev.downArrow
    );

  useKeystroke((ev) => {
    if (ev.paste) return;
    if (ev.ctrl && ev.input === "p") {
      setExpanded((v) => !v);
      return;
    }
    if (!isDetailScrollKey(ev)) return;
    if (ev.pageUp || ev.mouseScrollUp) {
      setDetailOffset((n) => Math.max(0, n - detailViewRows));
    } else if (ev.pageDown || ev.mouseScrollDown) {
      setDetailOffset((n) => Math.min(maxDetailOffset, n + detailViewRows));
    } else if (ev.home) {
      setDetailOffset(0);
    } else if (ev.end) {
      setDetailOffset(maxDetailOffset);
    } else if (ev.upArrow) {
      setDetailOffset((n) => Math.max(0, n - 1));
    } else if (ev.downArrow) {
      setDetailOffset((n) => Math.min(maxDetailOffset, n + 1));
    }
  });

  const refineLabel = t("planFlow.picker.refine");
  const bannerTemplate = t("planFlow.openQuestionsBanner");
  const [bannerBefore, bannerAfter] = bannerTemplate.split("{refine}");

  return (
    <ApprovalCard
      tone="accent"
      glyph="⊞"
      title={t("planFlow.approveCardTitle")}
      metaRight={t("planFlow.approveCardMetaRight")}
      metaRightColor={CARD.plan.color}
    >
      {openQuestions ? (
        <Box marginBottom={1} flexDirection="column">
          <Text color={TONE.warn}>
            {bannerBefore ?? ""}
            <Text bold>{refineLabel}</Text>
            {bannerAfter ?? ""}
          </Text>
          <Box marginTop={1} flexDirection="column">
            <Text color={TONE.warn} bold>
              {t("planFlow.openQuestionsHeader")}
            </Text>
            <MarkdownView text={openQuestions} />
          </Box>
        </Box>
      ) : null}
      {!expanded || plan.trim().length === 0 ? (
        <Box marginBottom={1} flexDirection="column">
          {effectiveSummary ? (
            <Text color={FG.body}>{effectiveSummary}</Text>
          ) : (
            <Text color={FG.faint}>{t("planFlow.noPlanSummary")}</Text>
          )}
          {!expanded && hasSteps ? (
            <Box marginTop={1} flexDirection="column">
              <PlanStepList steps={steps!} />
            </Box>
          ) : null}
          <Text color={FG.faint}>
            {expanded ? t("planFlow.detailExpandedHint") : t("planFlow.detailCollapsedHint")}
          </Text>
        </Box>
      ) : null}
      {expanded && plan.trim().length > 0 ? (
        <PlanDetailWindow
          lines={visiblePlanLines}
          overflow={detailOverflow}
          start={displayStart + 1}
          end={displayEnd}
          total={planLines.length}
        />
      ) : null}
      {showDetailScrollHint ? (
        <Box marginBottom={1}>
          <Text color={FG.faint}>{t("planFlow.detailScrollHint")}</Text>
        </Box>
      ) : null}
      <SingleSelect
        initialValue={openQuestions ? "refine" : "approve"}
        items={[
          {
            value: "approve",
            label: t("planFlow.picker.accept"),
            hint: t("planFlow.picker.acceptHint"),
          },
          {
            value: "refine",
            label: refineLabel,
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
        ignoreKey={isDetailScrollKey}
      />
    </ApprovalCard>
  );
}

function PlanDetailWindow({
  lines,
  overflow,
  start,
  end,
  total,
}: {
  lines: readonly string[];
  overflow: boolean;
  start: number;
  end: number;
  total: number;
}): React.ReactElement {
  return (
    <Box flexDirection="column">
      {overflow ? (
        <Text color={FG.faint}>{t("planFlow.detailWindow", { start, end, total })}</Text>
      ) : null}
      {lines.map((line, i) => (
        <Text key={`plan-detail-${start + i}`} wrap="truncate">
          {line.length > 0 ? line : " "}
        </Text>
      ))}
    </Box>
  );
}

function summarizePlan(
  plan: string,
  summary: string | undefined,
  steps: PlanStep[] | undefined,
): string {
  const trimmedSummary = summary?.trim();
  if (trimmedSummary) return trimmedSummary;
  const firstTextLine = plan
    .split("\n")
    .map((line) => line.trim())
    .find((line) => line.length > 0 && !/^#{1,6}\s*$/.test(line));
  if (firstTextLine) return firstTextLine.replace(/^#{1,6}\s+/, "").slice(0, 160);
  if (steps && steps.length > 0) return steps[0]?.title ?? "";
  return "";
}

export const PlanConfirm = React.memo(PlanConfirmInner);
