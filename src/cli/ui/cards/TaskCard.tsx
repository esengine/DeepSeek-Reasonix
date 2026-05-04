import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import type { TaskCard as TaskCardData, TaskStep } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";

const STEP_GLYPH: Record<TaskStep["status"], string> = {
  queued: "○",
  running: "▶",
  done: "✓",
  failed: "✗",
};

const STEP_COLOR: Record<TaskStep["status"], string> = {
  queued: FG.faint,
  running: TONE.brand,
  done: TONE.ok,
  failed: TONE.err,
};

const TASK_COLOR: Record<TaskCardData["status"], string> = {
  running: TONE.brand,
  done: TONE.ok,
  failed: TONE.err,
};

const TASK_GLYPH: Record<TaskCardData["status"], string> = {
  running: "▶",
  done: "✓",
  failed: "✗",
};

export function TaskCard({ card }: { card: TaskCardData }): React.ReactElement {
  const elapsed = `${(card.elapsedMs / 1000).toFixed(1)}s`;
  const meta = (
    <>
      <Text color={FG.body}>{card.title}</Text>
      <Text color={FG.faint}>{`· ${elapsed} · ${card.status}`}</Text>
    </>
  );
  return (
    <CardLayout
      glyph={TASK_GLYPH[card.status]}
      tone={TASK_COLOR[card.status]}
      title={`step ${card.index}/${card.total}`}
      meta={meta}
    >
      {card.steps.map((step) => (
        <Box key={step.id} flexDirection="row" gap={1}>
          <Text color={STEP_COLOR[step.status]}>{STEP_GLYPH[step.status]}</Text>
          <Text bold color={FG.body}>
            {(step.toolName ?? "step").padEnd(7)}
          </Text>
          <Text color={FG.sub}>{step.title}</Text>
          {step.detail ? <Text color={FG.faint}>{step.detail}</Text> : null}
          {step.elapsedMs !== undefined ? (
            <Text color={FG.faint}>{`${(step.elapsedMs / 1000).toFixed(2)}s`}</Text>
          ) : null}
        </Box>
      ))}
    </CardLayout>
  );
}
