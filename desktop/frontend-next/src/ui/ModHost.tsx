import { useEffect, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason, say } from "../i18n/kernel";
import type { AgentPort, ModState, ModStatus, ModsSettings, ModsSettingsEdit } from "../port/port";
import { HttpError } from "../port/port";
import { Group } from "./Group";
import { Switch } from "./Switch";
import { MODS_SHOWN } from "./modsgate";
import "../styles/mods.css";

const BUDGET_MAX = 55000;

// The words and the mark for each state: a state never rests on colour alone.
const STATE: Record<ModState, { mark: string; word: string }> = {
  inactive: { mark: "○", word: "modHost.notRunning" },
  starting: { mark: "◐", word: "modHost.starting" },
  running: { mark: "●", word: "common.running" },
  restarting: { mark: "↻", word: "modHost.restarting" },
  disabled: { mark: "⊘", word: "common.suspended" },
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
    if (!Number.isInteger(n) || n < 0 || n > BUDGET_MAX) return setError(t("common.enterAWholeNumberFrom2", { min: 0, max: BUDGET_MAX }));
    if (n !== settings.hook_budget_ms) void save({ hook_budget_ms: n });
  };

  const locked = busy || readOnly;
  const body = !settings ? (
    <div className="empty">{error || t("modHost.theModSettingsCouldNot")}</div>
  ) : (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("modHost.runClaudeCodeMods")}</span>
          <span className="ds">{t("modHost.offByDefaultWhenOn")}</span>
        </span>
        <Switch data-action="mods.enabled" on={settings.enabled} busy={locked} label={t("modHost.runClaudeCodeMods")} onClick={() => void save({ enabled: !settings.enabled })} />
      </div>
      <div className="lrow threshold-row">
        <span className="tx">
          <span className="lb">{t("modHost.timeLimitForOneHook")}</span>
          <span className="ds">{t("modHost.pastThisTimeTheMod", { max: BUDGET_MAX })}</span>
        </span>
        <div className="threshold-control">
          <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={t("modHost.timeLimitForOneHook")} data-action-keydown="mods.budget" data-action-change="mods.budget"
              value={draft} disabled={locked}
              onChange={(e) => setDraft(e.target.value.replace(/\D/g, ""))}
              onBlur={commit}
              onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => { if (e.key === "Enter") e.currentTarget.blur(); }} />
            <i>{t("modHost.ms")}</i>
          </span>
        </div>
      </div>
      {readOnly && <div className="peernote" role="status">{t("common.theseSettingsAreReadOnly")}</div>}
      {error && <div className="why" role="alert">{error}</div>}
      <ul className="modlist" aria-label={t("modHost.installedMods")}>
        {(mods ?? []).map((m) => (
          <li key={m.plugin} data-state={m.state}>
            <div className="modhead">
              <b>{m.plugin}</b>
              <span className="modstate"><span aria-hidden="true">{STATE[m.state].mark}</span> {t(STATE[m.state].word)}</span>
            </div>
            {m.code && <div className="ds">{say({ code: m.code, error: m.detail })}</div>}
            {m.restarts > 0 && <div className="ds">{t("modHost.restartedTimes", { n: m.restarts })}</div>}
            {!!m.events?.length && <div className="ds">{t("modHost.eventsItRegistered")}：{m.events.join(t("common.listSeparator"))}</div>}
            {!!m.unserved?.length && <div className="ds">{t("modHost.eventsThisAppNeverRaises")}：{m.unserved.join(t("common.listSeparator"))}</div>}
          </li>
        ))}
        {mods && mods.length === 0 && <li className="ds">{t("modHost.noInstalledPackageCarriesA")}</li>}
      </ul>
    </div>
  );

  return <Group id="mods" title={t("settings.claudeCodeMods")} hint={t("modHost.aModInAPackage")}>{body}</Group>;
}
