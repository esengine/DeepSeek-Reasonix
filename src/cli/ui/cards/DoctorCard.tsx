import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../../i18n/index.js";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import type { DoctorCard as DoctorCardData, DoctorCheckEntry } from "../state/cards.js";
import { useThemeTokens } from "../theme/context.js";
import { CARD } from "../theme/tokens.js";

const LEVEL_GLYPH: Record<DoctorCheckEntry["level"], string> = {
  ok: "✓",
  warn: "⚠",
  fail: "✖",
};

function levelTag(level: DoctorCheckEntry["level"]): string {
  switch (level) {
    case "ok":
      return t("cardLabels.levelOk");
    case "warn":
      return t("cardLabels.levelWarn");
    case "fail":
      return t("cardLabels.levelFail");
  }
}

export function DoctorCard({ card }: { card: DoctorCardData }): React.ReactElement {
  const { fg, tone } = useThemeTokens();
  const levelColor: Record<DoctorCheckEntry["level"], string> = {
    ok: tone.ok,
    warn: tone.warn,
    fail: tone.err,
  };
  const ok = card.checks.filter((c) => c.level === "ok").length;
  const warn = card.checks.filter((c) => c.level === "warn").length;
  const fail = card.checks.filter((c) => c.level === "fail").length;
  const labelWidth = card.checks.reduce((m, c) => Math.max(m, c.label.length), 0);
  const summary = `${card.checks.length} ${t("cardLabels.checksLabel")} · ${ok} ${t("cardLabels.passed")}${warn > 0 ? ` · ${warn} ${t("cardLabels.warnTag")}` : ""}${fail > 0 ? ` · ${fail} ${t("cardLabels.failTag")}` : ""}`;

  return (
    <Card tone={CARD.tool.color}>
      <CardHeader
        glyph="⚕"
        tone={CARD.tool.color}
        title={t("cardTitles.doctor")}
        meta={[summary]}
      />
      {card.checks.map((c) => (
        <Box key={c.label} flexDirection="row" gap={1}>
          <Text color={levelColor[c.level]}>{LEVEL_GLYPH[c.level]}</Text>
          <Text bold color={fg.body}>
            {c.label.padEnd(labelWidth + 1)}
          </Text>
          <Text color={fg.sub}>{c.detail}</Text>
          <Text color={levelColor[c.level]}>{levelTag(c.level)}</Text>
        </Box>
      ))}
    </Card>
  );
}
