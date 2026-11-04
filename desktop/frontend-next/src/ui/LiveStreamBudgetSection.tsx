import { useEffect, useState, useSyncExternalStore } from "react";
import { t } from "../i18n";
import { liveStreamBudget, MAX_LIVE_STREAM_BUDGET, onLiveStreamBudgetChange, setLiveStreamBudget } from "../state/prefs";
import { Group } from "./Group";

// How many open session tabs may keep a live connection at once. Empty is
// automatic. A browser holds six connections to one address and the session list
// takes one, so the window keeps a few live and reads the rest when they are shown.
export function LiveStreamBudgetSection() {
  const chosen = useSyncExternalStore(onLiveStreamBudgetChange, liveStreamBudget, liveStreamBudget);
  const [draft, setDraft] = useState(chosen === null ? "" : String(chosen));
  useEffect(() => setDraft(chosen === null ? "" : String(chosen)), [chosen]);
  const commit = () => {
    setLiveStreamBudget(draft === "" ? null : Number(draft));
    const now = liveStreamBudget();
    setDraft(now === null ? "" : String(now));
  };
  return (
    <Group
      id="live-stream-budget"
      title={t("settings.session_tabs_kept_live")}
      hint={t("settings.empty_means_automatic_a_browser")}
    >
      <div className="lrow">
        <span className="tx">
          <label className="lb" htmlFor="live-stream-budget-input">{t("settings.tabs_with_a_live_connection")}</label>
          <span className="ds">{t("settings.empty_is_automatic_1_to", { max: MAX_LIVE_STREAM_BUDGET })}</span>
        </span>
        <span className="unit-field">
          <input
            id="live-stream-budget-input"
            type="text"
            inputMode="numeric"
            placeholder={t("common.auto")}
            value={draft}
            onChange={(e) => setDraft(e.target.value.replace(/\D/g, ""))}
            onBlur={commit}
            data-action-change="appearance.live-stream-budget"
            data-action-keydown="appearance.live-stream-budget"
            onKeyDown={(e) => {
              if (e.key !== "Enter") return;
              commit();
              e.currentTarget.blur();
            }}
          />
          <i>{t("settings.tabs")}</i>
        </span>
      </div>
    </Group>
  );
}
