import { t } from "../i18n";
import type { ProviderRemoval } from "../port/port";

const ROLE_LABEL: Record<string, () => string> = {
  default: () => t("common.defaultModel"),
  planner: () => t("common.plan"),
  subagent: () => t("common.subagents2"),
  vision: () => t("common.vision"),
  guardian: () => t("common.review"),
  recovery: () => t("providerRemoval.recoveryReview"),
  triage: () => t("providerRemoval.smallClassifications"),
  decision: () => t("common.decision"),
  advisor: () => t("providerRemoval.advisor"),
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
      <span className="t">{why ? t("providerRemoval.removingDidNotFullyComplete", { name }) : t("providerRemoval.removed", { name })}</span>
      {why && <span className="why">{why}</span>}
      {moved && <span className="why">{t("providerRemoval.nowUsingFor", { to: report?.movedTo ?? "", roles: moved })}</span>}
      {cleared && <span className="why">{t("providerRemoval.clearedSoTheseAreOff", { roles: cleared })}</span>}
    </div>
  );
}
