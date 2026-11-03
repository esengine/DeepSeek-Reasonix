import { t } from "../i18n";
import type { SkillEntry } from "../port/port";

export function SkillDeclarations({ sk }: { sk: SkillEntry }) {
  if (!sk.subagent || (!sk.model && !sk.effort && !sk.allowedTools?.length)) return null;
  return (
    <details className="skprofile">
      <summary aria-label={t("skillDeclarations.subagentDeclarationsFor", { name: sk.name })}>{t("skillDeclarations.subagentDeclarations")}</summary>
      <p>{t("skillDeclarations.showsTheSkillSDeclarations")}</p>
      <dl>
        {sk.model && <><dt>{t("skillDeclarations.declaredModel")}</dt><dd>{sk.model}</dd></>}
        {sk.effort && <><dt>{t("skillDeclarations.declaredReasoningEffort")}</dt><dd>{sk.effort}</dd></>}
        {!!sk.allowedTools?.length && <>
          <dt>{t("skillDeclarations.declaredTools")}</dt>
          <dd>{sk.allowedTools.map((name, index) => <code key={index}>{name}</code>)}</dd>
        </>}
      </dl>
    </details>
  );
}
