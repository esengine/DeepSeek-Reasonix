import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import type { SessionInfo } from "../../memory/session.js";
import { useKeystroke } from "./keystroke-context.js";
import { FG, TONE, formatCost } from "./theme/tokens.js";

export type SessionPickerOutcome =
  | { kind: "open"; name: string }
  | { kind: "new" }
  | { kind: "delete"; name: string }
  | { kind: "rename"; name: string; newName: string }
  | { kind: "quit" };

export interface SessionPickerProps {
  sessions: ReadonlyArray<SessionInfo>;
  workspace: string;
  onChoose: (outcome: SessionPickerOutcome) => void;
  /** Live wallet currency from App.tsx; falls back to each session's stored `meta.balanceCurrency` per row. */
  walletCurrency?: string;
}

const PAGE_MARGIN = 6;

export function SessionPicker({
  sessions,
  workspace,
  onChoose,
  walletCurrency,
}: SessionPickerProps): React.ReactElement {
  const [focus, setFocus] = useState(0);
  const [renaming, setRenaming] = useState<{ from: string; buf: string } | null>(null);
  const { stdout } = useStdout();
  const rows = stdout?.rows ?? 40;
  const visibleCount = Math.max(3, rows - PAGE_MARGIN);

  useKeystroke((ev) => {
    if (ev.paste) {
      if (renaming) setRenaming({ ...renaming, buf: renaming.buf + ev.input });
      return;
    }
    if (renaming) {
      if (ev.escape) return setRenaming(null);
      if (ev.return) {
        const newName = renaming.buf.trim();
        if (newName.length === 0 || newName === renaming.from) {
          setRenaming(null);
          return;
        }
        onChoose({ kind: "rename", name: renaming.from, newName });
        setRenaming(null);
        return;
      }
      if (ev.backspace) {
        setRenaming({ ...renaming, buf: renaming.buf.slice(0, -1) });
        return;
      }
      if (ev.input && !ev.ctrl && !ev.meta && !ev.tab) {
        setRenaming({ ...renaming, buf: renaming.buf + ev.input });
      }
      return;
    }
    if (ev.escape) return onChoose({ kind: "quit" });
    if (ev.upArrow) return setFocus((f) => Math.max(0, f - 1));
    if (ev.downArrow) return setFocus((f) => Math.min(sessions.length, f + 1));
    if (ev.return) {
      if (sessions.length === 0 || focus === sessions.length) return onChoose({ kind: "new" });
      const target = sessions[focus]!;
      return onChoose({ kind: "open", name: target.name });
    }
    if (!ev.input) return;
    if (ev.input === "n") return onChoose({ kind: "new" });
    if (ev.input === "q") return onChoose({ kind: "quit" });
    if (sessions.length === 0) return;
    const target = sessions[focus];
    if (!target) return;
    if (ev.input === "d") return onChoose({ kind: "delete", name: target.name });
    if (ev.input === "r") return setRenaming({ from: target.name, buf: "" });
  });

  const start = Math.max(
    0,
    Math.min(focus - Math.floor(visibleCount / 2), sessions.length - visibleCount),
  );
  const end = Math.min(sessions.length, start + visibleCount);
  const shown = sessions.slice(start, end);
  const hiddenBelow = sessions.length - end;

  return (
    <Box flexDirection="column" marginY={1}>
      <Box>
        <Text bold color={TONE.brand}>
          {" ◈ REASONIX · pick a session "}
        </Text>
        <Text color={FG.meta}>{`  ·  ${workspace}`}</Text>
      </Box>
      <Box height={1} />
      {sessions.length === 0 ? (
        <Box>
          <Text color={FG.faint}>{"  no saved sessions in this workspace yet — press "}</Text>
          <Text bold color={TONE.brand}>
            {"⏎"}
          </Text>
          <Text color={FG.faint}>{" to start a new one"}</Text>
        </Box>
      ) : (
        shown.map((s, i) => (
          <SessionRow
            key={s.name}
            info={s}
            focused={start + i === focus}
            walletCurrency={walletCurrency}
          />
        ))
      )}
      {hiddenBelow > 0 ? (
        <Box>
          <Text color={FG.faint}>{`     … ${hiddenBelow} more`}</Text>
        </Box>
      ) : null}
      {renaming ? (
        <Box marginTop={1}>
          <Text color={FG.faint}>{`  rename "${renaming.from}" → `}</Text>
          <Text bold color={TONE.brand}>
            {renaming.buf}
          </Text>
          <Text backgroundColor={TONE.brand} color="black">
            {" "}
          </Text>
        </Box>
      ) : null}
      <Box marginTop={1}>
        <Text color={FG.faint}>
          {renaming
            ? "  ⏎ confirm rename  ·  esc cancel"
            : sessions.length === 0
              ? "  ⏎ new session  ·  esc quit"
              : "  ↑↓ pick  ·  ⏎ open  ·  [n] new  ·  [d] delete  ·  [r] rename  ·  esc quit"}
        </Text>
      </Box>
    </Box>
  );
}

function SessionRow({
  info,
  focused,
  walletCurrency,
}: {
  info: SessionInfo;
  focused: boolean;
  walletCurrency?: string;
}): React.ReactElement {
  const branch = info.meta.branch ?? "main";
  const summary =
    info.meta.summary ?? `${info.messageCount} message${info.messageCount === 1 ? "" : "s"}`;
  const turns = info.meta.turnCount ?? Math.ceil(info.messageCount / 2);
  const currency = walletCurrency ?? info.meta.balanceCurrency;
  const costLabel =
    info.meta.totalCostUsd !== undefined ? formatCost(info.meta.totalCostUsd, currency, 2) : "";
  const time = relativeTime(info.mtime);
  return (
    <Box>
      <Text color={focused ? TONE.brand : FG.faint}>{focused ? "  ▸ " : "    "}</Text>
      <Text bold={focused} color={focused ? FG.strong : FG.sub}>
        {info.name.padEnd(12)}
      </Text>
      <Text color={FG.meta}>{` · ${branch.padEnd(8)} · `}</Text>
      <Text color={focused ? FG.body : FG.sub}>{truncate(summary, 40)}</Text>
      <Box flexGrow={1} />
      <Text color={FG.faint}>{`${time.padStart(11)}   `}</Text>
      <Text color={FG.faint}>{`${turns} turns`}</Text>
      {costLabel ? <Text color={FG.faint}>{` · ${costLabel}`}</Text> : null}
    </Box>
  );
}

function truncate(s: string, max: number): string {
  if (s.length <= max) return s;
  return `${s.slice(0, max - 1)}…`;
}

function relativeTime(date: Date): string {
  const ms = Date.now() - date.getTime();
  const mins = Math.floor(ms / 60_000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins} min ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days === 1) return "yesterday";
  if (days < 7) return `${days} days ago`;
  return date.toISOString().slice(0, 10);
}
