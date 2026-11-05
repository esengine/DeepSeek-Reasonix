import { useEffect, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { AutoArchiveSettings } from "../port/boundary";
import { SettingRow } from "./SettingRow";
import { Switch } from "./Switch";

const LIMIT = 3650;

// Archiving hides a conversation from the everyday list and nothing else, so
// the switch ships off and the number only matters once it is on.
export function AutoArchive({ port }: { port: AgentPort }) {
  const [state, setState] = useState<AutoArchiveSettings | null>(null);
  const [days, setDays] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const adopt = (s: AutoArchiveSettings) => {
    setState(s);
    setDays(String(s.days));
  };

  useEffect(() => {
    port.autoArchive().then(adopt).catch(() => setState(null));
  }, [port]);

  const save = async (next: Pick<AutoArchiveSettings, "enabled" | "days">) => {
    setBusy(true);
    setError("");
    try {
      adopt(await port.saveAutoArchive(next));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const commit = () => {
    if (!state) return;
    const n = Number(days);
    if (!Number.isInteger(n) || n < 1 || n > LIMIT) return setError(t("common.enter_a_whole_number_from_2", { n: LIMIT }));
    if (n !== state.days) void save({ enabled: state.enabled, days: n });
  };

  if (!state) return <div className="empty">{t("data.could_not_read_the_auto")}</div>;
  return (
    <>
      <SettingRow id="auto-archive" title={t("data.auto_archive_inactive_conversations")} desc={t("settings.days_before_archiving")}
        more={t("data.archiving_only_moves_a_conversation")}>
        <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={t("data.idle_days")} data-action-keydown="auto-archive.days" data-action-change="auto-archive.days"
              value={days} disabled={busy}
              onChange={(e) => setDays(e.target.value.replace(/\D/g, ""))}
              onBlur={commit}
              onKeyDown={blurOnEnter} />
            <i>{t("data.days")}</i>
          </span>
        <Switch data-action="auto-archive.enabled" on={state.enabled} busy={busy} label={t("data.archive_conversations_left_idle")}
          onClick={() => void save({ enabled: !state.enabled, days: state.days })} />
      </SettingRow>
      {error && <div className="why">{error}</div>}
    </>
  );
}

const blurOnEnter = (e: KeyboardEvent<HTMLInputElement>) => {
  if (e.key === "Enter") e.currentTarget.blur();
};
