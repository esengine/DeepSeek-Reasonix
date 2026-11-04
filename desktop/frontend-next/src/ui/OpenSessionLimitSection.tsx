import { useEffect, useState, useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onOpenSessionLimitChange, openSessionLimit, setOpenSessionLimit } from "../state/prefs";
import { Group } from "./Group";

// How many sessions the window keeps open as tabs. Zero is the default and means
// none is ever folded away on its own; a number folds the one idle longest once
// the window holds that many. Typing is limited to digits, and what is stored is
// read back from the preference, so the box always shows what will happen.
export function OpenSessionLimitSection() {
  const limit = useSyncExternalStore(onOpenSessionLimitChange, openSessionLimit, openSessionLimit);
  const [draft, setDraft] = useState(String(limit));
  useEffect(() => setDraft(String(limit)), [limit]);
  const commit = () => {
    setOpenSessionLimit(draft === "" ? 0 : Number(draft));
    setDraft(String(openSessionLimit()));
  };
  return (
    <Group
      id="open-session-limit"
      title={t("settings.session_tabs_kept_open")}
      hint={t("settings.default_0_nothing_is_folded")}
    >
      <div className="lrow">
        <span className="tx">
          <label className="lb" htmlFor="open-session-limit-input">{t("settings.limit_on_session_tabs")}</label>
          <span className="ds">{t("settings.n_0_means_no_limit")}</span>
        </span>
        <span className="unit-field">
          <input
            id="open-session-limit-input"
            type="text"
            inputMode="numeric"
            value={draft}
            onChange={(e) => setDraft(e.target.value.replace(/\D/g, ""))}
            onBlur={commit}
            data-action-change="appearance.open-session-limit"
            data-action-keydown="appearance.open-session-limit"
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
