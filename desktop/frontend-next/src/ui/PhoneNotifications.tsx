import { useCallback, useEffect, useState, type ReactNode } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/http_error";
import type { AgentPort, NotifyPrefs, PhoneNotifyPrefs } from "../port/port";
import type { HubPort } from "../port/hub";
import type { PhoneView } from "../port/share";
import { Group } from "./Group";
import { Switch } from "./Switch";

const POLL_MS = 10_000;

const when = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
};

/** What this computer tells the phones that have signed in to the same account.
 *  Only the kind of occasion and a count leave this machine: no session name,
 *  command or text. A phone chooses on its own page whether to receive them. */
export function PhoneNotifications({ hub, port, onError }: { hub: HubPort; port: AgentPort; onError: (e: unknown) => void }) {
  const [prefs, setPrefs] = useState<NotifyPrefs | null>(null);
  const [phones, setPhones] = useState<PhoneView[] | null>(null);
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState("");
  const [confirm, setConfirm] = useState("");

  useEffect(() => {
    let live = true;
    port.notifyPrefs().then((p) => live && setPrefs(p)).catch(() => live && setPrefs(null));
    return () => { live = false; };
  }, [port]);

  const readPhones = useCallback(async () => {
    try {
      const list = await hub.sharePhones();
      setPhones(list ? list.phones : null);
      setProblem("");
    } catch (e) {
      setProblem(reason(e));
      if (e instanceof HttpError && e.reason?.code === "phones.unavailable") setPhones(null);
    }
  }, [hub]);

  useEffect(() => {
    void readPhones();
    const timer = setInterval(() => void readPhones(), POLL_MS);
    return () => clearInterval(timer);
  }, [readPhones]);

  const flip = useCallback(
    (patch: Partial<PhoneNotifyPrefs>) => {
      if (!prefs) return;
      port.setNotifyPrefs({ ...prefs, phone: { ...prefs.phone, ...patch } }).then((got) => got && setPrefs(got)).catch(onError);
    },
    [port, prefs, onError],
  );

  const act = async (id: string, fn: () => Promise<{ phones: PhoneView[] }>) => {
    setBusy(id);
    try {
      setPhones((await fn()).phones);
      setProblem("");
      setConfirm("");
    } catch (e) {
      setProblem(reason(e));
    } finally {
      setBusy("");
    }
  };

  // A kernel with no window answers no preferences at all, and a settings page
  // does not grow a heading for a switch it cannot show.
  if (!prefs) return null;
  const on = prefs.phone.enabled;
  const row = (label: string, hint: string, sw: ReactNode) => (
    <div className="lrow subrow" data-off={on ? undefined : ""}>
      <span className="tx">
        <span className="lb">{label}</span>
        <span className="ds">{hint}</span>
      </span>
      {sw}
    </div>
  );
  return (
    <Group id="phone-notify" title={t("common.phoneNotifications")}
      hint={t("phoneNotifications.whenSomethingIsWaitingFor")}>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("phoneNotifications.remindMyPhones")}</span>
          <span className="ds">{t("phoneNotifications.whenOffThisComputerReports")}</span>
        </span>
        <Switch data-action="phone-notify.enabled" on={on} label={t("phoneNotifications.remindMyPhones")} onClick={() => flip({ enabled: !on })} />
      </div>
      {row(t("common.whenSomethingWaitsForApproval"), t("common.theModelWantsToTouch"),
        <Switch data-action="phone-notify.approval" on={prefs.phone.approval} busy={!on} label={t("common.whenSomethingWaitsForApproval")} onClick={() => flip({ approval: !prefs.phone.approval })} />)}
      {row(t("common.whenTheModelAsksYou"), t("common.theModelStoppedToAsk"),
        <Switch data-action="phone-notify.input" on={prefs.phone.input} busy={!on} label={t("common.whenTheModelAsksYou")} onClick={() => flip({ input: !prefs.phone.input })} />)}
      {row(t("phoneNotifications.whenATurnFails"), t("common.theTurnEndedInAn"),
        <Switch data-action="phone-notify.error" on={prefs.phone.error} busy={!on} label={t("phoneNotifications.whenATurnFails")} onClick={() => flip({ error: !prefs.phone.error })} />)}

      {phones && (
        <>
          <div className="peernote"><b>{t("phoneNotifications.enrolledPhones")}</b></div>
          {phones.length === 0 && <div className="peernote">{t("phoneNotifications.noPhoneIsEnrolledYet")}</div>}
          <ul className="wa-list" aria-label={t("phoneNotifications.enrolledPhones")}>
            {phones.map((p) => (
              <li key={p.id}>
                <span className="tx">
                  <span className="lb">{p.name}{p.ordinal ? ` · ${t("common.device", { n: p.ordinal })}` : ""}</span>
                  <span className="ds">
                    {p.state === "pending" ? t("phoneNotifications.waitingForYouFingerprint", { id: p.keyThumbprint }) : t("phoneNotifications.lastUsed", { at: when(p.lastSeenAt) })}
                  </span>
                </span>
                <span className="acts">
                  {p.state === "pending" ? (
                    <>
                      <button className="act" data-action="phone.add" data-target={p.id} disabled={busy === p.id} onClick={() => void act(p.id, () => hub.resolvePhone(p.id, "add"))}>{t("common.add")}</button>
                      {p.replaceCandidate && (
                        <button className="act" data-action="phone.replace" data-target={p.id} disabled={busy === p.id} onClick={() => void act(p.id, () => hub.resolvePhone(p.id, "replace", p.replaceCandidate))}>{t("phoneNotifications.replaceTheOldDevice")}</button>
                      )}
                      <button className="act ghost" data-action="phone.reject" data-target={p.id} disabled={busy === p.id} onClick={() => void act(p.id, () => hub.resolvePhone(p.id, "reject"))}>{t("common.refuse")}</button>
                    </>
                  ) : confirm === p.id ? (
                    <button className="act danger" data-action="phone.revoke" data-target={p.id} disabled={busy === p.id} onClick={() => void act(p.id, () => hub.revokePhone(p.id))}>{t("phoneNotifications.confirmRemoval")}</button>
                  ) : (
                    <button className="act ghost" data-action="phone.revoke-ask" data-target={p.id} onClick={() => setConfirm(p.id)}>{t("common.remove")}</button>
                  )}
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
      {problem && <div className="why" role="alert">{problem}</div>}
    </Group>
  );
}
