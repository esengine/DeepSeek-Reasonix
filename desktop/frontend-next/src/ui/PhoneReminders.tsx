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
    if (error.reason === "denied") return t("phoneReminders.theBrowserDidNotAllow");
    if (error.reason === "unsupported") return t("phoneReminders.thisBrowserCannotReceiveReminders");
    if (error.reason === "push_service") return t("phoneReminders.theBrowserCouldNotRegister");
    return t("phoneReminders.thisPhoneHasNotBeen");
  }
  if (error instanceof AccountError && (error.status === 401 || error.code === "remote_reauth_required")) {
    return t("phoneReminders.signInToYourAccount");
  }
  return t("phoneReminders.theAccountServiceIsUnavailable");
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
    <Group id="phone-device" title={t("common.thisPhoneSReminders")}
      hint={t("phoneReminders.whenSomethingOnYourComputer")}>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("phoneReminders.remindThisPhone")}</span>
          <span className="ds">{t("phoneReminders.theFirstTimeYouSwitch")}</span>
        </span>
        <Switch data-action="phone-device.enabled" on={!!on} busy={busy || state === null || !supported} label={t("phoneReminders.remindThisPhone")}
          onClick={() => run(on ? live.disable : live.enable)} />
      </div>
      {!supported && <div className="peernote">{t("phoneReminders.thisBrowserCannotReceiveNotification")}</div>}
      {state?.kind === "waiting" && (
        <div className="peernote">
          {t("phoneReminders.thisPhoneIsWaitingFor")}
          <button className="act" data-action="phone-device.retry" disabled={busy} onClick={() => run(live.enable)}>{t("phoneReminders.tryAgain")}</button>
        </div>
      )}
      {state?.kind === "removed" && (
        <div className="peernote">
          {t("phoneReminders.theComputerHasRemovedThis")}
          <button className="act" data-action="phone-device.restart" disabled={busy} onClick={() => run(live.startOver)}>{t("phoneReminders.registerAgain")}</button>
        </div>
      )}
      {on && (
        <>
          {occasion("approval", t("phoneReminders.anActionNeedsApproval"), t("phoneReminders.theModelWantsToTouch"))}
          {occasion("input", t("phoneReminders.theModelAsksMeSomething"), t("phoneReminders.theModelHasStoppedAnd"))}
          {occasion("error", t("phoneReminders.aTurnEndsInAn"), t("common.theTurnEndedInAn"))}
          {on.status.failureCount > 0 && (
            <div className="peernote" role="status">
              {on.status.lastFailureStatus
                ? t("phoneReminders.theLastReminderWasNot", { status: on.status.lastFailureStatus })
                : t("phoneReminders.theLastReminderWasNot2")}
            </div>
          )}
        </>
      )}
      {problem && <div className="why" role="alert">{problem}</div>}
    </Group>
  );
}
