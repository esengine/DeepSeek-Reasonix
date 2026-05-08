import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useMemo, useState } from "react";
import type { CheckpointMeta } from "../../code/checkpoints.js";
import { fmtAgo } from "../../code/checkpoints.js";
import { type PickerBroadcastPorts, usePickerBroadcast } from "./dashboard/use-picker-broadcast.js";
import { useKeystroke } from "./keystroke-context.js";
import { FG, TONE } from "./theme/tokens.js";

export type CheckpointPickerOutcome =
  | { kind: "restore"; id: string }
  | { kind: "delete"; id: string }
  | { kind: "quit" };

export interface CheckpointPickerProps {
  checkpoints: ReadonlyArray<CheckpointMeta>;
  workspace: string;
  onChoose: (outcome: CheckpointPickerOutcome) => void;
  pickerPorts?: PickerBroadcastPorts;
}

const PAGE_MARGIN = 6;

export function CheckpointPicker({
  checkpoints,
  workspace,
  onChoose,
  pickerPorts,
}: CheckpointPickerProps): React.ReactElement {
  const [focus, setFocus] = useState(0);
  const { stdout } = useStdout();
  const rows = stdout?.rows ?? 40;
  const visibleCount = Math.max(3, rows - PAGE_MARGIN);

  const snapshot = useMemo(
    () => ({
      pickerKind: "checkpoints",
      title: `restore a checkpoint — ${workspace}`,
      items: checkpoints.map((c) => {
        const sizeKb = (c.bytes / 1024).toFixed(1);
        const tag = c.source === "manual" ? "" : ` (${c.source})`;
        return {
          id: c.id,
          title: `${c.name}${tag}`,
          subtitle: `${c.fileCount} file${c.fileCount === 1 ? "" : "s"} · ${sizeKb} KB`,
          badge: c.id.slice(0, 7),
          meta: fmtAgo(c.createdAt),
        };
      }),
      actions: ["pick", "delete", "cancel"] as const,
      hint: "↑↓ pick · ⏎ restore · [d] forget · esc quit",
    }),
    [checkpoints, workspace],
  );

  usePickerBroadcast(
    !!pickerPorts,
    {
      ...snapshot,
      actions: [...snapshot.actions],
    },
    (res) => {
      if (res.action === "pick") return onChoose({ kind: "restore", id: res.id });
      if (res.action === "delete") return onChoose({ kind: "delete", id: res.id });
      if (res.action === "cancel") return onChoose({ kind: "quit" });
    },
    pickerPorts ?? {
      broadcast: () => undefined,
      resolverRef: { current: null },
      snapshotRef: { current: null },
    },
  );

  useKeystroke((ev) => {
    if (ev.escape) return onChoose({ kind: "quit" });
    if (ev.upArrow) return setFocus((f) => Math.max(0, f - 1));
    if (ev.downArrow) return setFocus((f) => Math.min(checkpoints.length - 1, f + 1));
    if (checkpoints.length === 0) {
      if (ev.return) return onChoose({ kind: "quit" });
      return;
    }
    const target = checkpoints[focus];
    if (!target) return;
    if (ev.return) return onChoose({ kind: "restore", id: target.id });
    if (ev.input === "q") return onChoose({ kind: "quit" });
    if (ev.input === "d") return onChoose({ kind: "delete", id: target.id });
  });

  const start = Math.max(
    0,
    Math.min(focus - Math.floor(visibleCount / 2), checkpoints.length - visibleCount),
  );
  const end = Math.min(checkpoints.length, start + visibleCount);
  const shown = checkpoints.slice(start, end);
  const hiddenBelow = checkpoints.length - end;

  return (
    <Box flexDirection="column" marginY={1}>
      <Box>
        <Text bold color={TONE.brand}>
          {" ◈ REASONIX · pick a checkpoint "}
        </Text>
        <Text color={FG.meta}>{`  ·  ${workspace}`}</Text>
      </Box>
      <Box height={1} />
      {checkpoints.length === 0 ? (
        <Box>
          <Text color={FG.faint}>
            {"  no checkpoints in this workspace yet — see /checkpoint to make one"}
          </Text>
        </Box>
      ) : (
        shown.map((c, i) => <CheckpointRow key={c.id} info={c} focused={start + i === focus} />)
      )}
      {hiddenBelow > 0 ? (
        <Box>
          <Text color={FG.faint}>{`     … ${hiddenBelow} more`}</Text>
        </Box>
      ) : null}
      <Box marginTop={1}>
        <Text color={FG.faint}>
          {checkpoints.length === 0
            ? "  esc quit"
            : "  ↑↓ pick  ·  ⏎ restore  ·  [d] forget  ·  esc quit"}
        </Text>
      </Box>
    </Box>
  );
}

function CheckpointRow({
  info,
  focused,
}: {
  info: CheckpointMeta;
  focused: boolean;
}): React.ReactElement {
  const tag = info.source === "manual" ? "" : ` (${info.source})`;
  const sizeKb = (info.bytes / 1024).toFixed(1);
  const time = fmtAgo(info.createdAt);
  return (
    <Box>
      <Text color={focused ? TONE.brand : FG.faint}>{focused ? "  ▸ " : "    "}</Text>
      <Text color={FG.meta}>{info.id.slice(0, 7).padEnd(8)}</Text>
      <Text bold={focused} color={focused ? FG.strong : FG.sub}>
        {info.name}
      </Text>
      <Text color={FG.faint}>{tag}</Text>
      <Box flexGrow={1} />
      <Text color={FG.faint}>{`${time.padStart(8)}  ·  `}</Text>
      <Text
        color={FG.faint}
      >{`${info.fileCount} file${info.fileCount === 1 ? "" : "s"}, ${sizeKb} KB`}</Text>
    </Box>
  );
}
