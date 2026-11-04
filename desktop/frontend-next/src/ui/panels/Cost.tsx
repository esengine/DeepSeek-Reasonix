import type { ReactNode } from "react";
import { t } from "../../i18n";
import { Spark } from "../Spark";
import { money, moneyList, tokens } from "../../i18n/format";
import type { CurrencyAmount, Metrics } from "../../state/session";
import { DEFAULT_CURRENCY } from "../../state/usage";
import type { WalletLine, WalletReading } from "../../port/port";
import { MASK, since, useHidesAmounts, type Wallet } from "../wallet";
import { Grp, Row } from "./kit";

// The kernel states why a total is not final. Repeating one sentence for all of
// them tells a user who has simply not sent anything yet to go and fix a price
// table that is fine — three of these four are not about pricing at all.
const REASON: Record<string, string> = {
  no_usage: "usage.no_usage_yet",
  no_price: "usage.no_pricing_reported",
  mixed_original_currencies: "usage.currencies_not_combined",
  display_unavailable: "usage.display_currency_unavailable",
};

const SRC: Record<string, string> = {
  executor: "usage.main_loop",
  subagent: "common.subagents",
  compaction: "session.compaction",
  planner: "session.planning",
  classifier: "chat.classifying",
  title: "memory.titling",
};

interface Props {
  metrics: Metrics;
  wallet: Wallet;
  account: string;
  onRefreshWallet: () => void;
}

// A session that has not been billed yet still draws its zero, as it always
// has. The default labels that zero and nothing else: a billed round always
// brings the code it was charged in.
const NOTHING: CurrencyAmount[] = [{ currency: DEFAULT_CURRENCY, amount: 0 }];

export function Cost({ metrics, wallet, account, onRefreshWallet }: Props) {
  const hidden = useHidesAmounts();
  const shown = (amount: number, currency: string) => (hidden ? MASK : money(amount, currency));
  // One amount per currency, never a sum of two: the usage page prints its
  // report the same way.
  const amounts = (list: CurrencyAmount[]) => (hidden ? MASK : moneyList(list));
  const totals = metrics.cost.length ? metrics.cost : NOTHING;
  const sources = Object.entries(metrics.bySource).filter(([, list]) => list.some((m) => m.amount > 0));
  const peak = metrics.rounds.length ? Math.max(...metrics.rounds) : 0;
  const avg = metrics.rounds.length ? Math.round(metrics.rounds.reduce((a, b) => a + b, 0) / metrics.rounds.length) : 0;
  return (
    <Grp id="cost" name={t("usage.cost")} aside={t("common.this_session_2")}>
      <div className="mtop">
        {hidden
          ? <span className="amt">{MASK}</span>
          : totals.map((m) => <span className="amt" key={m.currency}>{money(m.amount, m.currency)}</span>)}
        {/* 一个按公布价计出来的数和一个拿兑底表估的数是两种断言。 */}
        {sources.length > 0 && (
          <span className="pill" data-tone={metrics.estimated ? "warn" : "ok"}>
            {metrics.estimated ? t("usage.fallback_price") : t("usage.settled")}
          </span>
        )}
        {metrics.turn.some((m) => m.amount > 0) && <span className="delta">{t("usage.this_turn")}+{amounts(metrics.turn)}</span>}
        {sources.length === 0 && (
          <span className="msub">{t(REASON[metrics.incompleteReason] ?? "usage.no_pricing_reported")}</span>
        )}
      </div>
      {metrics.alt && (
        <div className="mtop" data-alt="">
          <span className="amt">{shown(metrics.alt.amount, metrics.alt.currency)}</span>
          <span className="msub">{t("usage.as_quoted")}</span>
        </div>
      )}
      {metrics.alt && <p className="mnote">{t("usage.two_settlement_currencies_do_not")}</p>}
      {sources.map(([k, list]) => <Row key={k} k={t(SRC[k] ?? k)} v={amounts(list)} />)}
      <WalletRows wallet={wallet} account={account} onRefresh={onRefreshWallet} />
      {metrics.rounds.length > 1 && (
        <div className="sparkbox">
          <div className="sphd">
            <span>{t("usage.tokens_per_round")}</span>
            <span className="m">
              {t("usage.peak_peak_avg_avg", { peak: tokens(peak), avg: tokens(avg) })}
            </span>
          </div>
          <Spark points={metrics.rounds} h={30} />
        </div>
      )}
    </Grp>
  );
}

// The wallet sits under the session's costs because it answers the same
// question, and carries the account's name because it does not answer it about
// the same thing: what a run spent is this session's, what is left is the
// account's. Absent renders as nothing at all — most providers have no wallet
// endpoint, and a dash there reads as a number that failed to load.
function WalletRows({ wallet, account, onRefresh }: { wallet: Wallet; account: string; onRefresh: () => void }) {
  if (wallet.kind === "absent") return null;
  const name = (
    <button type="button" className="walletk" onClick={onRefresh} title={t("usage.read_the_balance_again")}>
      {account ? t("usage.wallet_name", { name: account }) : t("usage.wallet")}
    </button>
  );
  return (
    <div className="wallet">
      {wallet.kind === "unread" ? (
        <>
          {/* 读不到的原因是一句话,不是一个读数 —— 挤进值那一列会被压到折行。 */}
          <Row k={name} v="—" tone="warn" />
          <p className="mnote">{wallet.why}</p>
        </>
      ) : (
        <WalletRead reading={wallet.reading} name={name} />
      )}
    </div>
  );
}

function WalletRead({ reading, name }: { reading: WalletReading; name: ReactNode }) {
  const hidden = useHidesAmounts();
  const lines = reading.lines ?? [];
  const k = (
    <>
      {name}
      {/* 供应商说这个账户已经不能发请求了 —— 比余额数字本身重要。 */}
      {!reading.available && <span className="pill" data-tone="warn">{t("common.suspended")}</span>}
      {reading.stale && <span className="msub">{since(reading.fetchedAt, Date.now())}</span>}
    </>
  );
  if (hidden) return <Row k={k} v={MASK} />;
  if (lines.length > 1) {
    return (
      <>
        <Row k={k} v={<span className="msub">{t("usage.two_currencies_not_summed")}</span>} />
        {lines.map((l) => <Row key={l.currency} k={l.currency} v={<Amount line={l} />} />)}
      </>
    );
  }
  return <Row k={k} v={lines[0] ? <Amount line={lines[0]} /> : reading.display} />;
}

function Amount({ line }: { line: WalletLine }) {
  return (
    <>
      {line.total}
      {/* 赠金会过期,所以它不是余额的一部分那么简单。 */}
      {line.granted && <span className="msub">{t("usage.incl_amount_credit", { amount: line.granted })}</span>}
    </>
  );
}
