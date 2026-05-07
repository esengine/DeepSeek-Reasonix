import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import { useKeystroke } from "./keystroke-context.js";
import { PILL_MODEL, Pill, modelBadgeFor } from "./primitives/Pill.js";
import { FG, TONE } from "./theme/tokens.js";

export type ModelPickerOutcome = { kind: "select"; id: string } | { kind: "quit" };

export interface ModelPickerProps {
  /** API-fetched ids; null means "still loading / offline". */
  models: ReadonlyArray<string> | null;
  /** Model id currently active in the loop — marked with the cursor on open. */
  current: string;
  onChoose: (outcome: ModelPickerOutcome) => void;
  /** Triggers a refetch when the catalog is null/empty and the user presses [r]. */
  onRefresh?: () => void;
}

const PAGE_MARGIN = 6;

export function ModelPicker({
  models,
  current,
  onChoose,
  onRefresh,
}: ModelPickerProps): React.ReactElement {
  const list = (models && models.length > 0 ? models : FALLBACK_MODELS).slice();
  if (!list.includes(current)) list.unshift(current);
  const initialIndex = Math.max(0, list.indexOf(current));
  const [focus, setFocus] = useState(initialIndex);
  const { stdout } = useStdout();
  const rows = stdout?.rows ?? 40;
  const visibleCount = Math.max(3, rows - PAGE_MARGIN);

  useKeystroke((ev) => {
    if (ev.escape) return onChoose({ kind: "quit" });
    if (ev.upArrow) return setFocus((f) => Math.max(0, f - 1));
    if (ev.downArrow) return setFocus((f) => Math.min(list.length - 1, f + 1));
    if (ev.return) {
      const target = list[focus];
      if (target) onChoose({ kind: "select", id: target });
      return;
    }
    if (!ev.input) return;
    if (ev.input === "q") return onChoose({ kind: "quit" });
    if (ev.input === "r") onRefresh?.();
  });

  const start = Math.max(
    0,
    Math.min(focus - Math.floor(visibleCount / 2), list.length - visibleCount),
  );
  const end = Math.min(list.length, start + visibleCount);
  const shown = list.slice(start, end);
  const hiddenAbove = start;
  const hiddenBelow = list.length - end;
  const loading = models === null;
  const empty = models !== null && models.length === 0;

  return (
    <Box flexDirection="column" marginY={1}>
      <Box>
        <Text bold color={TONE.brand}>
          {" ◈ REASONIX · pick a model "}
        </Text>
        <Text color={FG.meta}>
          {loading
            ? "  ·  loading catalog…"
            : empty
              ? "  ·  catalog empty — using known fallbacks"
              : `  ·  ${list.length} available`}
        </Text>
      </Box>
      <Box height={1} />
      {hiddenAbove > 0 ? (
        <Box>
          <Text color={FG.faint}>{`     … ${hiddenAbove} earlier`}</Text>
        </Box>
      ) : null}
      {shown.map((id, i) => (
        <ModelRow key={id} id={id} focused={start + i === focus} active={id === current} />
      ))}
      {hiddenBelow > 0 ? (
        <Box>
          <Text color={FG.faint}>{`     … ${hiddenBelow} more`}</Text>
        </Box>
      ) : null}
      <Box marginTop={1}>
        <Text color={FG.faint}>{"  ↑↓ pick  ·  ⏎ confirm  ·  [r] refresh  ·  esc cancel"}</Text>
      </Box>
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
      {active ? <Text color={TONE.brand}>{"  · current"}</Text> : null}
    </Box>
  );
}

/** Hard-coded known DeepSeek ids — used when the API catalog hasn't loaded yet so the picker isn't empty on first open. */
const FALLBACK_MODELS: ReadonlyArray<string> = [
  "deepseek-v4-flash",
  "deepseek-v4-pro",
  "deepseek-chat",
  "deepseek-reasoner",
];
