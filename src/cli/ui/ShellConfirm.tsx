import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import { t } from "../../i18n/index.js";
import { DenyContextInput } from "./DenyContextInput.js";
import { SingleSelect } from "./Select.js";
import { ApprovalCard } from "./cards/ApprovalCard.js";
import { FG } from "./theme/tokens.js";

export type ShellConfirmChoice = "run_once" | "always_allow" | "deny";

export interface ShellConfirmProps {
  prompt: import("@reasonix/core-utils").ApprovalPrompt;
  onChoose: (choice: ShellConfirmChoice, denyContext?: string) => void;
}

export function ShellConfirm({ prompt, onChoose }: ShellConfirmProps) {
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

  // toApprovalPrompt mints English labels for the ACP surface; the unified
  // prompt.kind collapses foreground + background into "shell", so we recover
  // the split from meta.wait (only set on background prompts).
  const isBackground = prompt.meta?.wait !== undefined;
  const prefix = String(prompt.data?.prefix ?? "");
  const localTitle = isBackground
    ? t("shellConfirm.promptTitleRunBackground")
    : t("shellConfirm.promptTitleRunCommand");
  const localActionLabel = (id: string, fallback: string): string => {
    if (id === "run_once") return t("shellConfirm.actionRunOnce");
    if (id === "always_allow") return t("shellConfirm.actionAlwaysAllow", { prefix });
    if (id === "deny") return t("shellConfirm.actionDeny");
    return fallback;
  };

  return (
    <ApprovalCard
      tone={prompt.tone}
      glyph="?"
      title={localTitle}
      metaRight={t("shellConfirm.awaiting")}
      footerHint={t("shellConfirm.pickFooter")}
    >
      <Box marginBottom={1} flexDirection="column">
        <Box>
          <Text bold color={FG.strong}>
            {"$ "}
          </Text>
          <Text bold color={FG.strong}>
            {prompt.subtitle ?? ""}
          </Text>
        </Box>
      </Box>
      <InfoRows meta={prompt.meta} />
      <SingleSelect
        initialValue={prompt.actions[0]?.id ?? "run_once"}
        items={prompt.actions.map((a) => ({ value: a.id, label: localActionLabel(a.id, a.label) }))}
        onSubmit={(v) => {
          const action = prompt.actions.find((a) => a.id === v);
          if (action?.secondaryInput) {
            setPhase("deny");
          } else {
            onChoose(v as ShellConfirmChoice);
          }
        }}
        onTab={(v) => {
          const action = prompt.actions.find((a) => a.id === v);
          if (action?.secondaryInput) {
            setPhase("deny");
          }
        }}
        onCancel={() => onChoose("deny")}
      />
    </ApprovalCard>
  );
}

function InfoRows({ meta }: { meta?: Record<string, string> }): React.ReactElement | null {
  if (!meta || Object.keys(meta).length === 0) return null;
  const labelMap: Record<string, string> = {
    cwd: t("shellConfirm.cwdLabel"),
    timeout: t("shellConfirm.timeoutLabel"),
    wait: t("shellConfirm.waitLabel"),
  };
  const rows = Object.entries(meta).map(([key, value]) => ({
    label: labelMap[key] ?? key,
    value,
  }));
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
