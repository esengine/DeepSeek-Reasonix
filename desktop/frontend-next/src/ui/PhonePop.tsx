import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { HubPort } from "../port/hub";
import type { CloudDevice, CloudRemoteStatus, PairedDevice } from "../port/share";
import { copyText } from "./CopyButton";
import { useDismiss } from "./dismiss";
import { clock, deviceLabel, type Share, useShare } from "./PhoneAccess";
import { Switch } from "./Switch";

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
      <button
        className="thbtn phone-action"
        data-action="share.card"
        data-live={shareOpen ? "" : undefined}
        data-online={online > 0 ? "" : undefined}
        aria-expanded={open}
        aria-label={t("设备访问")}
        title={online > 0 ? t("设备访问 · {n} 台在线", { n: online }) : t("设备访问")}
        onClick={() => setOpen((v) => !v)}
      >
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <rect x="2.5" y="2.5" width="4" height="4" rx=".6" />
          <rect x="9.5" y="2.5" width="4" height="4" rx=".6" />
          <rect x="2.5" y="9.5" width="4" height="4" rx=".6" />
          <path d="M9.5 9.5h1.6v1.6M13.5 9.5v.01M9.5 13.5h.01M12 12h1.5v1.5H12Z" />
        </svg>
        {online > 0 && <b className="pc-count">{online}</b>}
      </button>
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
    if (d.online && !was?.online) said = t("设备 {n} 已连接", { n: i + 1 });
    else if (!d.online && was?.online) said = t("设备 {n} 已断开", { n: i + 1 });
  });
  prev.forEach((p, i) => {
    if (!next.some((d) => d.id === p.id)) said = t("设备 {n} 已断开", { n: i + 1 });
  });
  return said;
}

export function cloudPresenceNote(prev: CloudDevice[], next: CloudDevice[]): string {
  const connected = next.find((device) => !prev.some((was) => was.id === device.id));
  if (connected) return t("Web Studio 已连接");
  const disconnected = prev.find((device) => !next.some((now) => now.id === device.id));
  return disconnected ? t("Web Studio 已断开") : "";
}

function cloudRemoteNote(status: CloudRemoteStatus | undefined): string {
  if (status?.online) {
    return t("互联网连接需要登录同一账号，内容端到端加密；局域网直连只在可信网络中开启。");
  }
  switch (status?.reason) {
    case "relay_unreachable":
      return t("中转服务暂时无法连接，请检查网络或代理设置后重试。");
    case "relay_refused":
      return t("中转服务拒绝了这台设备的连接，请稍后重试。");
    default:
      return t("登录 Reasonix 账号后，可生成在外网也能使用的连接二维码。");
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

/** The card leads with the account-gated Internet route, then keeps the LAN
 *  listener as the faster, explicitly enabled alternative. */
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

  return (
    <div className="phonecard" role="dialog" aria-label={t("设备访问")}>
      <header>
        <span>
          <b>{t("设备访问")}</b>
          <small>{t("手机扫码即可进入这台电脑的 Studio")}</small>
        </span>
      </header>

      {failure && <p className="pc-err" role="alert">{failure}</p>}

      {st.cloudRemote?.online && (
        <div className="pc-code" data-cloud="">
          {cloudOffer ? (
            <>
              <img src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(cloudOffer.qr)}`} alt={t("互联网连接二维码")} width={148} height={148} />
              <span className="pc-when">{t("手机扫码 · 不在同一网络也能连接")}</span>
              <span className="pc-acts">
                <button data-action="share.cloud-copy" onClick={() => void copy(cloudOffer.url)}>{copied ? t("已复制") : t("复制链接")}</button>
                <i aria-hidden="true" />
                <button data-action="share.cloud-offer" disabled={busy} onClick={() => void newCloudCode()}>{t("刷新二维码")}</button>
              </span>
            </>
          ) : (
            <button className="pc-mint" data-action="share.cloud-offer" disabled={busy} onClick={() => void newCloudCode()}>
              {busy ? t("正在生成二维码…") : t("显示互联网连接二维码")}
            </button>
          )}
        </div>
      )}

      <section>
        <div className="pc-hd">
          <span>
            <b>{t("同一网络直连")}</b>
            <small>{t("速度更快，不经过中转")}</small>
          </span>
          <Switch data-action="share.toggle" on={st.open} busy={busy || noNetwork} label={t("允许局域网访问")} onClick={() => void toggle()} />
        </div>

        {st.open && (
          <div className="pc-code">
            {offer ? (
              <>
                <img src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(offer.qr)}`} alt={t("配对二维码")} width={148} height={148} />
                <span className="pc-when">{t("局域网配对 · {time} 前有效", { time: clock(offer.expires) })}</span>
                <span className="pc-acts">
                  <button data-action="share.copy" onClick={() => void copy(offer.url)}>{copied ? t("已复制") : t("复制链接")}</button>
                  <i aria-hidden="true" />
                  <button data-action="share.offer" disabled={busy} onClick={() => void newCode()}>{t("换一个")}</button>
                </span>
              </>
            ) : (
              <button className="pc-mint" data-action="share.offer" disabled={busy} onClick={() => void newCode()}>
                {t("显示局域网二维码")}
              </button>
            )}
          </div>
        )}
      </section>

      {st.cloudDevices.length > 0 && (
        <section>
          <div className="pc-hd">
            <b>{t("互联网远程")}</b>
            <small>{st.cloudDevices.length}</small>
          </div>
          {st.cloudDevices.map((device) => (
            <div className="pc-dev" key={device.id} data-online="">
              <i aria-hidden="true" />
              <span>{t("设备 {n}", { n: device.ordinal })}</span>
              <small>{device.name} · {t("在线")}</small>
              <button
                data-action={arming === device.id ? "share.revoke" : "share.ask-revoke"}
                data-target={device.id}
                data-armed={arming === device.id ? "" : undefined}
                onClick={() => disconnect(device.id)}
              >
                {arming === device.id ? t("确认断开") : t("断开")}
              </button>
            </div>
          ))}
        </section>
      )}

      {st.addresses.length > 1 && (
        <label className="pc-row">
          <span>{t("网络")}</span>
          <select data-action="share.address" value={chosen} disabled={busy} onChange={(e) => pick(e.target.value)}>
            {st.addresses.map((a) => (
              <option key={a.ip} value={a.ip}>
                {a.kind === "tailnet" ? `${a.ip} · Tailscale` : a.kind === "virtual" ? `${a.ip} · ${t("虚拟网卡")}` : `${a.ip} · ${a.interface}`}
              </option>
            ))}
          </select>
        </label>
      )}

      {st.open && (
        <section>
          <div className="pc-hd">
            <b>{t("已连接")}</b>
            <small>{st.devices.length}</small>
          </div>
          {st.devices.map((d, i) => (
            <div className="pc-dev" key={d.id} data-online={d.online ? "" : undefined}>
              <i aria-hidden="true" />
              <span title={d.name}>{deviceLabel(i)}</span>
              <small>{d.online ? t("在线") : t("最近 {time}", { time: clock(d.lastSeen) })}</small>
              <button
                data-action={arming === d.id ? "share.revoke" : "share.ask-revoke"}
                data-target={d.id}
                data-armed={arming === d.id ? "" : undefined}
                onClick={() => disconnect(d.id)}
              >
                {arming === d.id ? t("确认断开") : t("断开")}
              </button>
            </div>
          ))}
          {st.devices.length === 0 && <p>{t("还没有手机连上来")}</p>}
        </section>
      )}

      <p className="pc-note">
        {cloudRemoteNote(st.cloudRemote)}
      </p>
    </div>
  );
}
