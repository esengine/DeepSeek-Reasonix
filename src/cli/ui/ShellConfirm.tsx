import { homedir } from "node:os";
import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import { t } from "../../i18n/index.js";
import { DenyContextInput } from "./DenyContextInput.js";
import { SingleSelect } from "./Select.js";
import { ApprovalCard } from "./cards/ApprovalCard.js";
import { useReserveRows, useTotalRows } from "./layout/viewport-budget.js";
import { FG, TONE } from "./theme/tokens.js";

/** Header + subtitle + info rows + 3-option select + separator + footer — empirically 18 rows. */
const CHROME_ROWS = 18;
/** Floor so the user can always see *something* of the command on tiny terminals. */
const MIN_COMMAND_LINES = 3;

export type ShellConfirmChoice = "run_once" | "always_allow" | "deny";

export interface ShellConfirmProps {
  command: string;
  /** Prefix that would be persisted if the user picks "always allow". */
  allowPrefix: string;
  /** `run_background` returns early; `run_command` blocks the TUI. */
  kind?: "run_command" | "run_background";
  /** Working directory the command will run in — surfaced as an info row. */
  cwd?: string;
  /** run_command timeout in seconds — surfaced as "timeout 120s". */
  timeoutSec?: number;
  /** run_background startup wait in seconds — surfaced as "wait 3s". */
  waitSec?: number;
  onChoose: (choice: ShellConfirmChoice, denyContext?: string) => void;
}

/** Keep the first `max` lines so the SingleSelect + footer stay on screen; `hidden` counts the dropped tail. */
export function clampCommand(command: string, max: number): { preview: string; hidden: number } {
  const lines = command.split("\n");
  if (lines.length <= max) return { preview: command, hidden: 0 };
  return { preview: lines.slice(0, max).join("\n"), hidden: lines.length - max };
}

/** ~/foo when path is under $HOME so the row doesn't blow past viewport width. */
function tildeify(path: string): string {
  const home = homedir();
  if (!home) return path;
  const normalized = home.replace(/[\\/]+$/, "");
  if (path === normalized) return "~";
  if (path.startsWith(`${normalized}/`)) return `~/${path.slice(normalized.length + 1)}`;
  if (path.startsWith(`${normalized}\\`)) return `~\\${path.slice(normalized.length + 1)}`;
  return path;
}

export function ShellConfirm({
  command,
  allowPrefix,
  kind,
  cwd,
  timeoutSec,
  waitSec,
  onChoose,
}: ShellConfirmProps) {
  useReserveRows("modal", { min: 8, max: 14 });
  const totalRows = useTotalRows();
  const maxCommandLines = Math.max(MIN_COMMAND_LINES, totalRows - CHROME_ROWS);
  const { preview, hidden } = clampCommand(command, maxCommandLines);

  const isBackground = kind === "run_background";
  const subtitle = isBackground ? t("shellConfirm.bgSubtitle") : t("shellConfirm.subtitle");

  const [phase, setPhase] = useState<"pick" | "deny">("pick");

  if (phase === "deny") {
    return (
      <ApprovalCard
        tone="error"
        glyph="✗"
        title={t("shellConfirm.denyTitle")}
        metaRight={t("shellConfirm.optional")}
        footerHint={t("shellConfirm.denyFooter")}
      >
        <DenyContextInput
          onSubmit={(context) => onChoose("deny", context || undefined)}
          onCancel={() => onChoose("deny")}
        />
      </ApprovalCard>
    );
  }

  return (
    <ApprovalCard
      tone="warn"
      glyph={isBackground ? "⏱" : "?"}
      title={isBackground ? t("shellConfirm.bgTitle") : t("shellConfirm.title")}
      metaRight={t("shellConfirm.awaiting")}
      footerHint={t("shellConfirm.pickFooter")}
    >
      <Box marginBottom={1}>
        <Text color={FG.faint}>{subtitle}</Text>
      </Box>
      <Box marginBottom={1} flexDirection="column">
        <Box>
          <Text bold color={TONE.err}>
            {"$ "}
          </Text>
          <Text bold color={FG.strong}>
            {preview}
          </Text>
        </Box>
        {hidden > 0 ? (
          <Text color={FG.faint}>
            {t(hidden === 1 ? "shellConfirm.previewMore" : "shellConfirm.previewMorePlural", {
              n: hidden,
            })}
          </Text>
        ) : null}
      </Box>
      <InfoRows cwd={cwd} timeoutSec={timeoutSec} waitSec={waitSec} kind={kind} />
      <SingleSelect
        initialValue="run_once"
        items={[
          {
            value: "run_once",
            label: t("shellConfirm.allowOnce"),
            hint: t("shellConfirm.allowOnceDesc"),
          },
          {
            value: "always_allow",
            label: t("shellConfirm.allowAlways"),
            hint: t("shellConfirm.allowAlwaysDesc", { prefix: allowPrefix }),
          },
          {
            value: "deny",
            label: t("shellConfirm.deny"),
            hint: t("shellConfirm.denyDesc"),
          },
        ]}
        onSubmit={(v) => {
          if (v === "deny") setPhase("deny");
          else onChoose(v as ShellConfirmChoice);
        }}
        onTab={(v) => {
          if (v === "deny") setPhase("deny");
        }}
        onCancel={() => onChoose("deny")}
      />
    </ApprovalCard>
  );
}

function InfoRows({
  cwd,
  timeoutSec,
  waitSec,
  kind,
}: {
  cwd?: string;
  timeoutSec?: number;
  waitSec?: number;
  kind?: "run_command" | "run_background";
}): React.ReactElement | null {
  const rows: Array<{ label: string; value: string }> = [];
  if (cwd) rows.push({ label: t("shellConfirm.cwdLabel"), value: tildeify(cwd) });
  if (kind === "run_background" && waitSec !== undefined && waitSec > 0) {
    rows.push({ label: t("shellConfirm.waitLabel"), value: `${waitSec}s` });
  } else if (kind !== "run_background" && timeoutSec !== undefined) {
    rows.push({ label: t("shellConfirm.timeoutLabel"), value: `${timeoutSec}s` });
  }
  if (rows.length === 0) return null;
  const labelWidth = Math.max(...rows.map((r) => r.label.length));
  return (
    <Box flexDirection="column" marginBottom={1}>
      {rows.map((r) => (
        <Box key={r.label} flexDirection="row" gap={1}>
          <Text color={FG.faint}>{r.label.padEnd(labelWidth)}</Text>
          <Text color={FG.body}>{r.value}</Text>
        </Box>
      ))}
    </Box>
  );
}

/** First two tokens for known wrappers (`npm install`, `git commit`, …); else first token only. */
export function derivePrefix(command: string): string {
  const tokens = command.trim().split(/\s+/).filter(Boolean);
  if (tokens.length === 0) return "";
  if (tokens.length === 1) return tokens[0]!;
  const first = tokens[0]!;
  const TWO_TOKEN_WRAPPERS = new Set([
    "npm",
    "npx",
    "pnpm",
    "yarn",
    "bun",
    "git",
    "cargo",
    "go",
    "docker",
    "kubectl",
    "python",
    "python3",
    "deno",
    "pip",
    "pip3",
    "make",
    "rake",
    "bundle",
    "gem",
  ]);
  return TWO_TOKEN_WRAPPERS.has(first) ? `${first} ${tokens[1]}` : first;
}
