import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { Tariff } from "../port/session";

/** How long until the flip, in the coarsest unit still true, never rounding a
 *  minute away to nothing. */
export function until(iso: string, now: number): string {
  const ms = Date.parse(iso) - now;
  if (!Number.isFinite(ms) || ms <= 0) return "";
  const minutes = Math.max(1, Math.ceil(ms / 60000));
  if (minutes < 60) return t("common.n_min", { n: minutes });
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  if (hours >= 24) return t("composer.n_d", { n: Math.floor(hours / 24) });
  return rest ? t("composer.h_h_m_min", { h: hours, m: rest }) : t("common.n_h", { n: hours });
}

/** Which of the vendor's two rates this model is billed at right now. The kernel
 *  owns the schedule, so this only draws what it said, keeps the countdown
 *  honest, and asks for a fresh answer when the flip time passes. */
export function TariffChip({ tariff, onFlip }: { tariff: Tariff; onFlip: () => void }) {
  const [now, setNow] = useState(() => Date.now());
  const asked = useRef("");
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(timer);
  }, []);
  const due = tariff.changesAt !== undefined && Date.parse(tariff.changesAt) <= now;
  useEffect(() => {
    if (due && tariff.changesAt && asked.current !== tariff.changesAt) {
      asked.current = tariff.changesAt;
      onFlip();
    }
  }, [due, tariff.changesAt, onFlip]);
  if (due) return null;
  const left = tariff.changesAt ? until(tariff.changesAt, now) : "";
  const label = tariff.peak ? t("composer.peak") : t("composer.off_peak");
  const title = tariff.peak
    ? left ? t("composer.billed_at_the_vendors_published_4", { left }) : t("composer.billed_at_the_vendors_published_3")
    : left ? t("composer.billed_at_the_vendors_published_2", { left }) : t("composer.billed_at_the_vendors_published");
  return (
    <span className="studio-meter-tariff" data-peak={tariff.peak ? "" : undefined} title={title}>
      <span>{label}</span>
      {left && <i>{left}</i>}
    </span>
  );
}
