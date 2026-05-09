/** Modal-style picker for `submit_plan`: accept / refine / cancel. */

import { Box, Text } from "ink";
import React from "react";
import { t } from "../../i18n/index.js";
import type { PlanStep } from "../../tools/plan.js";
import { PlanStepList } from "./PlanStepList.js";
import { SingleSelect } from "./Select.js";
import { ApprovalCard } from "./cards/ApprovalCard.js";
import { useReserveRows } from "./layout/viewport-budget.js";
import { MarkdownView } from "./markdown-view.js";
import { extractOpenQuestionsSection } from "./plan-open-questions.js";
import { CARD, FG, TONE } from "./theme/tokens.js";

export type PlanConfirmChoice = "approve" | "refine" | "revise" | "cancel";

export interface PlanConfirmProps {
  plan: string;
  steps?: PlanStep[];
  /** Optional human-friendly title from the model — surfaced in the header. */
  summary?: string;
  onChoose: (choice: PlanConfirmChoice) => void;
  projectRoot?: string;
}

const PLAN_BODY_PREVIEW_LINES = 24;

function PlanConfirmInner({ plan, steps, onChoose }: PlanConfirmProps) {
  const stepRows = steps?.length ?? 0;
  const hasSteps = stepRows > 0;
  const openQuestions = extractOpenQuestionsSection(plan);
  const planLines = plan.split("\n");
  const truncatedBody = planLines.length > PLAN_BODY_PREVIEW_LINES;
  const previewBody = truncatedBody ? planLines.slice(0, PLAN_BODY_PREVIEW_LINES).join("\n") : plan;
  const previewRows = truncatedBody
    ? PLAN_BODY_PREVIEW_LINES
    : Math.min(planLines.length, PLAN_BODY_PREVIEW_LINES);
  const reservedFor = hasSteps ? stepRows : previewRows;
  const oqRows = openQuestions ? openQuestions.split("\n").length : 0;
  useReserveRows("modal", { min: 10, max: Math.max(16, reservedFor + oqRows + 14) });

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
      {hasSteps ? (
        <Box marginBottom={1} flexDirection="column">
          <PlanStepList steps={steps!} />
        </Box>
      ) : plan.trim().length > 0 ? (
        <Box marginBottom={1} flexDirection="column">
          <MarkdownView text={previewBody} />
          {truncatedBody ? (
            <Text color={FG.faint}>
              {t(
                planLines.length - PLAN_BODY_PREVIEW_LINES === 1
                  ? "planFlow.truncatedBodyMore"
                  : "planFlow.truncatedBodyMorePlural",
                { n: planLines.length - PLAN_BODY_PREVIEW_LINES },
              )}
            </Text>
          ) : null}
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
      />
    </ApprovalCard>
  );
}

/** Memoized — parent re-renders every tick; props only change on user action. */
export const PlanConfirm = React.memo(PlanConfirmInner);
