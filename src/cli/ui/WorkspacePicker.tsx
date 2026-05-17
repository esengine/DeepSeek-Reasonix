import { basename } from "node:path";
import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useEffect, useMemo, useState } from "react";
import { t } from "../../i18n/index.js";
import type { WorkspaceInfo } from "../../workspaces.js";
import { useKeystroke } from "./keystroke-context.js";
import { FG, TONE } from "./theme/tokens.js";

export type WorkspacePickerOutcome = { kind: "open"; path: string } | { kind: "quit" };

export interface WorkspacePickerProps {
  workspaces: ReadonlyArray<WorkspaceInfo>;
  currentWorkspace: string;
  onChoose: (outcome: WorkspacePickerOutcome) => void;
}

const PAGE_MARGIN = 6;

export function WorkspacePicker({
  workspaces,
  currentWorkspace,
  onChoose,
}: WorkspacePickerProps): React.ReactElement {
  const [focus, setFocus] = useState(0);
  const [searching, setSearching] = useState(false);
  const [query, setQuery] = useState("");
  const hasSearch = searching || query.length > 0;
  const activeWorkspaces = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return workspaces;
    return workspaces.filter((w) => w.path.toLowerCase().includes(needle));
  }, [workspaces, query]);
  const maxFocus = Math.max(0, activeWorkspaces.length - 1);

  useEffect(() => {
    setFocus((f) => Math.max(0, Math.min(f, maxFocus)));
  }, [maxFocus]);

  const { stdout } = useStdout();
  const rows = stdout?.rows ?? 40;
  const visibleCount = Math.max(3, rows - PAGE_MARGIN);

  useKeystroke((ev) => {
    if (ev.paste) {
      if (searching) setQuery((q) => `${q}${oneLine(ev.input)}`);
      return;
    }
    if (searching) {
      if (ev.escape) {
        setSearching(false);
        setQuery("");
        setFocus(0);
        return;
      }
      if (ev.upArrow) return setFocus((f) => Math.max(0, f - 1));
      if (ev.downArrow) return setFocus((f) => Math.min(maxFocus, f + 1));
      if (ev.return) {
        const target = activeWorkspaces[focus];
        if (target) onChoose({ kind: "open", path: target.path });
        return;
      }
      if (ev.backspace) {
        if (query.length === 0) {
          setSearching(false);
          return;
        }
        setQuery((q) => q.slice(0, -1));
        return;
      }
      if (ev.input && !ev.ctrl && !ev.meta && !ev.tab) setQuery((q) => `${q}${ev.input}`);
      return;
    }
    if (ev.escape) return onChoose({ kind: "quit" });
    if (ev.upArrow) return setFocus((f) => Math.max(0, f - 1));
    if (ev.downArrow) return setFocus((f) => Math.min(maxFocus, f + 1));
    if (ev.return) {
      const target = activeWorkspaces[focus];
      if (target) return onChoose({ kind: "open", path: target.path });
      return;
    }
    if (!ev.input) return;
    if (ev.input === "/") {
      setSearching(true);
      setQuery("");
      setFocus(0);
      return;
    }
    if (ev.input === "q") return onChoose({ kind: "quit" });
  });

  const start = Math.max(
    0,
    Math.min(focus - Math.floor(visibleCount / 2), activeWorkspaces.length - visibleCount),
  );
  const end = Math.min(activeWorkspaces.length, start + visibleCount);
  const shown = activeWorkspaces.slice(start, end);
  const hiddenBelow = activeWorkspaces.length - end;

  return (
    <Box flexDirection="column" marginY={1}>
      <Box>
        <Text bold color={TONE.brand}>
          {t("workspacePicker.header")}
        </Text>
        <Text color={FG.meta}>{`  ·  ${currentWorkspace}`}</Text>
        {hasSearch ? (
          <Text
            color={FG.meta}
          >{`  ·  /${query} (${activeWorkspaces.length}/${workspaces.length})`}</Text>
        ) : null}
      </Box>
      <Box height={1} />
      {workspaces.length === 0 ? (
        <Box>
          <Text color={FG.faint}>{t("workspacePicker.empty")}</Text>
        </Box>
      ) : activeWorkspaces.length === 0 ? (
        <Box>
          <Text color={FG.faint}>{t("workspacePicker.searchEmpty")}</Text>
        </Box>
      ) : (
        shown.map((w, i) => <WorkspaceRow key={w.path} info={w} focused={start + i === focus} />)
      )}
      {hiddenBelow > 0 ? (
        <Box>
          <Text color={FG.faint}>{t("cardLabels.more", { count: hiddenBelow })}</Text>
        </Box>
      ) : null}
      {searching ? (
        <Box marginTop={1}>
          <Text color={FG.faint}>{t("workspacePicker.searchPrompt")}</Text>
          <Text bold color={TONE.brand}>
            {query}
          </Text>
          <Text backgroundColor={TONE.brand} color="black">
            {" "}
          </Text>
        </Box>
      ) : null}
      <Box marginTop={1}>
        <Text color={FG.faint}>
          {searching ? t("workspacePicker.searchHint") : t("workspacePicker.pickerHint")}
        </Text>
      </Box>
    </Box>
  );
}

function WorkspaceRow({
  info,
  focused,
}: {
  info: WorkspaceInfo;
  focused: boolean;
}): React.ReactElement {
  const label = basename(info.path) || info.path;
  const meta = t(
    info.sessions === 1 ? "workspacePicker.sessions" : "workspacePicker.sessionsPlural",
    {
      count: info.sessions,
    },
  );
  return (
    <Box>
      <Text color={focused ? TONE.brand : FG.faint}>{focused ? "  ▸ " : "    "}</Text>
      <Text bold={focused} color={focused ? FG.strong : FG.sub}>
        {label.padEnd(16)}
      </Text>
      <Text color={FG.meta}>{info.current ? ` · ${t("workspacePicker.current")} · ` : " · "}</Text>
      <Text color={focused ? FG.body : FG.sub}>{info.path}</Text>
      <Box flexGrow={1} />
      <Text color={FG.faint}>{meta}</Text>
    </Box>
  );
}

function oneLine(s: string): string {
  return s.replace(/\s+/g, " ").trim();
}
