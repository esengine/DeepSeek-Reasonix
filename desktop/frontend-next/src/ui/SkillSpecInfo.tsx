import { Fragment } from "react";
import { t } from "../i18n";
import type { SkillEntry } from "../port/port";

// What the skill says about itself in the Agent Skills format: who may use it
// and what it expects. Shown as written; nothing here changes how it runs.
export function SkillSpecInfo({ sk }: { sk: SkillEntry }) {
  const meta = Object.entries(sk.metadata ?? {});
  if (!sk.license && !sk.compatibility && meta.length === 0) return null;
  return (
    <details className="skprofile">
      <summary aria-label={t("ext.skill_information_for_name", { name: sk.name })}>{t("ext.skill_information")}</summary>
      <dl>
        {sk.license && <><dt>{t("ext.license")}</dt><dd>{sk.license}</dd></>}
        {sk.compatibility && <><dt>{t("ext.requirements")}</dt><dd>{sk.compatibility}</dd></>}
        {meta.map(([key, value]) => <Fragment key={key}><dt>{key}</dt><dd>{value}</dd></Fragment>)}
      </dl>
    </details>
  );
}
