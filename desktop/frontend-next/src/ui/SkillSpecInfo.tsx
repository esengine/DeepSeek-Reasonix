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
      <summary aria-label={t("skillSpecInfo.skillInformationFor", { name: sk.name })}>{t("skillSpecInfo.skillInformation")}</summary>
      <dl>
        {sk.license && <><dt>{t("skillSpecInfo.license")}</dt><dd>{sk.license}</dd></>}
        {sk.compatibility && <><dt>{t("skillSpecInfo.requirements")}</dt><dd>{sk.compatibility}</dd></>}
        {meta.map(([key, value]) => <Fragment key={key}><dt>{key}</dt><dd>{value}</dd></Fragment>)}
      </dl>
    </details>
  );
}
