import { homedir } from "node:os";
import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import { t } from "../../i18n/index.js";
import { DenyContextInput } from "./DenyContextInput.js";
import { SingleSelect } from "./Select.js";
import { ApprovalCard } from "./cards/ApprovalCard.js";
import { useReserveRows } from "./layout/viewport-budget.js";
import { FG } from "./theme/tokens.js";

export type PathConfirmChoice = "run_once" | "always_allow" | "deny";

export interface PathConfirmProps {
  path: string;
  intent: "read" | "write";
  toolName: string;
  sandboxRoot: string;
  /** Directory prefix that would be persisted if the user picks "always allow". */
  allowPrefix: string;
  onChoose: (choice: PathConfirmChoice, denyContext?: string) => void;
}

function tildeify(p: string): string {
  const home = homedir();
  if (!home) return p;
  const normalized = home.replace(/[\\/]+$/, "");
  if (p === normalized) return "~";
  if (p.startsWith(`${normalized}/`)) return `~/${p.slice(normalized.length + 1)}`;
  if (p.startsWith(`${normalized}\\`)) return `~\\${p.slice(normalized.length + 1)}`;
  return p;
}

export function PathConfirm({
  path,
  intent,
  toolName,
  sandboxRoot,
  allowPrefix,
  onChoose,
}: PathConfirmProps) {
  useReserveRows("modal", { min: 8, max: 14 });

  const [phase, setPhase] = useState<"pick" | "deny">("pick");

  if (phase === "deny") {
    return (
      <ApprovalCard
        tone="error"
        glyph="✗"
        title={t("pathConfirm.denyTitle")}
        metaRight={t("pathConfirm.optional")}
        footerHint={t("pathConfirm.denyFooter")}
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
      glyph="!"
      title={t("pathConfirm.title")}
      metaRight={t("pathConfirm.awaiting")}
      footerHint={t("pathConfirm.pickFooter")}
    >
      <Box marginBottom={1}>
        <Text color={FG.faint}>
          {t(intent === "write" ? "pathConfirm.subtitleWrite" : "pathConfirm.subtitleRead", {
            tool: toolName,
          })}
        </Text>
      </Box>
      <InfoRows
        path={tildeify(path)}
        sandboxRoot={tildeify(sandboxRoot)}
        allowPrefix={tildeify(allowPrefix)}
      />
      <SingleSelect
        initialValue="run_once"
        items={[
          {
            value: "run_once",
            label: t("pathConfirm.allowOnce"),
            hint: t("pathConfirm.allowOnceDesc"),
          },
          {
            value: "always_allow",
            label: t("pathConfirm.allowAlways"),
            hint: t("pathConfirm.allowAlwaysDesc", { prefix: tildeify(allowPrefix) }),
          },
          {
            value: "deny",
            label: t("pathConfirm.deny"),
            hint: t("pathConfirm.denyDesc"),
          },
        ]}
        onSubmit={(v) => {
          if (v === "deny") setPhase("deny");
          else onChoose(v as PathConfirmChoice);
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
  path,
  sandboxRoot,
  allowPrefix,
}: {
  path: string;
  sandboxRoot: string;
  allowPrefix: string;
}): React.ReactElement {
  const rows: Array<{ label: string; value: string }> = [
    { label: t("pathConfirm.pathLabel"), value: path },
    { label: t("pathConfirm.sandboxLabel"), value: sandboxRoot },
  ];
  if (allowPrefix !== path) {
    rows.push({ label: t("pathConfirm.allowPrefixLabel"), value: allowPrefix });
  }
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
