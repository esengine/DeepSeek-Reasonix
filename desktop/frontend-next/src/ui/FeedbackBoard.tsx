import { useCallback, useEffect, useMemo, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { FeedbackBoard as Board, FeedbackBoardMe } from "../port/feedback";
import type { AgentPort } from "../port/port";
import { boardNameProblem, boardOfferSeen, boardPrefill, markBoardOfferSeen } from "./boardname";

const newest = (a: string, b: string): number => {
  const pa = a.replace(/^v/, "").split(".").map(Number);
  const pb = b.replace(/^v/, "").split(".").map(Number);
  for (let i = 0; i < 3; i++) if ((pa[i] ?? 0) !== (pb[i] ?? 0)) return (pb[i] ?? 0) - (pa[i] ?? 0);
  return 0;
};

const failure = (e: unknown): string => reason(e);

// The public board of people whose suggestions shipped, and the person's own switch
// for being on it. Hidden where the service serves no board. The name is the only
// thing shown, and being listed is always the person's own, separate choice.
export function FeedbackBoard({ port, reload = 0 }: { port: AgentPort; reload?: number }) {
  const [all, setAll] = useState<Board | null>(null);
  const [board, setBoard] = useState<Board | null>(null);
  const [release, setRelease] = useState("");
  const [signedIn, setSignedIn] = useState(false);
  const [me, setMe] = useState<FeedbackBoardMe | null>(null);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState("");
  const [linked, setLinked] = useState(false);
  const [offered, setOffered] = useState(() => !boardOfferSeen());
  const [offerName, setOfferName] = useState("");

  const load = useCallback(async () => {
    try {
      const [first, account] = await Promise.all([port.feedbackBoard(), port.account().catch(() => null)]);
      setAll(first);
      setBoard(first);
      const on = !!account && account.signedIn && !account.expired;
      setSignedIn(on);
      if (!on) {
        setLinked(false);
        return;
      }
      const [mine, links, env] = await Promise.all([
        port.feedbackBoardMe().catch(() => null),
        port.feedbackLinks().catch(() => []),
        port.feedbackEnv(document.documentElement.lang).catch(() => null),
      ]);
      setMe(mine);
      setLinked(links.some((l) => l.current));
      if (env) setOfferName((prev) => prev || boardPrefill(env.displayName));
    } catch {
      setAll(null);
    }
  }, [port]);
  useEffect(() => { void load(); }, [load, reload]);

  const versions = useMemo(
    () => [...new Set((all?.entries ?? []).flatMap((e) => e.releases.map((r) => r.version)))].sort(newest),
    [all],
  );

  const pick = async (next: string) => {
    setRelease(next);
    try {
      setBoard(await port.feedbackBoard(next || undefined));
    } catch (e) {
      setProblem(failure(e));
    }
  };

  const run = async (act: () => Promise<FeedbackBoardMe>) => {
    setBusy(true);
    setProblem("");
    try {
      setMe(await act());
      setAll(await port.feedbackBoard());
      setBoard(await port.feedbackBoard(release || undefined));
    } catch (e) {
      setProblem(failure(e));
    } finally {
      setBusy(false);
    }
  };

  if (!all?.available || !board) return null;
  const ranked = board.view === "release";
  const typed = draft.trim();

  return (
    <section className="fbk-board" aria-label={t("feedbackBoard.contributionBoard")}>
      <div className="fbk-link-hd">
        <span className="fbk-link-name">{t("feedbackBoard.contributionBoard")}</span>
        <span className="fbk-hint">{t("feedbackBoard.forPeopleWhoseFeedbackWas")}</span>
      </div>
      <label className="fbk-board-pick">
        <span className="fbk-hint">{t("common.version")}</span>
        <select aria-label={t("common.version")} data-action-change="feedback.board-release" value={release} onChange={(e) => void pick(e.target.value)}>
          <option value="">{t("feedbackBoard.allReleases")}</option>
          {versions.map((v) => <option key={v} value={v}>{v}</option>)}
        </select>
      </label>
      {board.entries.length === 0 ? (
        <p className="fbk-hint">{t("feedbackBoard.nobodyIsListedForThis")}</p>
      ) : (
        <ol className="fbk-board-list" data-ranked={ranked ? "" : undefined}>
          {board.entries.map((e) => (
            <li key={e.displayName}>
              {ranked && <span className="fbk-board-rank" aria-label={t("feedbackBoard.place", { n: e.rank ?? 0 })}>{e.rank}</span>}
              <span className="fbk-board-name">{e.displayName}</span>
              <span className="fbk-board-credits">{t("feedbackBoard.adopted", { n: e.credits })}</span>
              {!ranked && (
                <span className="fbk-board-where">
                  {e.releases.slice(0, 3).map((r) => <span key={r.version} className="fbk-chip" data-status="recorded">{r.version}{r.count > 1 ? ` ×${r.count}` : ""}</span>)}
                  {e.releases.length > 3 && <span className="fbk-meta">+{e.releases.length - 3}</span>}
                </span>
              )}
            </li>
          ))}
        </ol>
      )}
      <p className="fbk-hint">{t("feedbackBoard.aRankingDependsOnWhat")}</p>

      {signedIn && linked && offered && !me?.listed && !me?.delisted && (
        <div className="fbk-board-offer" role="group" aria-label={t("feedbackBoard.joinTheContributionBoard")}>
          <span className="fbk-link-name">{t("feedbackBoard.joinTheContributionBoard2")}</span>
          <p className="fbk-hint">{t("feedbackBoard.thisComputerIsLinkedTo")}</p>
          <div className="fbk-link-row">
            <input className="fbk-board-name-field" data-action-change="feedback.board-name" aria-label={t("common.nameShownOnTheBoard")} value={offerName} maxLength={40} placeholder={t("common.nameShownOnTheBoard")} onChange={(e) => setOfferName(e.target.value)} />
            <button type="button" className="btn sm" data-primary data-action="feedback.board-join" disabled={busy || boardNameProblem(offerName) !== null} onClick={() => { markBoardOfferSeen(); setOffered(false); void run(() => port.joinFeedbackBoard(offerName.trim())); }}>{t("common.joinTheBoard")}</button>
            <button type="button" className="btn sm" data-action="feedback.board-offer-dismiss" disabled={busy} onClick={() => { markBoardOfferSeen(); setOffered(false); }}>{t("common.noThanks")}</button>
          </div>
        </div>
      )}
      <div className="fbk-board-me">
        <span className="fbk-link-name">{t("feedbackBoard.myPlaceOnTheBoard")}</span>
        {!signedIn ? (
          <p className="fbk-hint">{t("feedbackBoard.signInFromSettingsFirst")}</p>
        ) : me?.delisted ? (
          <p className="fbk-hint" data-tone="warn">{t("feedbackBoard.aMaintainerRemovedThisAccount")}</p>
        ) : me?.listed ? (
          <div className="fbk-link-row">
            <span className="fbk-chip" data-status="recorded">{t("feedbackBoard.shownOnTheBoardAs")} · {me.displayName}</span>
            <button type="button" className="btn sm" data-action="feedback.board-leave" disabled={busy} onClick={() => void run(() => port.leaveFeedbackBoard())}>{t("feedbackBoard.leaveTheBoard")}</button>
          </div>
        ) : (
          <>
            <p className="fbk-hint">{t("feedbackBoard.onceYouJoinTheBoard")}</p>
            <div className="fbk-link-row">
              <input className="fbk-board-name-field" data-action-change="feedback.board-name" aria-label={t("common.nameShownOnTheBoard")} value={draft} maxLength={30} placeholder={t("common.nameShownOnTheBoard")} onChange={(e) => setDraft(e.target.value)} />
              <button type="button" className="btn sm" data-primary data-action="feedback.board-join" disabled={busy || typed.length < 2} onClick={() => void run(() => port.joinFeedbackBoard(typed))}>{t("common.joinTheBoard")}</button>
            </div>
            {typed.length >= 2 && <p className="fbk-hint" aria-live="polite">{t("common.itWillBeShownOn", { name: typed })}</p>}
          </>
        )}
      </div>
      {problem && <p className="fbk-hint" role="alert" data-tone="error">{problem}</p>}
    </section>
  );
}
