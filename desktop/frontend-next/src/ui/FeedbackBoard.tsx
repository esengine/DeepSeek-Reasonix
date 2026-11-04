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
    <section className="fbk-board" aria-label={t("feedback.contribution_board")}>
      <div className="fbk-link-hd">
        <span className="fbk-link-name">{t("feedback.contribution_board")}</span>
        <span className="fbk-hint">{t("feedback.for_people_whose_feedback_was")}</span>
      </div>
      <label className="fbk-board-pick">
        <span className="fbk-hint">{t("common.version")}</span>
        <select aria-label={t("common.version")} data-action-change="feedback.board-release" value={release} onChange={(e) => void pick(e.target.value)}>
          <option value="">{t("feedback.all_releases")}</option>
          {versions.map((v) => <option key={v} value={v}>{v}</option>)}
        </select>
      </label>
      {board.entries.length === 0 ? (
        <p className="fbk-hint">{t("feedback.nobody_is_listed_for_this")}</p>
      ) : (
        <ol className="fbk-board-list" data-ranked={ranked ? "" : undefined}>
          {board.entries.map((e) => (
            <li key={e.displayName}>
              {ranked && <span className="fbk-board-rank" aria-label={t("feedback.place_n", { n: e.rank ?? 0 })}>{e.rank}</span>}
              <span className="fbk-board-name">{e.displayName}</span>
              <span className="fbk-board-credits">{t("feedback.n_adopted", { n: e.credits })}</span>
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
      <p className="fbk-hint">{t("feedback.a_ranking_depends_on_what")}</p>

      {signedIn && linked && offered && !me?.listed && !me?.delisted && (
        <div className="fbk-board-offer" role="group" aria-label={t("feedback.join_the_contribution_board")}>
          <span className="fbk-link-name">{t("feedback.join_the_contribution_board_2")}</span>
          <p className="fbk-hint">{t("feedback.this_computer_is_linked_to")}</p>
          <div className="fbk-link-row">
            <input className="fbk-board-name-field" data-action-change="feedback.board-name" aria-label={t("feedback.name_shown_on_the_board")} value={offerName} maxLength={40} placeholder={t("feedback.name_shown_on_the_board")} onChange={(e) => setOfferName(e.target.value)} />
            <button type="button" className="btn sm" data-primary data-action="feedback.board-join" disabled={busy || boardNameProblem(offerName) !== null} onClick={() => { markBoardOfferSeen(); setOffered(false); void run(() => port.joinFeedbackBoard(offerName.trim())); }}>{t("feedback.join_the_board")}</button>
            <button type="button" className="btn sm" data-action="feedback.board-offer-dismiss" disabled={busy} onClick={() => { markBoardOfferSeen(); setOffered(false); }}>{t("common.no_thanks")}</button>
          </div>
        </div>
      )}
      <div className="fbk-board-me">
        <span className="fbk-link-name">{t("feedback.my_place_on_the_board")}</span>
        {!signedIn ? (
          <p className="fbk-hint">{t("feedback.sign_in_from_settings_first")}</p>
        ) : me?.delisted ? (
          <p className="fbk-hint" data-tone="warn">{t("feedback.a_maintainer_removed_this_account")}</p>
        ) : me?.listed ? (
          <div className="fbk-link-row">
            <span className="fbk-chip" data-status="recorded">{t("feedback.shown_on_the_board_as")} · {me.displayName}</span>
            <button type="button" className="btn sm" data-action="feedback.board-leave" disabled={busy} onClick={() => void run(() => port.leaveFeedbackBoard())}>{t("feedback.leave_the_board")}</button>
          </div>
        ) : (
          <>
            <p className="fbk-hint">{t("feedback.once_you_join_the_board")}</p>
            <div className="fbk-link-row">
              <input className="fbk-board-name-field" data-action-change="feedback.board-name" aria-label={t("feedback.name_shown_on_the_board")} value={draft} maxLength={30} placeholder={t("feedback.name_shown_on_the_board")} onChange={(e) => setDraft(e.target.value)} />
              <button type="button" className="btn sm" data-primary data-action="feedback.board-join" disabled={busy || typed.length < 2} onClick={() => void run(() => port.joinFeedbackBoard(typed))}>{t("feedback.join_the_board")}</button>
            </div>
            {typed.length >= 2 && <p className="fbk-hint" aria-live="polite">{t("feedback.it_will_be_shown_on", { name: typed })}</p>}
          </>
        )}
      </div>
      {problem && <p className="fbk-hint" role="alert" data-tone="error">{problem}</p>}
    </section>
  );
}
