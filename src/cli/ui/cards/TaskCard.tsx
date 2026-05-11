import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../../i18n/index.js";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import { PILL_PATH, PILL_SECTION, Pill } from "../primitives/Pill.js";
import type { TaskCard as TaskCardData, TaskStep } from "../state/cards.js";
import { useThemeTokens } from "../theme/context.js";

const STEP_GLYPH: Record<TaskStep["status"], string> = {
  queued: "○",
  running: "▶",
  done: "✓",
  failed: "✗",
};

const TASK_GLYPH: Record<TaskCardData["status"], string> = {
  running: "▶",
  done: "✓",
  failed: "✗",
};

const TASK_PILL: Record<TaskCardData["status"], { bg: string; fg: string }> = {
  running: PILL_SECTION.task,
  done: PILL_SECTION.taskDone,
  failed: PILL_SECTION.taskFailed,
};

export function TaskCard({ card }: { card: TaskCardData }): React.ReactElement {
  const { fg, tone } = useThemeTokens();
  const stepColor: Record<TaskStep["status"], string> = {
    queued: fg.faint,
    running: tone.warn,
    done: tone.ok,
    failed: tone.err,
  };
  const taskColor: Record<TaskCardData["status"], string> = {
    running: tone.warn,
    done: tone.ok,
    failed: tone.err,
  };
  const pill = TASK_PILL[card.status];
  const elapsed = `${(card.elapsedMs / 1000).toFixed(1)}s`;
  return (
    <Card tone={taskColor[card.status]}>
      <CardHeader
        glyph={TASK_GLYPH[card.status]}
        tone={taskColor[card.status]}
        title={t("cardTitles.task")}
        titleColor={pill.fg}
        titleBg={pill.bg}
        subtitle={`${card.index} / ${card.total}  ${card.title}`}
        meta={[elapsed, card.status]}
      />
      {card.steps.map((step) => (
        <Box key={step.id} flexDirection="row" gap={1}>
          <Text color={stepColor[step.status]}>{STEP_GLYPH[step.status]}</Text>
          <Text bold color={fg.body}>
            {(step.toolName ?? t("cardLabels.stepLabel")).padEnd(7)}
          </Text>
          <Pill label={step.title} {...PILL_PATH} bold={false} />
          {step.detail ? <Text color={fg.faint}>{step.detail}</Text> : null}
          {step.elapsedMs !== undefined ? (
            <Text color={fg.faint}>{`${(step.elapsedMs / 1000).toFixed(2)}s`}</Text>
          ) : null}
        </Box>
      ))}
    </Card>
  );
}
