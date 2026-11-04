import { t } from "../i18n";
import type { ProviderRemoval } from "../port/port";

const ROLE_LABEL: Record<string, () => string> = {
  default: () => t("common.default_model"),
  planner: () => t("common.plan"),
  subagent: () => t("common.subagents"),
  vision: () => t("models.vision"),
  guardian: () => t("models.review"),
  recovery: () => t("models.recovery_review"),
  triage: () => t("models.small_classifications"),
  decision: () => t("models.decision"),
  advisor: () => t("models.advisor"),
};

export function roleLabel(id: string): string {
  const [role, skill] = id.split(":", 2);
  const label = ROLE_LABEL[role]?.() ?? role;
  return skill ? `${label} · ${skill}` : label;
}

export const removalChangedRoles = (r: ProviderRemoval) => r.moved.length > 0 || r.cleared.length > 0;

export interface Removal {
  key: string;
  name: string;
  why: string;
  report: ProviderRemoval | null;
}

// Where a removal's outcome shows once the service it concerns has no detail
// to carry it: the roles it handed on or switched off, or a failure that came
// after the removal was already written.
export function RemovalNotice({ removal }: { removal: Removal }) {
  const { name, why, report } = removal;
  const moved = report?.moved.map(roleLabel).join("、") ?? "";
  const cleared = report?.cleared.map(roleLabel).join("、") ?? "";
  return (
    <div className="find" data-lvl="warn" role={why ? "alert" : "status"}>
      <span className="t">{why ? t("models.removing_name_did_not_fully", { name }) : t("models.removed_name", { name })}</span>
      {why && <span className="why">{why}</span>}
      {moved && <span className="why">{t("models.now_using_to_for_roles", { to: report?.movedTo ?? "", roles: moved })}</span>}
      {cleared && <span className="why">{t("models.cleared_so_these_are_off", { roles: cleared })}</span>}
    </div>
  );
}
