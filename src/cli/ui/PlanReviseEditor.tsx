import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import { t } from "../../i18n/index.js";
import type { PlanStep } from "../../tools/plan.js";
import { ApprovalCard } from "./cards/ApprovalCard.js";
import { useKeystroke } from "./keystroke-context.js";
import { FG, TONE } from "./theme/tokens.js";

export interface PlanReviseEditorProps {
  steps: PlanStep[];
  /** stepId set the model has already marked done — those rows render `[✓]` and are not editable. */
  completedStepIds?: Set<string>;
  onAccept: (revised: PlanStep[], skippedIds: ReadonlyArray<string>) => void;
  onCancel: () => void;
}

interface RowState {
  step: PlanStep;
  done: boolean;
  skipped: boolean;
}

export function PlanReviseEditor({
  steps,
  completedStepIds,
  onAccept,
  onCancel,
}: PlanReviseEditorProps): React.ReactElement {
  const [rows, setRows] = useState<RowState[]>(() =>
    steps.map((s) => ({ step: s, done: completedStepIds?.has(s.id) ?? false, skipped: false })),
  );
  const firstEditableIndex = rows.findIndex((r) => !r.done);
  const [focus, setFocus] = useState<number>(firstEditableIndex < 0 ? 0 : firstEditableIndex);

  useKeystroke((ev) => {
    if (ev.paste) return;
    if (ev.escape) {
      onCancel();
      return;
    }
    if (ev.return) {
      const revised = rows.map((r) => r.step);
      const skippedIds = rows.filter((r) => r.skipped).map((r) => r.step.id);
      onAccept(revised, skippedIds);
      return;
    }
    if (ev.upArrow) {
      setFocus((f) => Math.max(0, f - 1));
      return;
    }
    if (ev.downArrow) {
      setFocus((f) => Math.min(rows.length - 1, f + 1));
      return;
    }
    const ch = ev.input;
    if (ch === " ") {
      setRows((prev) => {
        const next = [...prev];
        const cur = next[focus];
        if (!cur || cur.done) return prev;
        next[focus] = { ...cur, skipped: !cur.skipped };
        return next;
      });
      return;
    }
    if (ch === "k") {
      // Move focused row up; swap with predecessor (if both editable).
      setRows((prev) => {
        if (focus <= 0) return prev;
        const a = prev[focus - 1];
        const b = prev[focus];
        if (!a || !b || a.done || b.done) return prev;
        const next = [...prev];
        next[focus - 1] = b;
        next[focus] = a;
        return next;
      });
      setFocus((f) => Math.max(0, f - 1));
      return;
    }
    if (ch === "j") {
      setRows((prev) => {
        if (focus >= prev.length - 1) return prev;
        const a = prev[focus];
        const b = prev[focus + 1];
        if (!a || !b || a.done || b.done) return prev;
        const next = [...prev];
        next[focus] = b;
        next[focus + 1] = a;
        return next;
      });
      setFocus((f) => Math.min(rows.length - 1, f + 1));
      return;
    }
  });

  return (
    <ApprovalCard
      tone="accent"
      glyph="✎"
      title={t("planFlow.reviseTitle")}
      metaRight={t("planFlow.reviseSteps", { count: rows.length })}
      footerHint={t("planFlow.reviseFooter")}
    >
      {rows.map((r, i) => (
        <ReviseRow key={r.step.id} row={r} index={i} focused={i === focus} />
      ))}
    </ApprovalCard>
  );
}

function ReviseRow({
  row,
  index,
  focused,
}: {
  row: RowState;
  index: number;
  focused: boolean;
}): React.ReactElement {
  const marker = row.done ? "[✓]" : row.skipped ? "[s]" : focused ? "[ ]" : "[ ]";
  const markerColor = row.done ? TONE.ok : row.skipped ? FG.faint : focused ? TONE.brand : FG.faint;
  const titleColor = row.done ? FG.sub : row.skipped ? FG.faint : focused ? FG.strong : FG.sub;
  const focusGlyph = focused ? <Text color={TONE.brand}>{"▸ "}</Text> : <Text>{"  "}</Text>;
  return (
    <Box>
      {focusGlyph}
      <Text color={markerColor}>{marker}</Text>
      <Text color={titleColor} bold={focused} italic={row.skipped} strikethrough={row.skipped}>
        {` ${index + 1}. ${row.step.title}`}
      </Text>
      {row.skipped ? <Text color={TONE.warn}>{"     ← skipped"}</Text> : null}
    </Box>
  );
}
