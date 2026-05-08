import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import { useKeystroke } from "./keystroke-context.js";
import { PRESETS, PRESET_DESCRIPTIONS } from "./presets.js";
import { PILL_MODEL, Pill, modelBadgeFor } from "./primitives/Pill.js";
import { FG, TONE } from "./theme/tokens.js";

export type ModelPickerOutcome =
  | { kind: "select"; id: string }
  | { kind: "preset"; name: "auto" | "flash" | "pro" }
  | { kind: "quit" };

export interface ModelPickerProps {
  /** API-fetched ids; null means "still loading / offline". */
  models: ReadonlyArray<string> | null;
  /** Model id currently active in the loop — marked with the cursor on open. */
  current: string;
  /** Used to detect which preset (if any) the loop currently matches. */
  currentEffort: "high" | "max";
  currentAutoEscalate: boolean;
  onChoose: (outcome: ModelPickerOutcome) => void;
  /** Triggers a refetch when the catalog is null/empty and the user presses [r]. */
  onRefresh?: () => void;
}

const PAGE_MARGIN = 8;
const PRESET_NAMES = ["auto", "flash", "pro"] as const;
type PresetName = (typeof PRESET_NAMES)[number];

type Row = { kind: "preset"; name: PresetName } | { kind: "model"; id: string };

export function ModelPicker({
  models,
  current,
  currentEffort,
  currentAutoEscalate,
  onChoose,
  onRefresh,
}: ModelPickerProps): React.ReactElement {
  const modelList = (models && models.length > 0 ? models : FALLBACK_MODELS).slice();
  if (!modelList.includes(current)) modelList.unshift(current);
  const presetRows: Row[] = PRESET_NAMES.map((name) => ({ kind: "preset", name }));
  const modelRows: Row[] = modelList.map((id) => ({ kind: "model", id }));
  const rows: Row[] = [...presetRows, ...modelRows];

  const activePreset = detectActivePreset(current, currentEffort, currentAutoEscalate);
  const initialIndex = activePreset
    ? PRESET_NAMES.indexOf(activePreset)
    : presetRows.length + Math.max(0, modelList.indexOf(current));
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
      if (target.kind === "preset") return onChoose({ kind: "preset", name: target.name });
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

  let lastSection: "preset" | "model" | null = null;

  return (
    <Box flexDirection="column" marginY={1}>
      <Box>
        <Text bold color={TONE.brand}>
          {" ◈ REASONIX · pick a setup "}
        </Text>
        <Text color={FG.meta}>
          {loading
            ? "  ·  loading catalog…"
            : empty
              ? "  ·  catalog empty — using known fallbacks"
              : `  ·  ${modelList.length} models available`}
        </Text>
      </Box>
      <Box height={1} />
      {hiddenAbove > 0 ? (
        <Box>
          <Text color={FG.faint}>{`     … ${hiddenAbove} earlier`}</Text>
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
              {row.kind === "preset"
                ? "    PRESETS  ·  recommended — model + effort + auto-escalate"
                : "    MODELS  ·  raw pick — auto-escalate stays as-is"}
            </Text>
          </Box>
        ) : null;
        const body =
          row.kind === "preset" ? (
            <PresetRow
              key={`p-${row.name}`}
              name={row.name}
              focused={focused}
              active={activePreset === row.name}
            />
          ) : (
            <ModelRow
              key={`m-${row.id}`}
              id={row.id}
              focused={focused}
              active={!activePreset && row.id === current}
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
          <Text color={FG.faint}>{`     … ${hiddenBelow} more`}</Text>
        </Box>
      ) : null}
      <Box marginTop={1}>
        <Text color={FG.faint}>{"  ↑↓ pick  ·  ⏎ confirm  ·  [r] refresh  ·  esc cancel"}</Text>
      </Box>
    </Box>
  );
}

function PresetRow({
  name,
  focused,
  active,
}: {
  name: PresetName;
  focused: boolean;
  active: boolean;
}): React.ReactElement {
  const desc = PRESET_DESCRIPTIONS[name];
  return (
    <Box>
      <Text color={focused ? TONE.brand : FG.faint}>{focused ? "  ▸ " : "    "}</Text>
      <Text bold={focused} color={focused ? FG.strong : FG.sub}>
        {name.padEnd(8)}
      </Text>
      <Text color={focused ? FG.body : FG.meta}>{desc.headline.padEnd(28)}</Text>
      <Text color={FG.meta}>{`  ${desc.cost}`}</Text>
      {active ? <Text color={TONE.brand}>{"  · current"}</Text> : null}
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

function detectActivePreset(
  model: string,
  effort: "high" | "max",
  autoEscalate: boolean,
): PresetName | null {
  for (const name of PRESET_NAMES) {
    const p = PRESETS[name];
    if (p.model === model && p.reasoningEffort === effort && p.autoEscalate === autoEscalate) {
      return name;
    }
  }
  return null;
}

/** Hard-coded known DeepSeek ids — used when the API catalog hasn't loaded yet so the picker isn't empty on first open. */
const FALLBACK_MODELS: ReadonlyArray<string> = [
  "deepseek-v4-flash",
  "deepseek-v4-pro",
  "deepseek-chat",
  "deepseek-reasoner",
];
