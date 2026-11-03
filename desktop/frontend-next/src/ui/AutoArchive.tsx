import { useEffect, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { AutoArchiveSettings } from "../port/boundary";
import { Group } from "./Group";
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
    if (!Number.isInteger(n) || n < 1 || n > LIMIT) return setError(t("common.enterAWholeNumberFrom", { n: LIMIT }));
    if (n !== state.days) void save({ enabled: state.enabled, days: n });
  };

  const body = !state ? (
    <div className="empty">{t("autoArchive.couldNotReadTheAuto")}</div>
  ) : (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("autoArchive.archiveConversationsLeftIdle")}</span>
          <span className="ds">{t("autoArchive.offByDefaultWhenOn")}</span>
        </span>
        <Switch
          data-action="auto-archive.enabled"
          on={state.enabled}
          busy={busy}
          label={t("autoArchive.archiveConversationsLeftIdle")}
          onClick={() => void save({ enabled: !state.enabled, days: state.days })}
        />
      </div>
      <div className="lrow threshold-row">
        <span className="tx">
          <span className="lb">{t("autoArchive.idleDays")}</span>
          <span className="ds">{t("autoArchive.archivedOnlyOnceItHas", { n: state.defaultDays })}</span>
        </span>
        <div className="threshold-control">
          <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={t("autoArchive.idleDays")} data-action-keydown="auto-archive.days" data-action-change="auto-archive.days"
              value={days} disabled={busy}
              onChange={(e) => setDays(e.target.value.replace(/\D/g, ""))}
              onBlur={commit}
              onKeyDown={blurOnEnter} />
            <i>{t("autoArchive.days")}</i>
          </span>
        </div>
      </div>
      {error && <div className="why">{error}</div>}
    </div>
  );

  return (
    <Group id="auto-archive" title={t("common.autoArchiveInactiveConversations")}
      hint={t("autoArchive.archivingOnlyMovesAConversation")}>
      {body}
    </Group>
  );
}

const blurOnEnter = (e: KeyboardEvent<HTMLInputElement>) => {
  if (e.key === "Enter") e.currentTarget.blur();
};
