import { useCallback, useEffect, useState } from "react";
import { SettingsOptions } from "./SettingsOptions";
import { ShieldCheck } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { SettingsView } from "../lib/types";
import { SettingsField, SettingsSection } from "./SettingsForm";
import { normalizeToolApprovalMode } from "../lib/types";
import { RiskConfirmation } from "./RiskConfirmation";

const TOOL_APPROVAL_MODES = ["read-only", "workspace-write", "danger-full-access"] as const;

type Props = {
  snapshot: SettingsView;
  busy: boolean;
  apply: (write: () => Promise<unknown>) => Promise<boolean>;
};

export function SessionExperienceSettings({ snapshot, busy, apply }: Props) {
  const t = useT();
  const defaultToolApprovalMode = normalizeToolApprovalMode(snapshot.defaultToolApprovalMode);
  const [confirmingFullAccess, setConfirmingFullAccess] = useState(false);
  const [acknowledged, setAcknowledged] = useState(false);
  const closeConfirmation = useCallback(() => {
    setAcknowledged(false);
    setConfirmingFullAccess(false);
  }, []);
  useEffect(() => {
    if (busy) closeConfirmation();
  }, [busy, closeConfirmation]);
  const saveApproval = (next: (typeof TOOL_APPROVAL_MODES)[number]) => {
    if (next === defaultToolApprovalMode) return;
    if (next === "danger-full-access") {
      setAcknowledged(false);
      setConfirmingFullAccess(true);
      return;
    }
    void apply(() => app.SetDefaultToolApprovalMode(next));
  };
  const confirmFullAccess = () => {
    if (busy || !acknowledged) return;
    closeConfirmation();
    void apply(() => app.SetDefaultToolApprovalMode("danger-full-access"));
  };
  return <>
  <SettingsSection title={t("settings.general.sectionConversation")}>
    <SettingsField label={t("settings.defaultToolApprovalMode")} hint={t("settings.defaultToolApprovalModeHint")} icon={<ShieldCheck size={18} />}>
      <SettingsOptions layout="field" className="set-seg" role="radiogroup" aria-label={t("settings.defaultToolApprovalMode")}>
        {TOOL_APPROVAL_MODES.map((value) => <button key={value} type="button" className={`set-seg__btn${defaultToolApprovalMode === value ? " set-seg__btn--on" : ""}`} role="radio" aria-checked={defaultToolApprovalMode === value} disabled={busy || confirmingFullAccess} onClick={() => saveApproval(value)}>{t(`settings.defaultToolApprovalMode.${value}`)}</button>)}
      </SettingsOptions>
    </SettingsField>
  </SettingsSection>
  <RiskConfirmation
    open={confirmingFullAccess}
    title={t("permission.fullAccessConfirm.title")}
    description={t("permission.fullAccessConfirm.futureDescription")}
    acknowledgeLabel={t("permission.fullAccessConfirm.futureAcknowledge")}
    cancelLabel={t("common.cancel")}
    closeLabel={t("common.close")}
    confirmLabel={t("permission.fullAccessConfirm.enable")}
    acknowledged={acknowledged}
    disabled={busy}
    onAcknowledgedChange={setAcknowledged}
    onCancel={closeConfirmation}
    onConfirm={confirmFullAccess}
  />
  </>;
}
