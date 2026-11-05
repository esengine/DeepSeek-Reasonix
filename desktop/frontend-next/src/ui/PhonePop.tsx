import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { HubPort } from "../port/hub";
import type { CloudDevice, CloudRemoteStatus, PairedDevice } from "../port/share";
import { copyText } from "./CopyButton";
import { useDismiss } from "./dismiss";
import { clock, deviceLabel, type Share, useShare } from "./PhoneAccess";
import { Switch } from "./Switch";
import { TooltipButton } from "./TooltipButton";

/** The chrome's way to put a phone on this window: a code on a card, one
 *  press from anywhere. Drawn only where the kernel has a window to share —
 *  a browser tab or a paired phone gets nothing here. */
export function PhonePop({ hub }: { hub: HubPort }) {
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const close = useCallback(() => setOpen(false), []);
  useDismiss(open, box, close);
  // Watched while the door is open, card or not: the count on the button and
  // the note that a phone came or went are the window's to show unasked.
  // A failure in the card is said in the card, next to what failed.
  const [failure, setFailure] = useState("");
  const share = useShare(hub, useCallback((e: unknown) => setFailure(reason(e)), []));
  const { refresh, newCode, newCloudCode } = share;
  const shareOpen = share.st?.open ?? false;
  const offerHeld = useRef(false);
  offerHeld.current = share.offer !== null;
  const cloudOfferHeld = useRef(false);
  cloudOfferHeld.current = share.cloudOffer !== null;

  // Opening the card is asking for a code, once per opening: after a phone
  // spends it, the next one is asked for by hand. Another surface may have shut
  // the door since the card was last drawn, so the code waits for the read.
  const minted = useRef(false);
  useEffect(() => {
    if (!open) {
      minted.current = false;
      setFailure("");
      return;
    }
    let live = true;
    refresh()
      .then((now) => {
        if (!live || minted.current) return;
        minted.current = true;
        if (now?.cloudRemote?.online && !cloudOfferHeld.current) {
          void newCloudCode();
        } else if (now?.open && !(offerHeld.current && now.offerExpires)) {
          void newCode();
        }
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [open, refresh, newCode, newCloudCode]);

  const localNote = usePresenceNote(share.st?.devices, open);
  const cloudNote = useCloudPresenceNote(share.st?.cloudDevices, open);

  if (!share.st) return null;
  const online = share.st.devices.filter((d) => d.online).length + share.st.cloudDevices.length;
  return (
    <div className="phonepop" ref={box}>
      <TooltipButton
        className="thbtn phone-action"
        data-action="share.card"
        data-live={shareOpen ? "" : undefined}
        data-online={online > 0 ? "" : undefined}
        aria-expanded={open}
        aria-label={t("remote.device_access")}
        title={online > 0 ? t("remote.device_access_n_online", { n: online }) : t("remote.device_access")}
        onClick={() => setOpen((v) => !v)}
      >
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <rect x="2.5" y="2.5" width="4" height="4" rx=".6" />
          <rect x="9.5" y="2.5" width="4" height="4" rx=".6" />
          <rect x="2.5" y="9.5" width="4" height="4" rx=".6" />
          <path d="M9.5 9.5h1.6v1.6M13.5 9.5v.01M9.5 13.5h.01M12 12h1.5v1.5H12Z" />
        </svg>
        {online > 0 && <b className="pc-count">{online}</b>}
      </TooltipButton>
      {open && <PhoneCard share={share} failure={failure} />}
      {(cloudNote || localNote) && <div className="pc-note-pop" role="status">{cloudNote || localNote}</div>}
    </div>
  );
}

/** What changed between two reads of the device list, as the one line worth
 *  saying: a phone that came online, went offline, or was unpaired. Each is
 *  named by its place in the list it was read from. */
export function presenceNote(prev: PairedDevice[], next: PairedDevice[]): string {
  let said = "";
  next.forEach((d, i) => {
    const was = prev.find((p) => p.id === d.id);
    if (d.online && !was?.online) said = t("remote.device_n_connected", { n: i + 1 });
    else if (!d.online && was?.online) said = t("remote.device_n_disconnected", { n: i + 1 });
  });
  prev.forEach((p, i) => {
    if (!next.some((d) => d.id === p.id)) said = t("remote.device_n_disconnected", { n: i + 1 });
  });
  return said;
}

export function cloudPresenceNote(prev: CloudDevice[], next: CloudDevice[]): string {
  const connected = next.find((device) => !prev.some((was) => was.id === device.id));
  if (connected) return t("remote.web_studio_connected");
  const disconnected = prev.find((device) => !next.some((now) => now.id === device.id));
  return disconnected ? t("remote.web_studio_disconnected") : "";
}

function cloudRemoteNote(status: CloudRemoteStatus | undefined): string {
  if (status?.online) {
    return t("remote.internet_access_requires_the_same");
  }
  switch (status?.reason) {
    case "relay_unreachable":
      return t("remote.the_relay_cannot_be_reached");
    case "relay_refused":
      return t("remote.the_relay_refused_this_devices");
    default:
      return t("remote.sign_in_to_reasonix_to");
  }
}

/** A line under the button when a phone comes or goes, for as long as it takes
 *  to read. Not while the card is open: the list there already shows it. */
function usePresenceNote(devices: PairedDevice[] | undefined, open: boolean): string {
  const [note, setNote] = useState("");
  const before = useRef<PairedDevice[] | null>(null);
  const timer = useRef<number | null>(null);
  useEffect(() => () => { if (timer.current !== null) window.clearTimeout(timer.current); }, []);
  useEffect(() => {
    if (!devices) return;
    const prev = before.current;
    before.current = devices;
    if (!prev || open) return;
    const said = presenceNote(prev, devices);
    if (!said) return;
    setNote(said);
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setNote(""), 3200);
  }, [devices, open]);
  return open ? "" : note;
}

function useCloudPresenceNote(devices: CloudDevice[] | undefined, open: boolean): string {
  const [note, setNote] = useState("");
  const before = useRef<CloudDevice[] | null>(null);
  const timer = useRef<number | null>(null);
  useEffect(() => () => { if (timer.current !== null) window.clearTimeout(timer.current); }, []);
  useEffect(() => {
    if (!devices) return;
    const prev = before.current;
    before.current = devices;
    if (!prev || open) return;
    const said = cloudPresenceNote(prev, devices);
    if (!said) return;
    setNote(said);
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setNote(""), 3200);
  }, [devices, open]);
  return open ? "" : note;
}

/** The phones already connected, then the two ways a phone gets here: the
 *  same network, which has to be switched on, and the account-gated relay. */
function PhoneCard({ share, failure }: { share: Share; failure: string }) {
  const { st, ip, pick, offer, cloudOffer, busy, newCode, newCloudCode, toggle, revoke } = share;
  const [copied, setCopied] = useState(false);
  const [arming, setArming] = useState("");
  const arm = useRef<number | null>(null);
  useEffect(() => () => { if (arm.current !== null) window.clearTimeout(arm.current); }, []);
  if (!st) return null;
  const noNetwork = st.addresses.length === 0;
  const chosen = ip || (st.open ? st.origin?.replace(/^https?:\/\//, "").replace(/:\d+$/, "") : "") || st.addresses[0]?.ip || "";

  const copy = (url: string) =>
    copyText(url)
      .then(() => {
        setCopied(true);
        window.setTimeout(() => setCopied(false), 1600);
      })
      .catch(() => {});

  // Disconnecting takes a second press on the same place, which is cheaper
  // than a dialog in a card this small and still not one slip.
  const disconnect = (id: string) => {
    if (arm.current !== null) window.clearTimeout(arm.current);
    if (arming !== id) {
      setArming(id);
      arm.current = window.setTimeout(() => setArming(""), 3000);
      return;
    }
    setArming("");
    void revoke(id);
  };

  const phones = st.cloudDevices.length + (st.open ? st.devices.filter((d) => d.online).length : 0);
  return (
    <div className="phonecard" role="dialog" aria-label={t("remote.device_access")}>
      <header>
        <span>
          <b>{t("remote.use_studio_on_your_phone")}</b>
          <small>{t("remote.scan_with_your_phone_to")}</small>
        </span>
      </header>

      {failure && <p className="pc-err" role="alert">{failure}</p>}

      {(st.cloudDevices.length > 0 || (st.open && st.devices.length > 0)) && (
        <section className="pc-phones">
          <div className="pc-hd">
            <b>{t("remote.n_phones_connected", { n: phones })}</b>
          </div>
          {st.cloudDevices.map((device) => (
            <div className="pc-dev" key={device.id} data-online="">
              <i aria-hidden="true" />
              <span>{t("remote.device_n", { n: device.ordinal })}</span>
              <small>{device.name} · {t("remote.from_anywhere")}</small>
              <button
                data-action={arming === device.id ? "share.revoke" : "share.ask-revoke"}
                data-target={device.id}
                data-armed={arming === device.id ? "" : undefined}
                onClick={() => disconnect(device.id)}
              >
                {arming === device.id ? t("remote.disconnect_2") : t("remote.disconnect")}
              </button>
            </div>
          ))}
          {st.open && st.devices.map((d, i) => (
            <div className="pc-dev" key={d.id} data-online={d.online ? "" : undefined}>
              <i aria-hidden="true" />
              <span title={d.name}>{deviceLabel(i)}</span>
              <small>{t("remote.same_wifi")} · {d.online ? t("remote.online") : t("remote.last_seen_time", { time: clock(d.lastSeen) })}</small>
              <button
                data-action={arming === d.id ? "share.revoke" : "share.ask-revoke"}
                data-target={d.id}
                data-armed={arming === d.id ? "" : undefined}
                onClick={() => disconnect(d.id)}
              >
                {arming === d.id ? t("remote.disconnect_2") : t("remote.disconnect")}
              </button>
            </div>
          ))}
        </section>
      )}

      <p className="pc-cap">{t("remote.how_the_phone_reaches_this")}</p>

      <section className="pc-route" data-on={st.open ? "" : undefined}>
        <div className="pc-hd">
          <span>
            <b>{t("remote.same_wifi")}</b>
            <small>{chosen ? t("remote.ip_fastest", { ip: chosen }) : t("remote.faster_without_the_relay")}</small>
          </span>
          <Switch data-action="share.toggle" on={st.open} busy={busy || noNetwork} label={t("remote.allow_local_network_access")} onClick={() => void toggle()} />
        </div>

        {st.open && (
          <div className="pc-code">
            {offer ? (
              <>
                <img src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(offer.qr)}`} alt={t("remote.pairing_qr_code")} width={148} height={148} />
                <span className="pc-side">
                  <span className="pc-when">{t("remote.local_pairing_valid_until_time", { time: clock(offer.expires) })}</span>
                  <span className="pc-acts">
                    <button data-action="share.copy" onClick={() => void copy(offer.url)}>{copied ? t("common.copied") : t("remote.copy_link")}</button>
                    <i aria-hidden="true" />
                    <button data-action="share.offer" disabled={busy} onClick={() => void newCode()}>{t("common.new_code")}</button>
                  </span>
                </span>
              </>
            ) : (
              <button className="pc-mint" data-action="share.offer" disabled={busy} onClick={() => void newCode()}>
                {t("remote.show_local_network_qr_code")}
              </button>
            )}
          </div>
        )}

        {st.addresses.length > 1 && (
          <label className="pc-row">
            <span>{t("common.network")}</span>
            <select data-size="sm" data-action="share.address" value={chosen} disabled={busy} onChange={(e) => pick(e.target.value)}>
              {st.addresses.map((a) => (
                <option key={a.ip} value={a.ip}>
                  {a.kind === "tailnet" ? `${a.ip} · Tailscale` : a.kind === "virtual" ? `${a.ip} · ${t("remote.virtual_adapter")}` : `${a.ip} · ${a.interface}`}
                </option>
              ))}
            </select>
          </label>
        )}
        {st.open && st.devices.length === 0 && <p className="pc-none">{t("remote.no_phone_connected_yet")}</p>}
      </section>

      <section className="pc-route" data-on={st.cloudRemote?.online ? "" : undefined}>
        <div className="pc-hd">
          <span>
            <b>{t("remote.from_anywhere")}</b>
            <small>{t("remote.needs_signing_in")}</small>
          </span>
        </div>
        {st.cloudRemote?.online ? (
          <div className="pc-code" data-cloud="">
            {cloudOffer ? (
              <>
                <img src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(cloudOffer.qr)}`} alt={t("remote.internet_connection_qr_code")} width={148} height={148} />
                <span className="pc-side">
                  <span className="pc-when">{t("remote.scan_with_your_phone_works")}</span>
                  <span className="pc-acts">
                    <button data-action="share.cloud-copy" onClick={() => void copy(cloudOffer.url)}>{copied ? t("common.copied") : t("remote.copy_link")}</button>
                    <i aria-hidden="true" />
                    <button data-action="share.cloud-offer" disabled={busy} onClick={() => void newCloudCode()}>{t("remote.refresh_qr_code")}</button>
                  </span>
                </span>
              </>
            ) : (
              <button className="pc-mint" data-action="share.cloud-offer" disabled={busy} onClick={() => void newCloudCode()}>
                {busy ? t("remote.creating_qr_code") : t("remote.show_internet_connection_qr_code")}
              </button>
            )}
          </div>
        ) : (
          <p className="pc-note">{cloudRemoteNote(st.cloudRemote)}</p>
        )}
      </section>
    </div>
  );
}
