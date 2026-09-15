import { useCallback, useEffect, useState } from "react";
import { SettingsOptions } from "./SettingsOptions";
import { PanelBottom, ShieldCheck } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import {
  getSessionExperience,
  applySessionExperience,
  type SessionExperience,
} from "../lib/sessionExperience";
import { hydrateReasoningDisplayMode } from "../lib/reasoningDisplayPreference";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import type { SettingsView } from "../lib/types";
import { SettingsField, SettingsSection } from "./SettingsForm";
import { normalizeToolApprovalMode } from "../lib/types";
import { RiskConfirmation } from "./RiskConfirmation";

const TOOL_APPROVAL_MODES = ["read-only", "workspace-write", "danger-full-access"] as const;
const MODES = ["standard", "deep", "concise"] as const satisfies readonly SessionExperience[];

type Props = {
  snapshot: SettingsView;
  busy: boolean;
  apply: (write: () => Promise<unknown>) => Promise<boolean>;
};

function normalizeSnapshot(value: unknown): SessionExperience {
  if (value === "deep") return "deep";
  if (value === "concise") return "concise";
  return "standard";
}

export function SessionExperienceSettings({ snapshot, busy, apply }: Props) {
  const t = useT();
  const [mode, setMode] = useState<SessionExperience>(getSessionExperience);
  const present = useCallback((next: SessionExperience) => {
    setMode(next);
    applySessionExperience(next);
    hydrateReasoningDisplayMode(next === "deep" ? "expanded" : "auto", next === "deep");
  }, []);
  // Snapshot identity matters: a failed write may reload the same backend value.
  useEffect(() => { present(normalizeSnapshot(snapshot.sessionExperience)); }, [snapshot, present]);
  const save = useCommittedCommand(async (next: SessionExperience) => {
    present(next);
    // The shared Settings apply/reload path owns both success and failure.
    await apply(() => app.SetSessionExperience(next));
  });
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
  const hintKey = mode === "deep"
    ? "settings.sessionExperience.deepHint"
    : mode === "concise"
      ? "settings.sessionExperience.conciseHint"
      : "settings.sessionExperience.standardHint";
  return <>
  <SettingsSection title={t("settings.general.sectionConversation")} description={t("settings.sessionExperienceHint")}>
    <SettingsField label={t("settings.sessionExperience")} hint={t(hintKey)} icon={<PanelBottom size={18} />}>
      <SettingsOptions layout="field" className="set-seg" role="radiogroup" aria-label={t("settings.sessionExperience")}>
        {MODES.map(value => <button key={value} type="button"
          className={`set-seg__btn${mode === value ? " set-seg__btn--on" : ""}`} role="radio"
          aria-checked={mode === value} disabled={busy} onClick={() => void save(value)}>
          {t(`settings.sessionExperience.${value}`)}
        </button>)}
      </SettingsOptions>
    </SettingsField>
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
