import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { reason } from "../i18n/kernel";
import { t } from "../i18n";
import { hidesAmounts, onHidesAmountsChange } from "../state/prefs";
import type { AgentPort, WalletReading } from "../port/port";

/** The answers to "how much is left" that must not collapse into one number: a
 *  provider with no wallet shows nothing, a wallet that could not be read shows
 *  why, and a value past its freshness says how old it is. A zero is none of
 *  those — it is a wallet that answered and said zero. */
export type Wallet =
  | { kind: "absent" }
  | { kind: "read"; reading: WalletReading }
  | { kind: "unread"; why: string };

export const ABSENT: Wallet = { kind: "absent" };

/** How long ago, in the coarsest unit still true. A wallet moves on the order
 *  of minutes, so seconds would be precision nobody can act on. */
export function since(iso: string, now: number): string {
  const ms = now - Date.parse(iso);
  if (!Number.isFinite(ms)) return "";
  const minutes = Math.floor(Math.max(0, ms) / 60000);
  if (minutes < 1) return t("刚刚");
  if (minutes < 60) return t("{n} 分钟前", { n: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t("{n} 小时前", { n: hours });
  return t("{n} 天前", { n: Math.floor(hours / 24) });
}

/** The account a wallet belongs to. modelRef is "<provider>/<model>", and the
 *  provider half is the name the user gave that connection. */
export const accountOf = (modelRef?: string): string => (modelRef ?? "").split("/")[0] ?? "";

/** What stands in for an amount while this machine hides amounts. */
export const MASK = "•••";

export function useHidesAmounts(): boolean {
  return useSyncExternalStore(onHidesAmountsChange, hidesAmounts, hidesAmounts);
}

/** The wallet only moves when a turn spends, so the caller decides when to read.
 *  It belongs to `source` — the account (connection) that answers it — so a
 *  change of source empties it at once and reads again, and only the latest
 *  read may land. A source that is not named yet is no account: the read made
 *  before the first status already answered for whichever one status then names. */
export function useWallet(port: AgentPort, source?: string): [Wallet, () => void] {
  const [wallet, setWallet] = useState<Wallet>(ABSENT);
  const latest = useRef(0);
  const refresh = useCallback(() => {
    const mine = ++latest.current;
    const land = (w: Wallet) => { if (mine === latest.current) setWallet(w); };
    port
      .balance()
      .then((reading) => land(reading ? { kind: "read", reading } : ABSENT))
      .catch((e) => land({ kind: "unread", why: reason(e) }));
  }, [port]);
  const held = useRef(source);
  useEffect(() => {
    if (held.current && source && held.current !== source) {
      setWallet(ABSENT);
      refresh();
    }
    held.current = source;
  }, [source, refresh]);
  return [wallet, refresh];
}
