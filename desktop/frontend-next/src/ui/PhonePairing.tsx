import { useCallback, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { host } from "../port/host";
import { useDismiss } from "./dismiss";
import { StudioIcon } from "./StudioIcon";
import { TooltipButton } from "./TooltipButton";

export function PhonePairing() {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [target, setTarget] = useState<URL | null>(null);
  const [error, setError] = useState("");
  const box = useRef<HTMLDivElement>(null);
  useDismiss(open, box, useCallback(() => setOpen(false), []));
  const scan = async () => {
    setBusy(true);
    setError("");
    setTarget(null);
    try {
      const code = await host().scanPairingCode();
      if (!code) return;
      const url = new URL(code);
      const local = new URLSearchParams(url.hash.slice(1)).has("pair");
      const cloud = url.origin === "https://reasonix.io" && url.pathname === "/remote/" && url.searchParams.has("device");
      if (!["http:", "https:"].includes(url.protocol) || url.username || url.password || !(local || cloud)) {
        setError(t("remote.invalid_pairing_code"));
        return;
      }
      setTarget(url);
    } catch (e) {
      setError(e instanceof TypeError ? t("remote.invalid_pairing_code") : reason(e));
    } finally {
      setBusy(false);
    }
  };
  return <div className="phonepop" ref={box}>
    <TooltipButton className="thbtn phone-action" data-action="device.scan-card" aria-expanded={open}
      aria-label={t("remote.scan_connect_computer")} title={t("remote.scan_connect_computer")} onClick={() => setOpen(v => !v)}>
      <StudioIcon name="link" />
    </TooltipButton>
    {open && <div className="phonecard phone-pairing" role="dialog" aria-label={t("remote.scan_connect_computer")}>
      <header><span><b>{t("remote.scan_connect_computer")}</b><small>{t("remote.scan_computer_hint")}</small></span></header>
      {error && <p className="pc-err" role="alert">{error}</p>}
      {target && <section className="pc-route"><b>{target.origin}</b><p className="pc-note">{t("remote.confirm_scanned_computer")}</p>
        <button className="btn" data-primary data-action="device.connect-scanned" onClick={() => host().openInApp(target.href)}>{t("remote.connect_scanned_computer")}</button>
      </section>}
      <button className="btn" data-action="device.scan" disabled={busy} onClick={() => void scan()}>{t("remote.open_camera_scan")}</button>
    </div>}
  </div>;
}
