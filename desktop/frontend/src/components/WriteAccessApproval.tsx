import type { Translator } from "../lib/i18n";
import type { WireApproval } from "../lib/types";

export type DecisionAction = {
  key: string;
  label: string;
  desc: string;
  tone?: "default" | "danger";
  primary?: boolean;
  // Plan revision and plan guidance open inline editors instead of submitting.
  // Other recovery actions use direct-click submit (no select-then-confirm).
  kind: "submit" | "toggle-revision" | "toggle-guidance" | "direct";
  run?: () => void;
};

export function writeAccessDecisionActions(
  t: Translator,
  onAnswer: (allow: boolean, session: boolean, persist: boolean) => void,
): DecisionAction[] {
  return [
    {
      key: "1",
      label: t("approval.writeAccessOnce"),
      desc: t("approval.writeAccessOnceDesc"),
      kind: "submit",
      run: () => onAnswer(true, false, false),
    },
    {
      key: "2",
      label: t("approval.writeAccessSession"),
      desc: t("approval.writeAccessSessionDesc"),
      kind: "submit",
      run: () => onAnswer(true, true, false),
    },
    {
      key: "3",
      label: t("approval.writeAccessProject"),
      desc: t("approval.writeAccessProjectDesc"),
      kind: "submit",
      run: () => onAnswer(true, true, true),
    },
    {
      key: "4",
      label: t("approval.deny"),
      desc: t("approval.denyDesc"),
      tone: "danger",
      kind: "submit",
      run: () => onAnswer(false, false, false),
    },
  ];
}

export function WriteAccessApprovalDetails({
  approval,
  subject,
  reason,
  reasonOpen,
  t,
}: {
  approval: WireApproval;
  subject: string;
  reason: string;
  reasonOpen: boolean;
  t: Translator;
}) {
  const writeAccess = approval.write_access;
  const directories = writeAccess?.display_directories?.length
    ? writeAccess.display_directories
    : writeAccess?.directories ?? [];
  return (
    <div className="approval-details">
      {subject && <pre className="approval-subject">{subject}</pre>}
      {directories.length > 0 && (
        <div className="approval-reason">
          <strong>{t("approval.writeAccessDirsLabel")}: </strong>
          {directories.join(", ")}
        </div>
      )}
      {writeAccess?.justification && (
        <div className="approval-reason">
          <strong>{t("approval.writeAccessJustificationLabel")}: </strong>
          {writeAccess.justification}
        </div>
      )}
      {writeAccess?.broad_home_access && (
        <div className="approval-reason" role="alert" style={{ color: "var(--danger, #c0392b)", fontWeight: 600 }}>
          {t("approval.writeAccessHomeWarning")}
        </div>
      )}
      {writeAccess?.ordinary_permission_needed && (
        <div className="approval-reason">{t("approval.writeAccessMergedHint")}</div>
      )}
      {reasonOpen && reason && <div className="approval-reason">{reason}</div>}
    </div>
  );
}
