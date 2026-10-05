import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { tokens as tokenCount } from "../i18n/format";

/** What this turn has spent and produced. Sent steps when a round bills;
 *  received climbs while the model writes, from the chunks themselves, because
 *  usage only lands when the round closes. An estimate says so — a number the
 *  next event will correct must not read as the measured one. */
export function RunTokens({ sent, received, estimated }: { sent: number; received: number; estimated: boolean }) {
  const up = useLanded(sent);
  const down = useLanded(received);
  if (sent + received <= 0) return null;
  return (
    <span className="studio-runtokens" title={estimated ? t("本轮已发送 / 正在接收（估算）的 token") : t("本轮已发送 / 已接收的 token")}>
      <span data-io="up" data-landed={up ? "" : undefined}>
        <StudioIcon name="arrow" />
        <b>{tokenCount(sent)}</b>
      </span>
      <span data-io="down" data-landed={down ? "" : undefined} data-est={estimated ? "" : undefined}>
        <StudioIcon name="arrow" />
        <b>{tokenCount(received)}</b>
      </span>
      <small>{estimated ? t("tokens ≈") : "tokens"}</small>
    </span>
  );
}

// A number that just moved is worth a glance; one that has been still for a
// while is not. The mark is dropped on a timer rather than on the next change,
// so a stream that stops mid-turn does not leave it lit.
function useLanded(n: number): boolean {
  const [lit, setLit] = useState(false);
  const was = useRef(n);
  useEffect(() => {
    if (was.current === n) return;
    was.current = n;
    setLit(true);
    const off = window.setTimeout(() => setLit(false), 520);
    return () => window.clearTimeout(off);
  }, [n]);
  return lit;
}
