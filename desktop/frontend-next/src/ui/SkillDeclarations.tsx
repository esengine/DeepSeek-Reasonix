import { t } from "../i18n";
import type { SkillEntry } from "../port/port";

export function SkillDeclarations({ sk }: { sk: SkillEntry }) {
  if (!sk.subagent || (!sk.model && !sk.effort && !sk.allowedTools?.length)) return null;
  return (
    <details className="skprofile">
      <summary aria-label={t("ext.subagent_declarations_for_name", { name: sk.name })}>{t("ext.subagent_declarations")}</summary>
      <p>{t("ext.shows_the_skills_declarations_runtime")}</p>
      <dl>
        {sk.model && <><dt>{t("ext.declared_model")}</dt><dd>{sk.model}</dd></>}
        {sk.effort && <><dt>{t("ext.declared_reasoning_effort")}</dt><dd>{sk.effort}</dd></>}
        {!!sk.allowedTools?.length && <>
          <dt>{t("ext.declared_tools")}</dt>
          <dd>{sk.allowedTools.map((name, index) => <code key={index}>{name}</code>)}</dd>
        </>}
      </dl>
    </details>
  );
}
