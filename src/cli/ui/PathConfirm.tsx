import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import { t } from "../../i18n/index.js";
import { DenyContextInput } from "./DenyContextInput.js";
import { SingleSelect } from "./Select.js";
import { ApprovalCard } from "./cards/ApprovalCard.js";
import { FG } from "./theme/tokens.js";

export type PathConfirmChoice = "run_once" | "always_allow" | "deny";

export interface PathConfirmProps {
  prompt: import("@reasonix/core-utils").ApprovalPrompt;
  onChoose: (choice: PathConfirmChoice, denyContext?: string) => void;
}

export function PathConfirm({ prompt, onChoose }: PathConfirmProps) {
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

  const path = prompt.subtitle ?? "";
  const allowPrefix = String(prompt.data?.prefix ?? "");
  // prompt.kind collapses read/write into "path"; recover from data.intent.
  const intent = prompt.data?.intent === "write" ? "write" : "read";
  const localTitle =
    intent === "write" ? t("pathConfirm.promptTitleWrite") : t("pathConfirm.promptTitleRead");
  const localActionLabel = (id: string, fallback: string): string => {
    if (id === "run_once") {
      return intent === "write"
        ? t("pathConfirm.actionAllowWrite")
        : t("pathConfirm.actionAllowRead");
    }
    if (id === "always_allow") return t("pathConfirm.actionAlwaysAllow", { prefix: allowPrefix });
    if (id === "deny") return t("pathConfirm.actionDeny");
    return fallback;
  };

  return (
    <ApprovalCard
      tone={prompt.tone}
      glyph="!"
      title={localTitle}
      metaRight={t("pathConfirm.awaiting")}
      footerHint={t("pathConfirm.pickFooter")}
    >
      <Box marginBottom={1}>
        <Text color={FG.faint}>{prompt.preview ?? ""}</Text>
      </Box>
      <InfoRows path={path} sandboxRoot={prompt.meta?.sandboxRoot} allowPrefix={allowPrefix} />
      <SingleSelect
        initialValue={prompt.actions[0]?.id ?? "run_once"}
        items={prompt.actions.map((a) => ({ value: a.id, label: localActionLabel(a.id, a.label) }))}
        onSubmit={(v) => {
          const action = prompt.actions.find((a) => a.id === v);
          if (action?.secondaryInput) {
            setPhase("deny");
          } else {
            onChoose(v as PathConfirmChoice);
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

function InfoRows({
  path,
  sandboxRoot,
  allowPrefix,
}: {
  path: string;
  sandboxRoot?: string;
  allowPrefix: string;
}): React.ReactElement {
  const rows: Array<{ label: string; value: string }> = [
    { label: t("pathConfirm.pathLabel"), value: path },
  ];
  if (sandboxRoot) {
    rows.push({ label: t("pathConfirm.sandboxLabel"), value: sandboxRoot });
  }
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
