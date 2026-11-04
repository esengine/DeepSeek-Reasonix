import { useCallback, useEffect, useMemo, useState } from "react";
import { t } from "../i18n";
import { accountOrigin, ownFetch } from "../port/cloud_remote";
import { AccountError, accountClient, indexedDbKeyStore, uaClassOf } from "../port/phone_device";
import { EnableRefused, realPushBrowser, type PushCategories } from "../port/phone_push";
import { phoneReminders, type PhoneReminders, type Reminders } from "../port/phone_reminders";
import { Group } from "./Group";
import { Switch } from "./Switch";

function realReminders(host: string): PhoneReminders {
  return phoneReminders(host, {
    account: accountClient(accountOrigin, ownFetch),
    keys: indexedDbKeyStore,
    browser: realPushBrowser(),
    uaClass: uaClassOf(navigator.userAgent),
  });
}

function problemFor(error: unknown): string {
  if (error instanceof EnableRefused) {
    if (error.reason === "denied") return t("remote.the_browser_did_not_allow");
    if (error.reason === "unsupported") return t("remote.this_browser_cannot_receive_reminders");
    if (error.reason === "push_service") return t("remote.the_browser_could_not_register");
    return t("remote.this_phone_has_not_been");
  }
  if (error instanceof AccountError && (error.status === 401 || error.code === "remote_reauth_required")) {
    return t("remote.sign_in_to_your_account");
  }
  return t("remote.the_account_service_is_unavailable");
}

/** What this phone does about reminders from one computer. Switching on is what
 *  registers the phone with the account: reading this block creates nothing. */
export function PhoneReminders({ host, reminders }: { host: string; reminders?: PhoneReminders }) {
  const live = useMemo(() => reminders ?? realReminders(host), [host, reminders]);
  const [state, setState] = useState<Reminders | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState("");

  useEffect(() => {
    let current = true;
    live.load().then((s) => current && setState(s)).catch((e) => { if (current) { setState({ kind: "off" }); setProblem(problemFor(e)); } });
    return () => { current = false; };
  }, [live]);

  // The first call into `live` stays in the press that made it: the browser's
  // permission prompt is only shown from one.
  const run = useCallback((go: () => Promise<Reminders>) => {
    setBusy(true);
    setProblem("");
    go().then(setState).catch((e) => setProblem(problemFor(e))).finally(() => setBusy(false));
  }, []);

  const supported = live.supported();
  const on = state?.kind === "on" ? state : null;
  const choose = (patch: Partial<PushCategories>) => on && run(() => live.choose({ ...on.status.categories, ...patch }));
  const occasion = (key: keyof PushCategories, label: string, hint: string) => (
    <div className="lrow subrow">
      <span className="tx"><span className="lb">{label}</span><span className="ds">{hint}</span></span>
      <Switch data-action="phone-device.occasion" data-value={key} on={on?.status.categories[key] ?? false} busy={busy}
        label={label} onClick={() => choose({ [key]: !on?.status.categories[key] })} />
    </div>
  );
  return (
    <Group id="phone-device" title={t("remote.this_phones_reminders")}
      hint={t("remote.when_something_on_your_computer")}>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("remote.remind_this_phone")}</span>
          <span className="ds">{t("remote.the_first_time_you_switch")}</span>
        </span>
        <Switch data-action="phone-device.enabled" on={!!on} busy={busy || state === null || !supported} label={t("remote.remind_this_phone")}
          onClick={() => run(on ? live.disable : live.enable)} />
      </div>
      {!supported && <div className="peernote">{t("remote.this_browser_cannot_receive_notifications")}</div>}
      {state?.kind === "waiting" && (
        <div className="peernote">
          {t("remote.this_phone_is_waiting_for")}
          <button className="act" data-action="phone-device.retry" disabled={busy} onClick={() => run(live.enable)}>{t("remote.try_again")}</button>
        </div>
      )}
      {state?.kind === "removed" && (
        <div className="peernote">
          {t("remote.the_computer_has_removed_this")}
          <button className="act" data-action="phone-device.restart" disabled={busy} onClick={() => run(live.startOver)}>{t("remote.register_again")}</button>
        </div>
      )}
      {on && (
        <>
          {occasion("approval", t("remote.an_action_needs_approval"), t("remote.the_model_wants_to_touch"))}
          {occasion("input", t("remote.the_model_asks_me_something"), t("remote.the_model_has_stopped_and"))}
          {occasion("error", t("remote.a_turn_ends_in_an"), t("remote.the_turn_ended_in_an"))}
          {on.status.failureCount > 0 && (
            <div className="peernote" role="status">
              {on.status.lastFailureStatus
                ? t("remote.the_last_reminder_was_not_2", { status: on.status.lastFailureStatus })
                : t("remote.the_last_reminder_was_not")}
            </div>
          )}
        </>
      )}
      {problem && <div className="why" role="alert">{problem}</div>}
    </Group>
  );
}
