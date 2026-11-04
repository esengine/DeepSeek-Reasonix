import { useCallback, useEffect, useState } from "react";
import { current, t } from "../i18n";
import type { FeedbackLinkedInstall } from "../port/feedback";
import type { AgentPort } from "../port/port";
import { reason } from "../i18n/kernel";
import { boardNameProblem, boardPrefill, markBoardOfferSeen } from "./boardname";
import { FeedbackBoardJoin } from "./FeedbackBoardJoin";
import { linkFailure } from "./feedbackfailure";

interface Props {
  port: AgentPort;
  // Linking names this install, which only exists once a report has been sent.
  hasReport: boolean;
  // The board changed under the page: the link step may have listed the person.
  onBoardChanged?: () => void;
}

const day = (iso: string): string => {
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? "" : at.toLocaleDateString(current() === "zh" ? "zh-CN" : "en", { year: "numeric", month: "short", day: "numeric" });
};

// The account link is the person's own call: nothing here runs at sign-in, and
// every control is inert until an account is signed in.
export function FeedbackLink({ port, hasReport, onBoardChanged }: Props) {
  const [signedIn, setSignedIn] = useState<boolean | null>(null);
  const [links, setLinks] = useState<FeedbackLinkedInstall[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [asking, setAsking] = useState(false);
  const [join, setJoin] = useState(true);
  const [name, setName] = useState("");

  const load = useCallback(async () => {
    try {
      const state = await port.account();
      setSignedIn(state.signedIn && !state.expired);
      if (!state.signedIn || state.expired) {
        setLinks(null);
        return;
      }
      setLinks(await port.feedbackLinks());
      setProblem(null);
    } catch (e) {
      setProblem(linkFailure(e).message);
    }
  }, [port]);

  useEffect(() => {
    void load();
  }, [load]);

  const run = async (act: () => Promise<void>) => {
    setBusy(true);
    setProblem(null);
    try {
      await act();
      setLinks(await port.feedbackLinks());
    } catch (e) {
      setProblem(linkFailure(e).message);
    } finally {
      setBusy(false);
    }
  };

  const ask = () => {
    setAsking(true);
    setJoin(true);
    setProblem(null);
    port.feedbackEnv(document.documentElement.lang).then((e) => setName((prev) => prev || boardPrefill(e.displayName)), () => {});
  };

  // Linking happens on confirm; the board name rides along only when it is
  // ticked and valid, and a refusal of the name leaves the link in place.
  const confirm = async () => {
    setBusy(true);
    setProblem(null);
    try {
      await port.linkFeedbackAccount();
    } catch (e) {
      setProblem(linkFailure(e).message);
      setBusy(false);
      return;
    }
    markBoardOfferSeen();
    setAsking(false);
    try {
      setLinks(await port.feedbackLinks());
      if (join && boardNameProblem(name) === null) await port.joinFeedbackBoard(name.trim());
    } catch (e) {
      setProblem(join ? t("feedback.linked_but_joining_the_board", { why: reason(e) }) : linkFailure(e).message);
    } finally {
      setBusy(false);
      onBoardChanged?.();
    }
  };

  if (signedIn === null) return null;
  const here = links?.find((l) => l.current) ?? null;
  const others = (links ?? []).filter((l) => !l.current);
  const canLink = signedIn && hasReport && !busy;

  return (
    <section className="fbk-link" aria-label={t("feedback.account_link")}>
      <div className="fbk-link-hd">
        <span className="fbk-link-name">{t("feedback.account_link")}</span>
        <span className="fbk-hint">{t("feedback.once_linked_this_computers_feedback")}</span>
      </div>
      {!signedIn && <p className="fbk-hint">{t("feedback.sign_in_to_your_account")}</p>}
      {signedIn && !hasReport && !here && <p className="fbk-hint">{t("feedback.you_can_link_this_computer")}</p>}
      <div className="fbk-link-row">
        {here ? (
          <>
            <span className="fbk-chip" data-status="recorded">{t("feedback.this_computer_is_linked")} · {day(here.linkedAt)}</span>
            <button type="button" className="btn sm" data-action="feedback.account-unlink" disabled={busy} onClick={() => void run(() => port.unlinkFeedbackAccount())}>
              {t("feedback.unlink_2")}
            </button>
          </>
        ) : (
          <button type="button" className="btn sm" data-action="feedback.account-link" disabled={!canLink || asking} onClick={ask}>
            {t("feedback.link_this_computer_to_my")}
          </button>
        )}
      </div>
      {asking && !here && (
        <div className="fbk-link-confirm" role="group" aria-label={t("feedback.confirm_linking")}>
          <FeedbackBoardJoin checked={join} name={name} disabled={busy} onChecked={setJoin} onName={setName} />
          <div className="fbk-link-row">
            <button type="button" className="btn sm" data-primary data-action="feedback.account-link-confirm" disabled={busy || (join && boardNameProblem(name) !== null)} onClick={() => void confirm()}>
              {t("common.confirm")}
            </button>
            <button type="button" className="btn sm" data-action="feedback.account-link-cancel" disabled={busy} onClick={() => setAsking(false)}>
              {t("common.cancel")}
            </button>
          </div>
        </div>
      )}
      {others.length > 0 && (
        <ul className="fbk-link-list" aria-label={t("feedback.other_computers_on_this_account")}>
          {others.map((l) => (
            <li key={l.id}>
              <code className="fbk-code">{l.id}</code>
              <span className="fbk-meta">{day(l.linkedAt)}</span>
              <button type="button" className="btn sm" data-action="feedback.account-unlink-other" data-target={l.id} disabled={busy} onClick={() => void run(() => port.unlinkFeedbackInstall(l.id))}>
                {t("feedback.unlink")}
              </button>
            </li>
          ))}
        </ul>
      )}
      {problem && <p className="fbk-hint" role="alert" data-tone="error">{problem}</p>}
    </section>
  );
}
