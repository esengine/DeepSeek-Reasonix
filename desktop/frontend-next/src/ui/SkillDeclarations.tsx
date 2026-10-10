import { t } from "../i18n";
import type { SkillEntry } from "../port/port";

export function SkillDeclarations({ sk }: { sk: SkillEntry }) {
  if (!sk.subagent || (!sk.model && !sk.effort && !sk.allowedTools?.length)) return null;
  return (
    <details className="skprofile">
      <summary aria-label={t("{name} 的子代理声明", { name: sk.name })}>{t("子代理声明")}</summary>
      <p>{t("显示技能自身声明；实际运行配置还会结合当前设置解析。")}</p>
      <dl>
        {sk.model && <><dt>{t("声明的模型")}</dt><dd>{sk.model}</dd></>}
        {sk.effort && <><dt>{t("声明的推理强度")}</dt><dd>{sk.effort}</dd></>}
        {!!sk.allowedTools?.length && <>
          <dt>{t("声明的工具")}</dt>
          <dd>{sk.allowedTools.map((name, index) => <code key={index}>{name}</code>)}</dd>
        </>}
      </dl>
    </details>
  );
}
