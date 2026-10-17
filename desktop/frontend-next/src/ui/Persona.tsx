import { useEffect, useId, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { PersonaDraft, PersonaPreset, PersonaSettings } from "../port/boundary";
import { Group } from "./Group";
import { Switch } from "./Switch";

// The kernel refuses a prefix over 32 KiB and a total over 64 KiB; the box
// bounds the field it can see instead of letting a huge paste grow silently.
const LIMIT = 32768;

const BLANK: PersonaDraft = { prefix: "", suffix: "", complete: false, include_runtime_context: true };

const draftOf = (s: PersonaSettings): PersonaDraft => ({
  prefix: s.prefix,
  suffix: s.suffix,
  complete: s.complete,
  include_runtime_context: s.include_runtime_context,
});

const sameDraft = (a: PersonaDraft, b: PersonaDraft) =>
  a.prefix === b.prefix && a.suffix === b.suffix && a.complete === b.complete &&
  a.include_runtime_context === b.include_runtime_context;

const isPreset = (p: PersonaPreset, d: PersonaDraft) =>
  sameDraft({ prefix: p.prefix, suffix: p.suffix, complete: p.complete, include_runtime_context: p.include_runtime_context }, d);

// Presets are served with their prompt text and a stable id; the words the
// page shows about them are this interface's, so they go through t() here.
function presetName(id: string): string {
  switch (id) {
    case "engineer": return t("严谨工程师");
    case "concise": return t("简洁");
    case "mentor": return t("教学导师");
    case "critic": return t("严苛评审");
    default: return id;
  }
}

function presetAbout(id: string): string {
  switch (id) {
    case "engineer": return t("先验证再下结论，只做最少的正确改动，并说明剩余风险。");
    case "concise": return t("回答简短直接，不铺垫，优先代码与要点。");
    case "mentor": return t("像老师一样说明取舍与原理，把决定权留给你。");
    case "critic": return t("以审查者的立场找问题：先指出失效方式和证据，再谈通过。");
    default: return "";
  }
}

// A persona leads the system prompt as one identity and trails the first-party
// guidance as one posture. It is composed once at boot, so this page writes a
// file and says plainly that only the sessions opened afterwards read it.
export function Persona({ port }: { port: AgentPort }) {
  const [state, setState] = useState<PersonaSettings | null>(null);
  const [draft, setDraft] = useState<PersonaDraft>(BLANK);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const prefixId = useId();
  const suffixId = useId();

  const adopt = (s: PersonaSettings) => {
    setState(s);
    setDraft(draftOf(s));
    setSaved(false);
  };

  useEffect(() => {
    port.persona().then(adopt).catch(() => setState(null));
  }, [port]);

  const change = (patch: Partial<PersonaDraft>) => {
    setDraft((d) => ({ ...d, ...patch }));
    setSaved(false);
    setError("");
  };

  const save = async () => {
    if (!state) return;
    // The switch cannot be turned on without a prefix, but clearing the box
    // after it was on is one keystroke away — the refusal has to live here.
    if (draft.complete && !draft.prefix.trim()) {
      setError(t("启用「用前缀替换整个系统提示词」时，前缀不能为空。"));
      return;
    }
    setBusy(true);
    setError("");
    try {
      adopt(await port.savePersonaSettings(draft));
      setSaved(true);
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const preset = state?.presets.find((p) => isPreset(p, draft)) ?? null;
  const blank = !draft.prefix && !draft.suffix;
  // A draft the presets do not describe is the user's own; saying so is the
  // answer to "which of these am I looking at".
  const now = !state ? undefined : preset ? presetName(preset.id) : blank ? undefined : t("自定义");
  const changed = !!state && !sameDraft(draft, draftOf(state));

  return (
    <Group
      id="persona"
      title={t("系统人设")}
      now={now}
      hint={t("系统人设决定 agent 的身份：前缀写在系统提示词最前面，后缀写在内置指令之后。保存只影响之后新建的会话，当前会话保持它打开时的提示词。")}
    >
      {!state ? (
        <div className="empty">{t("无法读取人设设置。")}</div>
      ) : (
        <div className="box">
          {state.presets.length > 0 && (
            <>
              <div className="seg" data-text role="group" aria-label={t("预设")}>
                {state.presets.map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    data-action="persona.preset"
                    data-value={p.id}
                    aria-pressed={preset?.id === p.id}
                    disabled={busy}
                    onClick={() => change({
                      prefix: p.prefix,
                      suffix: p.suffix,
                      complete: p.complete,
                      include_runtime_context: p.include_runtime_context,
                    })}
                  >
                    {presetName(p.id)}
                  </button>
                ))}
              </div>
              <p className="persona-name">
                {preset
                  ? presetAbout(preset.id)
                  : blank
                    ? t("未应用预设；直接填写下面的两段即可。")
                    : t("已修改预设内容，按下面的字段保存。")}
              </p>
            </>
          )}

          <div className="pers-field">
            <label className="lb" htmlFor={prefixId}>{t("系统提示词开头")}</label>
            <span className="ds">{t("作为系统提示词的第一段，写在内置指令之前；留空表示不添加。")}</span>
            <textarea
              id={prefixId}
              dir="auto"
              rows={5}
              maxLength={LIMIT}
              spellCheck={false}
              aria-label={t("系统提示词开头")}
              data-action-change="persona.prefix"
              value={draft.prefix}
              disabled={busy}
              onChange={(e) => change({ prefix: e.target.value })}
            />
          </div>

          <div className="pers-field">
            <label className="lb" htmlFor={suffixId}>{t("第一方指令之后")}</label>
            <span className="ds">{t("紧跟在内置指令之后、运行时上下文之前；留空表示不添加。")}</span>
            <textarea
              id={suffixId}
              dir="auto"
              rows={5}
              maxLength={LIMIT}
              spellCheck={false}
              aria-label={t("第一方指令之后")}
              data-action-change="persona.suffix"
              value={draft.suffix}
              disabled={busy}
              onChange={(e) => change({ suffix: e.target.value })}
            />
          </div>

          <div className="lrow">
            <span className="tx">
              <span className="lb">{t("用前缀替换整个系统提示词（complete）")}</span>
              <span className="ds">
                {!draft.prefix.trim() && t("先填写前缀才能开启。")}
                {t("开启后只用前缀作为整个系统提示词，其余内置指令、运行时上下文、记忆与技能不再写入（工具仍可用）。")}
              </span>
            </span>
            <Switch
              data-action="persona.complete"
              on={draft.complete}
              // Off can always be clicked; on needs a prefix to replace the
              // prompt with.
              busy={busy || (!draft.complete && !draft.prefix.trim())}
              label={t("用前缀替换整个系统提示词（complete）")}
              onClick={() => change({ complete: !draft.complete })}
            />
          </div>

          <div className="lrow">
            <span className="tx">
              <span className="lb">{t("不注入可选运行时上下文")}</span>
              <span className="ds">{t("关闭后不注入探测到的运行环境与离线提示；指令、记忆与技能照常写入。")}</span>
            </span>
            <Switch
              data-action="persona.runtime-context"
              on={!draft.include_runtime_context}
              busy={busy}
              label={t("不注入可选运行时上下文")}
              onClick={() => change({ include_runtime_context: !draft.include_runtime_context })}
            />
          </div>

          <div className="lrow">
            <span className="tx">
              <span className="lb">{t("保存")}</span>
              <span className="ds">{t("写入用户配置，从新会话开始生效；当前会话保持它打开时的系统提示词。")}</span>
            </span>
            <button className="act" data-action="persona.save" disabled={busy || !changed} onClick={() => void save()}>
              {t(busy ? "保存中…" : "保存")}
            </button>
          </div>

          {saved && (
            <div className="find" data-lvl="ok" role="status">
              <span className="t">{t("已保存，新会话生效")}</span>
              <span className="why"><code>{state.path}</code></span>
            </div>
          )}
          {error && (
            <div className="find" data-lvl="err" role="alert">
              <span className="t">{t("操作未完成")}</span>
              <span className="why">{error}</span>
            </div>
          )}
        </div>
      )}
    </Group>
  );
}
