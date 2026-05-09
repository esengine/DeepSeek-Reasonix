/**
 * Compact tree-style renderer for a plan's structured step list. Used
 * by PlanConfirm (on approval) and PlanCheckpointConfirm (mid-execution)
 * so the user always sees the same visual representation.
 *
 * Layout per step:
 *
 *     2/5 done (40%) · est. 5 steps
 *     ┣  ✓  step-1 · Extract tokens into a module
 *     ┣  ✓  step-2 · Migrate session cookies            ⚠ med
 *     ┣  ▸  step-3 · Update tests                       ⚠ high
 *     ┣  ○  step-4 · Run regression suite
 *     ┗  ○  step-5 · Audit every callsite
 *        ████████░░░░░░░░░░░░  40%
 *
 * Why this shape:
 *   - Status icons (✓ ▸ ○ ✗) read at a glance — color + glyph are
 *     redundant signals, useful for color-blind users and for
 *     terminals where a single bg-color cell is the only contrast.
 *   - Tree branch lines (┣ ┗) visually bind the steps as one group
 *     and mark "last step" with a corner — the eye finds the bottom
 *     without counting.
 *   - Risk only shown ≥medium. low risk on every line is noise (most
 *     steps are low-risk — that's the default). med + high are the
 *     ones that deserve attention before approve.
 *   - Bottom progress bar (24 cells of █ / ░) makes "how far in are
 *     we" answerable from the cursor's eye position alone.
 */

import { Box, Text } from "ink";
import React from "react";
import { t } from "../../i18n/index.js";
import type { PlanStep, PlanStepRisk } from "../../tools/plan.js";
import { CharBar } from "./char-bar.js";
import { COLOR, GLYPH } from "./theme.js";

export type StepStatus = "pending" | "running" | "done" | "skipped";

export interface PlanStepListProps {
  steps: PlanStep[];
  /**
   * Map of stepId → status. Missing ids default to "pending" so a
   * plan just submitted (no completions yet) renders cleanly.
   */
  statuses?: Map<string, StepStatus> | Record<string, StepStatus>;
  /**
   * Optional current step — rendered with the `cur` (▸) glyph in cyan
   * even when its status is still "pending", so the user sees which
   * one's about to run. If the step's status is "running" we always
   * use the cur glyph regardless of focusStepId.
   */
  focusStepId?: string;
}

function getStatus(stepId: string, statuses: PlanStepListProps["statuses"]): StepStatus {
  if (!statuses) return "pending";
  if (statuses instanceof Map) {
    return statuses.get(stepId) ?? "pending";
  }
  return statuses[stepId] ?? "pending";
}

interface StatusGlyph {
  glyph: string;
  color: string;
}

/**
 * Map (status, focus) → (glyph, color). Centralized so a future tweak
 * (e.g. add a "queued for retry" state) lands in one switch instead of
 * five render branches.
 */
function statusGlyph(status: StepStatus, isCur: boolean): StatusGlyph {
  if (status === "done") return { glyph: GLYPH.done, color: COLOR.ok };
  if (status === "running") return { glyph: GLYPH.cur, color: COLOR.primary };
  if (status === "skipped") return { glyph: GLYPH.fail, color: COLOR.info };
  // pending: focus override gets the cur glyph (▸) in primary color so
  // the active row pops without us needing a separate column.
  if (isCur) return { glyph: GLYPH.cur, color: COLOR.primary };
  return { glyph: GLYPH.pending, color: COLOR.info };
}

function riskLabel(risk: PlanStepRisk | undefined): { text: string; color: string } | null {
  if (risk === "med") return { text: `${GLYPH.warn} med`, color: COLOR.warn };
  if (risk === "high") return { text: `${GLYPH.warn} high`, color: COLOR.err };
  // low + undefined: omitted entirely (the default reading should be
  // "low risk" — surfacing it on every line buries the med/high ones).
  return null;
}

function PlanStepListInner({ steps, statuses, focusStepId }: PlanStepListProps) {
  if (steps.length === 0) return null;
  const statusList = steps.map((s) => getStatus(s.id, statuses));
  const total = steps.length;
  const doneCount = statusList.filter((s) => s === "done").length;
  const pct = Math.round((doneCount / total) * 100);
  // Show progress only when the plan has any motion. A freshly-submitted
  // plan with 0/N done renders without the bar to avoid an empty
  // "░░░░░░░░░░ 0%" rule that signals nothing.
  const showProgress = doneCount > 0;

  return (
    <Box flexDirection="column">
      <Box>
        <Text dimColor>
          {showProgress
            ? t(
                total === 1
                  ? "planFlow.stepList.counterDoneSingular"
                  : "planFlow.stepList.counterDone",
                { done: doneCount, total, pct },
              )
            : t(total === 1 ? "planFlow.stepList.counterSingular" : "planFlow.stepList.counter", {
                total,
              })}
        </Text>
      </Box>
      <Box flexDirection="column">
        {steps.map((step, i) => {
          const status = statusList[i]!;
          const isLast = i === total - 1;
          const isCur = focusStepId === step.id;
          const sg = statusGlyph(status, isCur);
          const risk = riskLabel(step.risk);
          const titleDim = status === "done" || status === "skipped";
          return (
            <Box key={step.id}>
              <Text color={COLOR.info} dimColor>
                {isLast ? GLYPH.branchEnd : GLYPH.branch}
              </Text>
              <Text>{"  "}</Text>
              <Text color={sg.color} bold={status === "running" || isCur}>
                {sg.glyph}
              </Text>
              <Text>{"  "}</Text>
              <Text
                dimColor={titleDim}
                bold={isCur || status === "running"}
                strikethrough={status === "done" || status === "skipped"}
              >
                {`${step.id} · ${step.title}`}
              </Text>
              {risk ? (
                <>
                  <Text>{"   "}</Text>
                  <Text color={risk.color}>{risk.text}</Text>
                </>
              ) : null}
            </Box>
          );
        })}
      </Box>
      {showProgress ? (
        <Box>
          <Text>{"      "}</Text>
          <CharBar pct={pct} width={24} />
        </Box>
      ) : null}
    </Box>
  );
}

export const PlanStepList = React.memo(PlanStepListInner);

export function riskOf(step: PlanStep): PlanStepRisk | undefined {
  return step.risk;
}
