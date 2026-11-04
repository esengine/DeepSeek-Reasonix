import { useEffect, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason, say } from "../i18n/kernel";
import type { AgentPort, ModState, ModStatus, ModsSettings, ModsSettingsEdit } from "../port/port";
import { HttpError } from "../port/port";
import { Group } from "./Group";
import { Switch } from "./Switch";
import { MODS_SHOWN } from "./modsgate";

const BUDGET_MAX = 55000;

// The words and the mark for each state: a state never rests on colour alone.
const STATE: Record<ModState, { mark: string; word: string }> = {
  inactive: { mark: "○", word: "未运行" },
  starting: { mark: "◐", word: "正在启动" },
  running: { mark: "●", word: "运行中" },
  restarting: { mark: "↻", word: "正在重启" },
  disabled: { mark: "⊘", word: "已停用" },
};

/** Claude Code mods: modules an installed package ships, run in a process of
 *  their own with the user's full trust. The switch is the only authorisation to
 *  run any of them, so it says what that means in plain words, and a client that
 *  may not edit it sees it read-only rather than failing on a click. */
export function ModHost({ port, shown = MODS_SHOWN }: { port: AgentPort; shown?: boolean }) {
  return shown ? <ModHostBlock port={port} /> : null;
}

function ModHostBlock({ port }: { port: AgentPort }) {
  const [settings, setSettings] = useState<ModsSettings | null>(null);
  const [mods, setMods] = useState<ModStatus[] | null>(null);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [readOnly, setReadOnly] = useState(false);
  const [error, setError] = useState("");

  const adopt = (s: ModsSettings) => {
    setSettings(s);
    setDraft(String(s.hook_budget_ms));
  };

  useEffect(() => {
    let alive = true;
    port.modsSettings().then((s) => alive && adopt(s)).catch((e) => alive && setError(reason(e)));
    port.mods().then((m) => alive && setMods(m)).catch(() => alive && setMods([]));
    return () => {
      alive = false;
    };
  }, [port]);

  const save = async (edit: ModsSettingsEdit) => {
    setBusy(true);
    setError("");
    try {
      adopt(await port.saveModsSettings(edit));
      setMods(await port.mods());
    } catch (e) {
      if (e instanceof HttpError && e.reason?.code === "mods.editing_disabled") setReadOnly(true);
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const commit = () => {
    if (!settings) return;
    const n = Number(draft);
    if (!Number.isInteger(n) || n < 0 || n > BUDGET_MAX) return setError(t("请输入 {min} 到 {max} 之间的整数。", { min: 0, max: BUDGET_MAX }));
    if (n !== settings.hook_budget_ms) void save({ hook_budget_ms: n });
  };

  const locked = busy || readOnly;
  const body = !settings ? (
    <div className="empty">{error || t("无法读取 mod 设置。")}</div>
  ) : (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("运行 Claude Code mod")}</span>
          <span className="ds">{t("默认关闭。打开后，已安装的 mod 里的代码会以你的权限在这台电脑上运行（完全信任）：没有沙盒，不受网络出口治理，它做的事也不会被本应用观测到。关闭时一个 mod 都不会运行。")}</span>
        </span>
        <Switch data-action="mods.enabled" on={settings.enabled} busy={locked} label={t("运行 Claude Code mod")} onClick={() => void save({ enabled: !settings.enabled })} />
      </div>
      <div className="lrow threshold-row">
        <span className="tx">
          <span className="lb">{t("单个钩子的运行时限")}</span>
          <span className="ds">{t("超过这个时间，mod 的进程会被结束。0 表示使用默认的 10 秒，最大 {max} 毫秒。", { max: BUDGET_MAX })}</span>
        </span>
        <div className="threshold-control">
          <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={t("单个钩子的运行时限")} data-action-keydown="mods.budget" data-action-change="mods.budget"
              value={draft} disabled={locked}
              onChange={(e) => setDraft(e.target.value.replace(/\D/g, ""))}
              onBlur={commit}
              onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => { if (e.key === "Enter") e.currentTarget.blur(); }} />
            <i>{t("毫秒")}</i>
          </span>
        </div>
      </div>
      {readOnly && <div className="peernote" role="status">{t("这些设置现在是只读的：这个窗口没有被授予编辑权限。")}</div>}
      {error && <div className="why" role="alert">{error}</div>}
      <ul className="modlist" aria-label={t("已安装的 mod")}>
        {(mods ?? []).map((m) => (
          <li key={m.plugin} data-state={m.state}>
            <div className="modhead">
              <b>{m.plugin}</b>
              <span className="modstate"><span aria-hidden="true">{STATE[m.state].mark}</span> {t(STATE[m.state].word)}</span>
            </div>
            {m.code && <div className="ds">{say({ code: m.code, error: m.detail })}</div>}
            {m.restarts > 0 && <div className="ds">{t("已重启 {n} 次", { n: m.restarts })}</div>}
            {!!m.events?.length && <div className="ds">{t("它注册的事件")}：{m.events.join("、")}</div>}
            {!!m.unserved?.length && <div className="ds">{t("本应用不会触发这些事件")}：{m.unserved.join("、")}</div>}
          </li>
        ))}
        {mods && mods.length === 0 && <li className="ds">{t("没有安装带 mod 的插件包。")}</li>}
      </ul>
    </div>
  );

  return <Group id="mods" title={t("Claude Code mod")} hint={t("插件包里的 mod 可以在会话的事件上挂钩子。")}>{body}</Group>;
}
