import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import type { DoctorCard as DoctorCardData, DoctorCheckEntry } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";

const LEVEL_COLOR: Record<DoctorCheckEntry["level"], string> = {
  ok: TONE.ok,
  warn: TONE.warn,
  fail: TONE.err,
};

const LEVEL_GLYPH: Record<DoctorCheckEntry["level"], string> = {
  ok: "✓",
  warn: "⚠",
  fail: "✗",
};

const LEVEL_TAG: Record<DoctorCheckEntry["level"], string> = {
  ok: "OK",
  warn: "warn",
  fail: "FAIL",
};

export function DoctorCard({ card }: { card: DoctorCardData }): React.ReactElement {
  const ok = card.checks.filter((c) => c.level === "ok").length;
  const warn = card.checks.filter((c) => c.level === "warn").length;
  const fail = card.checks.filter((c) => c.level === "fail").length;
  const summary = `${card.checks.length} checks · ${ok} passed${warn > 0 ? ` · ${warn} warn` : ""}${fail > 0 ? ` · ${fail} fail` : ""}`;
  const labelWidth = card.checks.reduce((m, c) => Math.max(m, c.label.length), 0);
  const headerTone = fail > 0 ? TONE.err : warn > 0 ? TONE.warn : TONE.ok;

  return (
    <CardLayout glyph="⚕" tone={headerTone} title="doctor" meta={summary}>
      {card.checks.map((c) => (
        <Box key={c.label} flexDirection="row" gap={1}>
          <Text color={LEVEL_COLOR[c.level]}>{LEVEL_GLYPH[c.level]}</Text>
          <Text bold color={FG.body}>
            {c.label.padEnd(labelWidth)}
          </Text>
          <Text color={FG.sub}>{c.detail}</Text>
          <Text color={LEVEL_COLOR[c.level]}>{LEVEL_TAG[c.level]}</Text>
        </Box>
      ))}
    </CardLayout>
  );
}
