import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useEffect, useMemo, useState } from "react";
import { t } from "../../i18n/index.js";
import type { SessionInfo } from "../../memory/session.js";
import { type PickerBroadcastPorts, usePickerBroadcast } from "./dashboard/use-picker-broadcast.js";
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
  /** When provided, broadcasts to the web dashboard so it can resolve via `/api/modal/resolve`. */
  pickerPorts?: PickerBroadcastPorts;
  onFocusChange?: (focus: number) => void;
}

const PAGE_MARGIN = 6;

export function SessionPicker({
  sessions,
  workspace,
  onChoose,
  walletCurrency,
  pickerPorts,
  onFocusChange,
}: SessionPickerProps): React.ReactElement {
  const [focus, setFocus] = useState(0);

  useEffect(() => {
    onFocusChange?.(focus);
  }, [focus, onFocusChange]);
  const [renaming, setRenaming] = useState<{ from: string; buf: string } | null>(null);
  const { stdout } = useStdout();
  const rows = stdout?.rows ?? 40;
  const visibleCount = Math.max(3, rows - PAGE_MARGIN);

  const snapshot = useMemo(
    () => ({
      pickerKind: "sessions",
      title: t("sessionPicker.title", { workspace }),
      items: sessions.map((s) => {
        const branch = s.meta.branch ?? "main";
        const count = s.messageCount;
        const summary =
          s.meta.summary ??
          t(count === 1 ? "sessionPicker.messages" : "sessionPicker.messagesPlural", { count });
        const turns = s.meta.turnCount ?? Math.ceil(s.messageCount / 2);
        const currency = walletCurrency ?? s.meta.balanceCurrency;
        const costLabel =
          s.meta.totalCostUsd !== undefined ? formatCost(s.meta.totalCostUsd, currency, 2) : "";
        return {
          id: s.name,
          title: s.name,
          subtitle: summary,
          badge: branch,
          meta: costLabel
            ? `${t("sessionPicker.turns", { count: turns })} · ${costLabel}`
            : t("sessionPicker.turns", { count: turns }),
        };
      }),
      actions: ["pick", "delete", "rename", "new", "cancel"] as const,
      hint: t("sessionPicker.pickerHint"),
    }),
    [sessions, workspace, walletCurrency],
  );

  usePickerBroadcast(
    !!pickerPorts,
    {
      ...snapshot,
      actions: [...snapshot.actions],
    },
    (res) => {
      if (res.action === "pick") return onChoose({ kind: "open", name: res.id });
      if (res.action === "delete") return onChoose({ kind: "delete", name: res.id });
      if (res.action === "rename")
        return onChoose({ kind: "rename", name: res.id, newName: res.text });
      if (res.action === "new") return onChoose({ kind: "new" });
      if (res.action === "cancel") return onChoose({ kind: "quit" });
    },
    pickerPorts ?? {
      broadcast: () => undefined,
      resolverRef: { current: null },
      snapshotRef: { current: null },
    },
  );

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
          {t("sessionPicker.header")}
        </Text>
        <Text color={FG.meta}>{`  ·  ${workspace}`}</Text>
      </Box>
      <Box height={1} />
      {sessions.length === 0 ? (
        <Box>
          <Text color={FG.faint}>{t("sessionPicker.empty")}</Text>
          <Text bold color={TONE.brand}>
            {"⏎"}
          </Text>
          <Text color={FG.faint}>{t("sessionPicker.emptyNew")}</Text>
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
          <Text color={FG.faint}>{t("cardLabels.more", { count: hiddenBelow })}</Text>
        </Box>
      ) : null}
      {renaming ? (
        <Box marginTop={1}>
          <Text color={FG.faint}>{t("sessionPicker.renamePrompt", { from: renaming.from })}</Text>
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
            ? t("sessionPicker.renameHint")
            : sessions.length === 0
              ? t("sessionPicker.emptyHint")
              : t("sessionPicker.pickerHint")}
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
  const count = info.messageCount;
  const summary =
    info.meta.summary ??
    t(count === 1 ? "sessionPicker.messages" : "sessionPicker.messagesPlural", { count });
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
      <Text color={FG.faint}>{t("sessionPicker.turns", { count: turns })}</Text>
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
  if (mins < 1) return t("sessionPicker.justNow");
  if (mins < 60) return t("sessionPicker.minAgo", { count: mins });
  const hours = Math.floor(mins / 60);
  if (hours < 24) return t("sessionPicker.hoursAgo", { count: hours });
  const days = Math.floor(hours / 24);
  if (days === 1) return t("sessionPicker.yesterday");
  if (days < 7) return t("sessionPicker.daysAgo", { count: days });
  return date.toISOString().slice(0, 10);
}
