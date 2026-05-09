import { Box, Text } from "ink";
import React, { useState } from "react";
import { t } from "../../i18n/index.js";
import { ApprovalCard, type ApprovalCardProps } from "./cards/ApprovalCard.js";
import { useKeystroke } from "./keystroke-context.js";
import { MarkdownView } from "./markdown-view.js";
import { CARD, FG, TONE } from "./theme/tokens.js";
import { useTick } from "./ticker.js";

export type PlanRefineMode =
  | "approve"
  | "refine"
  | "reject"
  | "checkpoint-revise"
  | "choice-custom";

export interface PlanRefineInputProps {
  mode: PlanRefineMode;
  /** Open-questions / risks block extracted from the plan, rendered above the input on refine. */
  questions?: string;
  onSubmit: (feedback: string) => void;
  onCancel: () => void;
}

interface ModeMeta {
  title: string;
  glyph: string;
  tone: ApprovalCardProps["tone"];
  cursorColor: string;
  hint: string;
  blankHint: string;
}

const MODE_VISUALS: Record<
  PlanRefineMode,
  { glyph: string; tone: ApprovalCardProps["tone"]; cursorColor: string }
> = {
  approve: { glyph: "◇", tone: "user", cursorColor: CARD.user.color },
  refine: { glyph: "✎", tone: "warn", cursorColor: CARD.warn.color },
  reject: { glyph: "✗", tone: "error", cursorColor: CARD.error.color },
  "checkpoint-revise": { glyph: "✎", tone: "warn", cursorColor: CARD.warn.color },
  "choice-custom": { glyph: "⌥", tone: "accent", cursorColor: CARD.plan.color },
};

function modeMeta(mode: PlanRefineMode): ModeMeta {
  const v = MODE_VISUALS[mode];
  return {
    title: t(`planFlow.modes.${mode}.title`),
    hint: t(`planFlow.modes.${mode}.hint`),
    blankHint: t(`planFlow.modes.${mode}.blankHint`),
    glyph: v.glyph,
    tone: v.tone,
    cursorColor: v.cursorColor,
  };
}

export function PlanRefineInput({ mode, questions, onSubmit, onCancel }: PlanRefineInputProps) {
  const [value, setValue] = useState("");

  useKeystroke((ev) => {
    if (ev.paste) {
      setValue((v) => v + ev.input.replace(/\r?\n/g, " "));
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
      setValue((v) => v.slice(0, -1));
      return;
    }
    if (ev.input && !ev.ctrl && !ev.meta) {
      setValue((v) => v + ev.input);
    }
  });

  const tick = useTick();
  const cursorOn = Math.floor(tick / 4) % 2 === 0;
  const meta = modeMeta(mode);
  const showQuestions = mode === "refine" && !!questions && questions.trim().length > 0;

  return (
    <ApprovalCard
      tone={meta.tone}
      glyph={meta.glyph}
      title={meta.title}
      footerHint={t("planFlow.refineFooter")}
    >
      {showQuestions ? (
        <Box marginBottom={1} flexDirection="column">
          <Text color={TONE.warn} bold>
            {t("planFlow.refineQuestionsHeading")}
          </Text>
          <MarkdownView text={questions!} />
        </Box>
      ) : null}
      <Box marginBottom={1}>
        <Text color={FG.sub}>
          {meta.hint}
          {value === "" ? meta.blankHint : ""}
        </Text>
      </Box>
      <Box>
        <Text color={meta.cursorColor} bold>
          {"› "}
        </Text>
        <Text>{value}</Text>
        <Text color={meta.cursorColor} bold>
          {cursorOn ? "▍" : " "}
        </Text>
      </Box>
    </ApprovalCard>
  );
}
