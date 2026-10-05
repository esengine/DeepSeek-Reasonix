import type { ReactNode } from "react";
import { t } from "../../i18n";
import { Spark } from "../Spark";
import { money, tokens } from "../../i18n/format";
import type { Metrics } from "../../state/session";
import type { WalletLine, WalletReading } from "../../port/port";
import { MASK, since, useHidesAmounts, type Wallet } from "../wallet";
import { Grp, Row } from "./kit";

// The kernel states why a total is not final. Repeating one sentence for all of
// them tells a user who has simply not sent anything yet to go and fix a price
// table that is fine — three of these four are not about pricing at all.
const REASON: Record<string, string> = {
  no_usage: "尚未产生用量",
  no_price: "价目未上报",
  mixed_original_currencies: "多币种未合并",
  display_unavailable: "显示币种不可用",
};

const SRC: Record<string, string> = {
  executor: "主循环",
  subagent: "子代理",
  compaction: "压缩",
  planner: "规划",
  classifier: "分类",
  title: "标题",
};

interface Props {
  metrics: Metrics;
  wallet: Wallet;
  account: string;
  onRefreshWallet: () => void;
}

export function Cost({ metrics, wallet, account, onRefreshWallet }: Props) {
  const hidden = useHidesAmounts();
  const shown = (amount: number, currency: string) => (hidden ? MASK : money(amount, currency));
  const sources = Object.entries(metrics.bySource).filter(([, v]) => v > 0);
  const peak = metrics.rounds.length ? Math.max(...metrics.rounds) : 0;
  const avg = metrics.rounds.length ? Math.round(metrics.rounds.reduce((a, b) => a + b, 0) / metrics.rounds.length) : 0;
  return (
    <Grp id="cost" name={t("成本")} aside={t("本会话")}>
      <div className="mtop">
        <span className="amt">{shown(metrics.cost, metrics.currency)}</span>
        {/* 一个按公布价计出来的数和一个拿兑底表估的数是两种断言。 */}
        {sources.length > 0 && (
          <span className="pill" data-tone={metrics.estimated ? "warn" : "ok"}>
            {metrics.estimated ? t("按兜底价估算") : t("已结算")}
          </span>
        )}
        {metrics.turn > 0 && <span className="delta">{t("本回合")}+{shown(metrics.turn, metrics.currency)}</span>}
        {sources.length === 0 && (
          <span className="msub">{t(REASON[metrics.incompleteReason] ?? "价目未上报")}</span>
        )}
      </div>
      {metrics.alt && (
        <div className="mtop" data-alt="">
          <span className="amt">{shown(metrics.alt.amount, metrics.alt.currency)}</span>
          <span className="msub">{t("原币种")}</span>
        </div>
      )}
      {metrics.alt && <p className="mnote">{t("两种结算币种不予合并 —— 合成单一总额需要虚构一个汇率。")}</p>}
      {sources.map(([k, v]) => <Row key={k} k={t(SRC[k] ?? k)} v={shown(v, metrics.currency)} />)}
      <WalletRows wallet={wallet} account={account} onRefresh={onRefreshWallet} />
      {metrics.rounds.length > 1 && (
        <div className="sparkbox">
          <div className="sphd">
            <span>{t("每回合用量")}</span>
            <span className="m">
              {t("峰值 {peak} · 均 {avg}", { peak: tokens(peak), avg: tokens(avg) })}
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
    <button type="button" className="walletk" onClick={onRefresh} title={t("重新读取余额")}>
      {account ? t("钱包 · {name}", { name: account }) : t("钱包")}
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
      {!reading.available && <span className="pill" data-tone="warn">{t("已停用")}</span>}
      {reading.stale && <span className="msub">{since(reading.fetchedAt, Date.now())}</span>}
    </>
  );
  if (hidden) return <Row k={k} v={MASK} />;
  if (lines.length > 1) {
    return (
      <>
        <Row k={k} v={<span className="msub">{t("两种币种不合计")}</span>} />
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
      {line.granted && <span className="msub">{t("含赠金 {amount}", { amount: line.granted })}</span>}
    </>
  );
}
