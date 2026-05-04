import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import type { PlanCard as PlanCardData, PlanStep } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

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
  running: TONE.brand,
  done: TONE.ok,
  failed: TONE.err,
  blocked: TONE.warn,
  skipped: FG.faint,
};

export function PlanCard({ card }: { card: PlanCardData }): React.ReactElement {
  const doneCount = card.steps.filter((s) => s.status === "done").length;
  const variantTag =
    card.variant === "resumed" ? "resumed · " : card.variant === "replay" ? "⏪ archive · " : "";
  const progress = `${variantTag}${doneCount}/${card.steps.length} done`;

  return (
    <Box flexDirection="column" marginTop={1}>
      <Box flexDirection="row" gap={1}>
        <Text color={TONE.accent}>⊞</Text>
        <Text color={TONE.accent} bold>
          {card.title}
        </Text>
        <Text color={FG.faint}>{`· ${progress}`}</Text>
      </Box>
      {card.steps.map((step, i) => {
        const isActive = step.status === "running";
        const titleColor = isActive ? FG.strong : FG.sub;
        return (
          <Box key={step.id} paddingLeft={2} flexDirection="row" gap={1}>
            <Text color={STATUS_COLOR[step.status]}>{STATUS_GLYPH[step.status]}</Text>
            <Text bold={isActive} color={titleColor}>
              {`${i + 1}. ${step.title}`}
            </Text>
            {isActive ? <Text color={TONE.brand}>← in progress</Text> : null}
          </Box>
        );
      })}
    </Box>
  );
}
