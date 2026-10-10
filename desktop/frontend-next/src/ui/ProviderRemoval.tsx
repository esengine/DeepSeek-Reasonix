import { t } from "../i18n";
import type { ProviderRemoval } from "../port/port";

const ROLE_LABEL: Record<string, () => string> = {
  default: () => t("默认模型"),
  planner: () => t("计划"),
  subagent: () => t("子代理"),
  vision: () => t("看图"),
  guardian: () => t("复核"),
  recovery: () => t("恢复审查"),
  triage: () => t("小型分类"),
  decision: () => t("决策"),
  advisor: () => t("顾问"),
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
      <span className="t">{why ? t("{name} 的删除没有完整完成", { name }) : t("已删除 {name}", { name })}</span>
      {why && <span className="why">{why}</span>}
      {moved && <span className="why">{t("已改用 {to}：{roles}", { to: report?.movedTo ?? "", roles: moved })}</span>}
      {cleared && <span className="why">{t("已清空，对应功能已关闭：{roles}", { roles: cleared })}</span>}
    </div>
  );
}
