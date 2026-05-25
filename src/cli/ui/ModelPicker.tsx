import { Box, Text, useStdout } from "ink";
import React, { useState } from "react";
import type { ReasoningEffort } from "../../config.js";
import { t } from "../../i18n/index.js";
import { useKeystroke } from "./keystroke-context.js";
import { PILL_MODEL, Pill, modelBadgeFor } from "./primitives/Pill.js";
import { FG, TONE } from "./theme/tokens.js";

export type ModelPickerOutcome =
  | { kind: "select"; id: string }
  | { kind: "effort"; effort: ReasoningEffort }
  | { kind: "quit" };

export interface ModelPickerProps {
  /** API-fetched ids; null means "still loading / offline". */
  models: ReadonlyArray<string> | null;
  /** Model id currently active in the loop — marked with the cursor on open. */
  current: string;
  currentEffort: ReasoningEffort;
  /** Effort enum filtered for the active endpoint — drops "max" on non-DeepSeek hosts (#1794). */
  effortChoices: ReadonlyArray<ReasoningEffort>;
  onChoose: (outcome: ModelPickerOutcome) => void;
  /** Triggers a refetch when the catalog is null/empty and the user presses [r]. */
  onRefresh?: () => void;
}

const PAGE_MARGIN = 8;

type Row = { kind: "effort"; effort: ReasoningEffort } | { kind: "model"; id: string };

export function ModelPicker({
  models,
  current,
  currentEffort,
  effortChoices,
  onChoose,
  onRefresh,
}: ModelPickerProps): React.ReactElement {
  const modelList = (models && models.length > 0 ? models : FALLBACK_MODELS).slice();
  if (!modelList.includes(current)) modelList.unshift(current);

  const effortRows: Row[] = effortChoices.map((effort) => ({
    kind: "effort",
    effort,
  }));
  const modelRows: Row[] = modelList.map((id) => ({ kind: "model", id }));
  const rows: Row[] = [...effortRows, ...modelRows];

  const initialIndex = effortRows.length + Math.max(0, modelList.indexOf(current));
  const [focus, setFocus] = useState(initialIndex);
  const { stdout } = useStdout();
  const termRows = stdout?.rows ?? 40;
  const visibleCount = Math.max(6, termRows - PAGE_MARGIN);

  useKeystroke((ev) => {
    if (ev.escape) return onChoose({ kind: "quit" });
    if (ev.upArrow) return setFocus((f) => Math.max(0, f - 1));
    if (ev.downArrow) return setFocus((f) => Math.min(rows.length - 1, f + 1));
    if (ev.return) {
      const target = rows[focus];
      if (!target) return;
      if (target.kind === "effort") return onChoose({ kind: "effort", effort: target.effort });
      return onChoose({ kind: "select", id: target.id });
    }
    if (!ev.input) return;
    if (ev.input === "q") return onChoose({ kind: "quit" });
    if (ev.input === "r") onRefresh?.();
  });

  const start = Math.max(
    0,
    Math.min(focus - Math.floor(visibleCount / 2), rows.length - visibleCount),
  );
  const end = Math.min(rows.length, start + visibleCount);
  const shown = rows.slice(start, end);
  const hiddenAbove = start;
  const hiddenBelow = rows.length - end;
  const loading = models === null;
  const empty = models !== null && models.length === 0;

  let lastSection: Row["kind"] | null = null;

  return (
    <Box flexDirection="column" marginY={1}>
      <Box>
        <Text bold color={TONE.brand}>
          {t("modelPicker.header")}
        </Text>
        <Text color={FG.meta}>
          {loading
            ? t("modelPicker.loading")
            : empty
              ? t("modelPicker.catalogEmpty")
              : t("modelPicker.modelsAvailable", { count: modelList.length })}
        </Text>
      </Box>
      <Box height={1} />
      {hiddenAbove > 0 ? (
        <Box>
          <Text color={FG.faint}>{`     … ${hiddenAbove}`}</Text>
        </Box>
      ) : null}
      {shown.map((row, i) => {
        const idx = start + i;
        const focused = idx === focus;
        const showHeader = row.kind !== lastSection;
        lastSection = row.kind;
        const header = showHeader ? (
          <Box key={`hdr-${row.kind}`} marginTop={idx === 0 ? 0 : 1}>
            <Text color={FG.meta}>
              {row.kind === "effort"
                ? t("modelPicker.effortHeader")
                : t("modelPicker.modelsHeader")}
            </Text>
          </Box>
        ) : null;
        const body =
          row.kind === "effort" ? (
            <EffortRow
              key={`e-${row.effort}`}
              effort={row.effort}
              focused={focused}
              active={row.effort === currentEffort}
            />
          ) : (
            <ModelRow
              key={`m-${row.id}`}
              id={row.id}
              focused={focused}
              active={row.id === current}
            />
          );
        return (
          <React.Fragment key={`row-${idx}`}>
            {header}
            {body}
          </React.Fragment>
        );
      })}
      {hiddenBelow > 0 ? (
        <Box>
          <Text color={FG.faint}>{t("cardLabels.more", { count: hiddenBelow })}</Text>
        </Box>
      ) : null}
      <Box marginTop={1}>
        <Text color={FG.faint}>{t("modelPicker.pickerFooter")}</Text>
      </Box>
    </Box>
  );
}

function EffortRow({
  effort,
  focused,
  active,
}: {
  effort: ReasoningEffort;
  focused: boolean;
  active: boolean;
}): React.ReactElement {
  return (
    <Box>
      <Text color={focused ? TONE.brand : FG.faint}>{focused ? "  ▸ " : "    "}</Text>
      <Text bold={focused} color={focused ? FG.strong : FG.sub}>
        {effort.padEnd(8)}
      </Text>
      <Text color={FG.meta}>{t(`modelPicker.effortDesc.${effort}` as const)}</Text>
      {active ? <Text color={TONE.brand}>{t("modelPicker.currentLabel")}</Text> : null}
    </Box>
  );
}

function ModelRow({
  id,
  focused,
  active,
}: {
  id: string;
  focused: boolean;
  active: boolean;
}): React.ReactElement {
  const badge = modelBadgeFor(id);
  return (
    <Box>
      <Text color={focused ? TONE.brand : FG.faint}>{focused ? "  ▸ " : "    "}</Text>
      <Text bold={focused} color={focused ? FG.strong : FG.sub}>
        {id.padEnd(24)}
      </Text>
      <Text> </Text>
      <Pill label={badge.label} {...PILL_MODEL[badge.kind]} bold={false} />
      {active ? <Text color={TONE.brand}>{t("modelPicker.currentLabel")}</Text> : null}
    </Box>
  );
}

const FALLBACK_MODELS: ReadonlyArray<string> = ["deepseek-v4-flash", "deepseek-v4-pro"];
