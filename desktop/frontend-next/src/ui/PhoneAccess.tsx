import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import type { HubPort } from "../port/hub";
import type { CloudShareOffer, ShareOffer, ShareStatus } from "../port/share";
import { copyText } from "./CopyButton";
import { ApplyNote } from "./Group";
import { Switch } from "./Switch";

interface Props {
  hub: HubPort;
  onError: (e: unknown) => void;
}

// While the door is open the list is what changes under the reader: a phone
// pairs, the code on screen is spent. Nothing pushes that, so it is read.
const POLL_MS = 3000;

export function clock(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

// A device is named by when it paired. Its user agent is kept, as the title,
// but read as a name it is a hundred characters and every one starts the same.
export const deviceLabel = (i: number) => t("设备 {n}", { n: i + 1 });

export type Share = ReturnType<typeof useShare>;

/** The window's door for phones, as one state both the settings block and the
 *  chrome's card draw from. `watch` is whether this surface is on screen: only
 *  then is the device list worth reading on a timer. */
export function useShare(hub: HubPort, onError: (e: unknown) => void, watch = true) {
  const [st, setSt] = useState<ShareStatus | null>(null);
  const [ip, setIp] = useState("");
  const [offer, setOffer] = useState<ShareOffer | null>(null);
  const [cloudOffer, setCloudOffer] = useState<CloudShareOffer | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState("");

  const read = useCallback(
    () =>
      hub.shareStatus().then((now) => {
        setSt(now);
        return now;
      }),
    [hub],
  );

  useEffect(() => {
    read().catch(() => setSt(null));
  }, [read]);

  useEffect(() => {
    if (!watch) return;
    void read().catch(() => {});
    const timer = setInterval(() => read().catch(() => {}), POLL_MS);
    return () => clearInterval(timer);
  }, [watch, read]);

  // The kernel withdraws a code once it pairs or lapses; the picture of it has
  // to go too, or the next phone scans a code that can no longer work.
  useEffect(() => {
    if (offer && st && !st.offerExpires) setOffer(null);
  }, [st, offer]);

  useEffect(() => {
    if (cloudOffer && st && !st.cloudRemote?.online) setCloudOffer(null);
  }, [st, cloudOffer]);

  const run = useCallback(
    async (step: () => Promise<void>) => {
      setBusy(true);
      try {
        await step();
      } catch (e) {
        onError(e);
      } finally {
        setBusy(false);
      }
    },
    [onError],
  );

  // The code and the status that knows it is on offer land together; set one
  // at a time, the effect above reads a fresh code against a status that has
  // not heard of it yet and drops it.
  const mint = useCallback(async () => {
    const next = await hub.offerShare();
    const now = await hub.shareStatus();
    setSt(now);
    setOffer(next);
  }, [hub]);

  const newCode = useCallback(() => run(mint), [run, mint]);
  const newCloudCode = useCallback(
    () => run(async () => setCloudOffer(await hub.offerCloudShare())),
    [hub, run],
  );

  const toggle = () => run(async () => {
    if (st?.open) {
      setOffer(null);
      setSt(await hub.closeShare());
      return;
    }
    const at = ip || st?.addresses[0]?.ip || "";
    await hub.openShare(at);
    await mint();
  });

  const revoke = (id: string) => run(async () => setSt(await hub.revokeDevice(id)));

  // Choosing another network while the door is open moves it there: the old
  // address stops answering, so the code and every pairing go with it.
  const pick = (next: string) => {
    setIp(next);
    if (!st?.open) return;
    void run(async () => {
      setOffer(null);
      await hub.openShare(next);
      await mint();
    });
  };

  // A fixed port is read at the next open, so an open door is moved to it the
  // way a network change moves it: the old origin stops answering.
  const savePort = (port: number) =>
    run(async () => {
      const next = await hub.setSharePort(port);
      setSt(next);
      if (!next.open) return;
      const at = ip || next.origin?.replace(/^https?:\/\//, "").replace(/:\d+$/, "") || next.addresses[0]?.ip || "";
      setOffer(null);
      await hub.openShare(at);
      await mint();
    });

  return { st, ip, pick, savePort, offer, cloudOffer, busy, confirm, setConfirm, newCode, newCloudCode, toggle, revoke, refresh: read };
}

/** The switch, the network, the code and the paired phones. */
export function ShareBody({ share }: { share: Share }) {
  const { st, ip, pick, savePort, offer, busy, confirm, setConfirm, newCode, toggle, revoke } = share;
  const [copied, setCopied] = useState<"" | "done" | "failed">("");
  const [portDraft, setPortDraft] = useState<string | null>(null);
  if (!st) return null;
  // The link the code draws, for a device that cannot scan: the same one-time
  // credential, so it is copied on request and never shown as text.
  const copyLink = (url: string) =>
    copyText(url)
      .then(() => setCopied("done"))
      .catch(() => setCopied("failed"))
      .finally(() => window.setTimeout(() => setCopied(""), 1600));
  const chosen = ip || (st.open ? st.origin?.replace(/^https?:\/\//, "").replace(/:\d+$/, "") : "") || st.addresses[0]?.ip || "";
  const noNetwork = st.addresses.length === 0;
  const commitPort = () => {
    if (portDraft === null) return;
    const next = portDraft === "" ? 0 : Number(portDraft);
    setPortDraft(null);
    if (next !== (st.port ?? 0)) void savePort(next);
  };

  return (
    <>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("允许手机访问")}</span>
          <span className="ds">
            {noNetwork
              ? t("这台电脑现在没有局域网地址，连上 Wi-Fi 或网线后再试。")
              : t("手机和这台电脑在同一个网络里，扫码后就能看会话、发消息、批准操作。关闭或退出 Studio 会断开所有手机。")}
          </span>
        </span>
        <Switch data-action="share.toggle" on={st.open} busy={busy || noNetwork} label={t("允许手机访问")} onClick={() => void toggle()} />
      </div>

      {st.addresses.length > 1 && (
        <label className="rmtf">
          <span>{t("网络")}</span>
          <select
            data-action="share.address"
            value={chosen}
            disabled={busy}
            onChange={(e) => pick(e.target.value)}
          >
            {st.addresses.map((a) => (
              <option key={a.ip} value={a.ip}>
                {a.kind === "tailnet" ? `${a.ip} · Tailscale` : a.kind === "virtual" ? `${a.ip} · ${a.interface} · ${t("虚拟网卡")}` : `${a.ip} · ${a.interface}`}
              </option>
            ))}
          </select>
        </label>
      )}
      {st.addresses.length > 1 && st.open && st.devices.length > 0 && (
        <p className="rmthint">{t("换网络会断开已连接的手机，它们需要扫新的二维码。")}</p>
      )}

      <label className="rmtf">
        <span>{t("端口")}</span>
        <input
          data-action="share.port"
          inputMode="numeric"
          maxLength={5}
          value={portDraft ?? (st.port ? String(st.port) : "")}
          placeholder={t("自动")}
          disabled={busy}
          onChange={(e) => setPortDraft(e.target.value.replace(/\D/g, ""))}
          onBlur={commitPort}
        />
      </label>
      <p className="rmthint">
        {t("留空则每次开启时由系统随机选择，范围 1024–65535。更改端口后，手机需要重新扫码配对。")}
      </p>
      {st.open && st.devices.length > 0 && <p className="rmthint">{t("改端口会断开已连接的手机，它们需要扫新的二维码。")}</p>}

      {st.open && (
        <div className="shareopen">
          {offer ? (
            <div className="shareqr">
              <img src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(offer.qr)}`} alt={t("配对二维码")} width={184} height={184} />
              <div className="sharehow">
                <span className="lb">{t("用手机相机扫码")}</span>
                <span className="ds">{t("二维码只能配对一台手机，{time} 前有效。别把它发到群里或截图外传。", { time: clock(offer.expires) })}</span>
                <code dir="ltr">{st.origin}</code>
                <button className="rmtlnk sharecopy" data-action="share.copy" onClick={() => void copyLink(offer.url)}>
                  {copied === "done" ? t("已复制") : copied === "failed" ? t("复制不了") : t("复制配对链接")}
                </button>
              </div>
            </div>
          ) : (
            <button className="rmtadd" data-action="share.offer" disabled={busy} onClick={() => void newCode()}>
              {t("显示配对二维码")}
            </button>
          )}

          <div className="sharedevs">
            {st.devices.map((d, i) => (
              <div className="sharedev" key={d.id}>
                <span className="nm" title={d.name}>{deviceLabel(i)}</span>
                <span className="st">{d.online ? t("{time} 配对 · 在线", { time: clock(d.pairedAt) }) : t("{time} 配对 · 最近 {seen}", { time: clock(d.pairedAt), seen: clock(d.lastSeen) })}</span>
                {confirm === d.id ? (
                  <span className="rmtconfirm" role="alertdialog">
                    <span>{t("断开这台手机？它需要重新扫码才能再连上。")}</span>
                    <button data-action="share.keep" data-target={d.id} onClick={() => setConfirm("")}>{t("取消")}</button>
                    <button
                      data-danger=""
                      data-action="share.revoke"
                      data-target={d.id}
                      autoFocus
                      onClick={() => {
                        setConfirm("");
                        void revoke(d.id);
                      }}
                    >
                      {t("断开")}
                    </button>
                  </span>
                ) : (
                  <button className="rmtlnk" data-danger="" data-action="share.ask-revoke" data-target={d.id} onClick={() => setConfirm(d.id)}>
                    {t("断开")}
                  </button>
                )}
              </div>
            ))}
            {st.devices.length === 0 && <p className="rmtempty">{t("还没有手机连上来。")}</p>}
          </div>

          <p className="sharewarn">
            {t("连接走的是局域网里的明文 HTTP：同一个网络里的其他人能看到传输内容。在自己家里可以用，咖啡馆、机场这类公共网络不要开。Tailscale 地址的流量本身是加密的。")}
          </p>
        </div>
      )}
    </>
  );
}

/** A phone on the same network drives this window's panes. Nothing here is
 *  kept: the door shuts with the window, and shutting it unpairs every phone. */
export function PhoneAccess({ hub, onError }: Props) {
  const share = useShare(hub, onError);
  // A settings page must not grow a heading for a door this kernel cannot open.
  if (!share.st) return null;
  return (
    <section className="grp" id="set-phone" data-setting="phone">
      <div className="grp-hd">
        <h3>{t("手机访问")}</h3>
      </div>
      <div className="grp-items share">
        <ShareBody share={share} />
      </div>
      <ApplyNote id="phone" />
    </section>
  );
}
