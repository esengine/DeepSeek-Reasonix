import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { ACCOUNT_SIGNIN_DISABLED, reason } from "../i18n/kernel";
import { HttpError, type AgentPort, type MarketPackage, type MarketVote as Vote } from "../port/port";

// How the list reads a package's reception. The share is the registry's
// up / (up + down); with no votes there is no share, which is not 0%.
export function approvalLabel(p: Pick<MarketPackage, "approvalRate" | "upCount" | "downCount">): string {
  if (p.approvalRate == null) return t("ext.no_votes_yet");
  return t("ext.pct_positive_n_votes", { pct: Math.round(p.approvalRate * 100), n: p.upCount + p.downCount });
}

// One vote per account: pressing the lit button again withdraws it. Whether
// this account may vote is the registry's answer, never guessed here.
export function MarketVote({ port, pkg, onSignIn }: { port: AgentPort; pkg: MarketPackage; onSignIn?: () => void }) {
  const [vote, setVote] = useState<Vote | null>(null);
  const [off, setOff] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const buttons = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let live = true;
    setBusy(true);
    setError("");
    port
      .marketMyVote(pkg.slug)
      .then((v) => live && setVote(v))
      .catch((e) => {
        if (!live) return;
        // A paired device cannot spend this machine's account token: show the
        // tally alone rather than buttons that would always refuse.
        if (e instanceof HttpError && e.reason?.code === ACCOUNT_SIGNIN_DISABLED) {
          setOff(true);
          setVote({ signedIn: false, value: 0 });
        } else {
          setError(reason(e));
          if (e instanceof HttpError && e.reason?.code === "market.signed_out") setVote({ signedIn: false, value: 0 });
        }
      })
      .finally(() => live && setBusy(false));
    return () => {
      live = false;
    };
  }, [port, pkg.slug, attempt]);

  const up = vote?.upCount ?? pkg.upCount;
  const down = vote?.downCount ?? pkg.downCount;
  const rate = vote?.approvalRate !== undefined ? vote.approvalRate : pkg.approvalRate;
  const mine = vote?.value ?? 0;
  const signedIn = !!vote?.signedIn;
  const can = signedIn && vote?.canVote !== false;

  const cast = async (value: -1 | 1) => {
    setBusy(true);
    setError("");
    try {
      setVote(await port.voteMarket(pkg.slug, mine === value ? 0 : value));
    } catch (e) {
      setError(reason(e));
      if (e instanceof HttpError && e.reason?.code === "market.signed_out") {
        setVote((current) => ({ ...current, signedIn: false, value: 0 }));
      }
    } finally {
      setBusy(false);
    }
  };

  const note = !vote
    ? ""
    : off
      ? t("ext.voting_is_not_available_on")
      : !signedIn
        ? ""
        : vote.own
          ? t("ext.you_cannot_vote_on_a")
          : vote.emailVerified === false
            ? t("ext.verify_your_account_email_to")
            : "";

  return (
    <div className="mkt-vote" data-mine={mine} aria-busy={busy}>
      {rate != null && <span className="mkt-vote-bar" aria-hidden="true"><i style={{ width: `${Math.round(rate * 100)}%` }} /></span>}
      <span className="mkt-vote-rate">{approvalLabel({ approvalRate: rate ?? null, upCount: up, downCount: down })}</span>
      <div className="mkt-vote-btns" role="group" aria-label={t("ext.vote")} ref={buttons} tabIndex={-1}>
        <button
          className="act"
          data-action="market.vote"
          data-value="up"
          aria-pressed={mine === 1}
          disabled={!can || busy}
          title={mine === 1 ? t("ext.withdraw_upvote") : t("ext.upvote")}
          onClick={() => void cast(1)}
        >
          <span aria-hidden="true">▲</span> {t("ext.upvote")} <span className="n">{up}</span>
        </button>
        <button
          className="act"
          data-action="market.vote"
          data-value="down"
          aria-pressed={mine === -1}
          disabled={!can || busy}
          title={mine === -1 ? t("ext.withdraw_downvote") : t("ext.downvote")}
          onClick={() => void cast(-1)}
        >
          <span aria-hidden="true">▼</span> {t("ext.downvote")} <span className="n">{down}</span>
        </button>
      </div>
      {vote && !signedIn && !off && (
        <button className="act mkt-vote-signin" data-action="market.signin" onClick={onSignIn} disabled={!onSignIn}>
          {t("ext.sign_in_to_vote")}
        </button>
      )}
      {note && <span className="note">{note}</span>}
      {error && <span className="why" role="alert">{error}</span>}
      {!vote && (error || attempt > 0) && (
        <button className="act" data-action="market.vote-retry" disabled={busy} onClick={() => { buttons.current?.focus(); setAttempt((n) => n + 1); }}>
          {t(busy ? "common.loading" : "common.retry")}
        </button>
      )}
    </div>
  );
}
