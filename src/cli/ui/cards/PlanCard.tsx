import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import type { PlanCard as PlanCardData, PlanStep } from "../state/cards.js";
import { FG, TONE, TONE_ACTIVE } from "../theme/tokens.js";

const STATUS_GLYPH: Record<PlanStep["status"], string> = {
  queued: "○",
  running: "▶",
  done: "✓",
  failed: "✗",
  blocked: "!",
  skipped: "s",
};

const STATUS_COLOR: Record<PlanStep["status"], string> = {
  queued: FG.faint,
  running: TONE_ACTIVE.brand,
  done: TONE.ok,
  failed: TONE.err,
  blocked: TONE.warn,
  skipped: FG.faint,
};

const VISIBLE_WINDOW = 5;

export function PlanCard({ card }: { card: PlanCardData }): React.ReactElement {
  const doneCount = card.steps.filter((s) => s.status === "done").length;
  const variantTag =
    card.variant === "resumed" ? "resumed · " : card.variant === "replay" ? "⏪ archive · " : "";
  const progress = `${variantTag}${doneCount}/${card.steps.length} done`;
  const hasRunning = card.steps.some((s) => s.status === "running");
  const tone = hasRunning ? TONE_ACTIVE.accent : TONE.accent;

  const window = pickWindow(card.steps);

  return (
    <Card tone={tone}>
      <CardHeader glyph="⊞" tone={tone} title={card.title} meta={[progress]} />
      {window.hiddenBefore > 0 ? (
        <Box flexDirection="row" gap={1}>
          <Text color={TONE.ok}>✓</Text>
          <Text color={FG.faint}>{`⋯ ${window.hiddenBefore} done`}</Text>
        </Box>
      ) : null}
      {window.steps.map((step) => {
        const isActive = step.status === "running";
        const titleColor = isActive ? FG.strong : FG.sub;
        return (
          <Box key={step.id} flexDirection="row" gap={1}>
            <Text color={STATUS_COLOR[step.status]}>{STATUS_GLYPH[step.status]}</Text>
            <Text bold={isActive} color={titleColor}>
              {`${step.indexLabel}. ${step.title}`}
            </Text>
            {isActive ? <Text color={TONE_ACTIVE.brand}>← in progress</Text> : null}
          </Box>
        );
      })}
      {window.hiddenAfter > 0 ? (
        <Box flexDirection="row" gap={1}>
          <Text color={FG.faint}>○</Text>
          <Text color={FG.faint}>{`⋯ ${window.hiddenAfter} upcoming`}</Text>
        </Box>
      ) : null}
    </Card>
  );
}

interface WindowedStep extends PlanStep {
  indexLabel: number;
}

interface StepWindow {
  steps: WindowedStep[];
  hiddenBefore: number;
  hiddenAfter: number;
}

/** Fixed window keeps the live strip's height constant — variable-height plan cards in the live region cause Yoga to thrash on every step transition. */
function pickWindow(steps: ReadonlyArray<PlanStep>): StepWindow {
  if (steps.length <= VISIBLE_WINDOW) {
    return {
      steps: steps.map((s, i) => ({ ...s, indexLabel: i + 1 })),
      hiddenBefore: 0,
      hiddenAfter: 0,
    };
  }
  const anchor = anchorIndex(steps);
  const start = Math.max(0, Math.min(anchor, steps.length - VISIBLE_WINDOW));
  const end = start + VISIBLE_WINDOW;
  return {
    steps: steps.slice(start, end).map((s, i) => ({ ...s, indexLabel: start + i + 1 })),
    hiddenBefore: start,
    hiddenAfter: Math.max(0, steps.length - end),
  };
}

function anchorIndex(steps: ReadonlyArray<PlanStep>): number {
  const runningIdx = steps.findIndex((s) => s.status === "running");
  if (runningIdx >= 0) return runningIdx;
  const firstPending = steps.findIndex((s) => s.status === "queued" || s.status === "blocked");
  if (firstPending >= 0) return firstPending;
  return Math.max(0, steps.length - VISIBLE_WINDOW);
}
