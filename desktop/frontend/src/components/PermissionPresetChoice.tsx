import { useCallback, useEffect, useState } from "react";
import { Eye, ShieldAlert, ShieldCheck } from "lucide-react";
import { useT } from "../lib/i18n";
import { normalizeToolApprovalMode, type ToolApprovalMode } from "../lib/types";
import { hasConfirmedFullAccessForProject, rememberFullAccessConfirmationForProject } from "../lib/fullAccessConfirmation";
import { ComposerChoice } from "./ComposerChoice";
import { RiskConfirmation } from "./RiskConfirmation";

export function PermissionPresetChoice({
  value,
  disabled,
  scopeKey,
  projectConfirmationKey,
  onPick,
}: {
  value: ToolApprovalMode;
  disabled: boolean;
  scopeKey: string;
  projectConfirmationKey: string;
  onPick: (value: ToolApprovalMode) => void;
}) {
  const t = useT();
  const preset = normalizeToolApprovalMode(value);
  const [confirmingFullAccess, setConfirmingFullAccess] = useState(false);
  const [acknowledged, setAcknowledged] = useState(false);

  const closeConfirmation = useCallback(() => {
    setAcknowledged(false);
    setConfirmingFullAccess(false);
  }, []);

  useEffect(() => {
    closeConfirmation();
  }, [closeConfirmation, disabled, projectConfirmationKey, scopeKey]);

  const choose = (next: ToolApprovalMode) => {
    const normalized = normalizeToolApprovalMode(next);
    if (normalized === preset) return;
    if (normalized === "danger-full-access") {
      if (hasConfirmedFullAccessForProject(projectConfirmationKey)) {
        onPick(normalized);
        return;
      }
      setAcknowledged(false);
      setConfirmingFullAccess(true);
      return;
    }
    onPick(normalized);
  };

  const confirm = () => {
    if (disabled || !acknowledged) return;
    rememberFullAccessConfirmationForProject(projectConfirmationKey);
    closeConfirmation();
    onPick("danger-full-access");
  };

  return <>
    <ComposerChoice
      label={t(preset === "read-only" ? "composer.permissionReadOnly" : preset === "workspace-write" ? "composer.permissionWorkspaceWrite" : "composer.permissionFullAccess")}
      showChevron
      icon={preset === "danger-full-access" ? <ShieldAlert size={16} /> : preset === "workspace-write" ? <ShieldCheck size={16} /> : <Eye size={16} />}
      tone={`composer-choice--permission-${preset}`}
      value={preset}
      disabled={disabled || confirmingFullAccess}
      onPick={(next) => choose(next as ToolApprovalMode)}
      options={[
        { value: "read-only", label: t("composer.permissionReadOnly"), icon: <Eye size={18} />, description: t("composer.permissionReadOnlyDesc") },
        { value: "workspace-write", label: t("composer.permissionWorkspaceWrite"), badge: t("composer.permissionRecommended"), icon: <ShieldCheck size={18} />, description: t("composer.permissionWorkspaceWriteDesc") },
        { value: "danger-full-access", label: t("composer.permissionFullAccess"), icon: <ShieldAlert size={18} />, description: t("composer.permissionFullAccessDesc") },
      ]}
    />
    <RiskConfirmation
      open={confirmingFullAccess}
      title={t("permission.fullAccessConfirm.title")}
      description={t("permission.fullAccessConfirm.currentDescription")}
      acknowledgeLabel={t("permission.fullAccessConfirm.currentAcknowledge")}
      cancelLabel={t("common.cancel")}
      closeLabel={t("common.close")}
      confirmLabel={t("permission.fullAccessConfirm.enable")}
      acknowledged={acknowledged}
      disabled={disabled}
      onAcknowledgedChange={setAcknowledged}
      onCancel={closeConfirmation}
      onConfirm={confirm}
    />
  </>;
}
