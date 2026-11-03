import "../../styles/compaction-live.css";
import { useState } from "react";
import type { Compaction } from "../../port/wire";
import { useStartsOpen } from "../../state/foldpref";
import { Sym } from "../Sym";
import { t } from "../../i18n";
import { FOLD_WHY } from "../../i18n/compaction_why";
import { tokens } from "../../i18n/format";
import { tx } from "../../i18n/rich";

// What sent this fold. The host knows which of the two thresholds was reached
// and how big it is; a card that says only "a threshold" leaves a fold at 16%
// of a declared window looking like the host acting on its own.
//
// The literals live inside t() rather than in a table, which a module body
// would freeze in the source language and a catalogue scanner would never see.
function why(c: Compaction): string {
  if (c.trigger === "manual") return t("compactionCard.triggeredByHand");
  if (c.trigger !== "auto") return c.trigger ?? "";
  if (!c.triggerTokens) return t("compactionCard.automaticAContextThresholdWas");
  const n = tokens(c.triggerTokens);
  if (c.boundary === "capacity") return t("compactionCard.automaticInputReachedTheWindow", { n });
  return t("compactionCard.automaticInputReached", { n });
}

// A digest reads as complete whatever it dropped, so the count of the fold's
// changes it actually carried is the one thing this card can say that the
// summary text cannot. It is stated whether or not anything is missing: a
// number that only appears on failure is a number nobody learns to read.
function Coverage({ c }: { c: Compaction }) {
  const required = c.coverageRequired ?? 0;
  if (required === 0) return null;
  const missing = c.coverageMissing ?? 0;
  const kept = required - missing;
  return (
    <div className="comp-l">
      <div className="row">
        <span className="k">✓</span>
        <span>
          {missing === 0 ? (
            <>{tx("compactionCard.allChangesMadeHereAre", { n: <b>{required}</b> })}</>
          ) : (
            <>{tx("compactionCard.changesAreInTheDigest", { kept: <b>{kept}</b>, required })}</>
          )}
          {c.coverageBackstopped && t("compactionCard.gapsKeptByTheHost")}
        </span>
      </div>
      {missing > 0 && (
        <div className="row">
          <span className="x">×</span>
          <span>
            {tx("compactionCard.moreAreIndexEntriesOnly", { n: <b>{missing}</b> })}
          </span>
        </div>
      )}
    </div>
  );
}

const PART: Record<string, string> = {
  system: "common.systemPrompt",
  tools: "common.toolDefinitions",
  digest: "common.summary",
  user: "common.userMessages",
  reply: "common.modelReplies",
  tool_output: "common.toolOutput",
};

// What a fold left behind, when that is still a large share of the window. The
// card says what the rest is made of — the host knows, and a person looking at
// "607k to 316k" otherwise cannot tell whether the remainder is normal.
function Remains({ c }: { c: Compaction }) {
  const r = c.remains;
  if (!r || r.total <= 0 || r.parts.length === 0) return null;
  const top = r.parts.filter((p) => p.tokens > 0).slice(0, 3).map((p) => `${t(PART[p.name] ?? p.name)} ${tokens(p.tokens)}`).join("、");
  const big = r.largest?.[0];
  return (
    <div className="comp-l">
      <div className="row">
        <span className="k">·</span>
        <span>
          {tx("compactionCard.stillHeldAfterTheFold", { n: <b>{tokens(r.total)}</b>, top })}
          {big && big.tokens > 0 && <>{" · "}{big.tool ? t("compactionCard.theLargestIsAResult", { tool: big.tool, n: tokens(big.tokens) }) : t("compactionCard.theLargestIsAMessage", { role: t(PART[big.role === "assistant" ? "reply" : big.role === "tool" ? "tool_output" : "user"] ?? big.role), n: tokens(big.tokens) })}</>}
        </span>
      </div>
      {r.active > 0 && (
        <div className="row">
          <span className="k">·</span>
          <span>{t("common.theTurnInProgressHolds", { n: tokens(r.active) })}</span>
        </div>
      )}
    </div>
  );
}

// The tail of what has been written, kept to one line: a digest is thousands
// of tokens of headings and lists, and growing the card by all of them would
// move everything below it for the length of the fold.
function lastLine(text: string): string {
  const line = text.trimEnd().split("\n").at(-1)?.trim() ?? "";
  return line.length > 96 ? "…" + line.slice(-96) : line;
}

export function CompactionCard({ c, done }: { c: Compaction; done: boolean }) {
  const before = c.sourceTokens ?? 0;
  const after = c.projectionTokens ?? 0;
  const shrank = before > 0 && after > 0 && after < before;
  const aborted = done && !c.messages;
  const cause = aborted && c.code ? FOLD_WHY[c.code] : undefined;
  return (
    <div className="call" data-k="host">
      <div className="g">
        <Sym glyph="⊘" />
      </div>
      <div className="c">
        <div className="hl">
          <span className={done ? "nm" : "nm shim"}>{t(!done ? "common.compacting" : aborted ? "compactionCard.compactionDidNotComplete" : "compactionCard.compactionDone")}</span>
          <span className="tag">{t("common.host")}</span>
          {c.trigger && <span className="arg" title={why(c)}>{why(c)}</span>}
        </div>
        {/* While it runs, the digest's own last line is the progress bar: a
            fold can take a minute, and a placeholder that says nothing for a
            minute cannot be told apart from one that has hung. */}
        {!done && (
          <div className="out">
            <div className="comp">
              {/* Before the first word of the digest arrives there is still
                  something true to say: how much is being folded. Without it
                  the card sits blank for the length of a model call. */}
              {(c.messages || before > 0) && (
                <div className="comp-n">
                  {c.messages ? (
                    <>{tx("compactionCard.foldingMessagesIntoADigest", { n: <b>{c.messages}</b> })}</>
                  ) : (
                    t("compactionCard.foldingIntoADigest")
                  )}
                  {before > 0 && (
                    <>
                      {" · "}
                      <b>{tokens(before)}</b>
                    </>
                  )}
                </div>
              )}
              <div className="comp-n comp-live">{c.summary ? lastLine(c.summary) : " "}</div>
            </div>
          </div>
        )}
        {done && (
          <div className="out">
            <div className="comp">
              {/* The filled part is what the fold gave back, not what it kept:
                  the bar's fill is the ok colour, and more of it has to mean
                  more room. */}
              {shrank && (
                <div className="comp-bar" title={`${before} → ${after} tokens`}>
                  <i style={{ width: `${Math.max(2, Math.round((1 - after / before) * 100))}%` }} />
                </div>
              )}
              <div className="comp-n">
                {c.messages ? (
                  <>{tx("compactionCard.foldedMessages", { n: <b>{c.messages}</b> })}</>
                ) : (
                  t("compactionCard.thisPassFoldedNothing")
                )}
                {cause && <>{" · "}{t(cause)}</>}
                {shrank && (
                  <>
                    {" · "}
                    <b>{tokens(before)}</b> → <b>{tokens(after)}</b>
                  </>
                )}
              </div>
              <Coverage c={c} />
              <Remains c={c} />
              {c.summary && <Brief text={c.summary} />}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function Brief({ text }: { text: string }) {
  const start = useStartsOpen("compaction");
  const [touched, setOpen] = useState<boolean | null>(null);
  const open = touched ?? start;
  return (
    <details open={open} onToggle={(e) => e.currentTarget.open !== open && setOpen(e.currentTarget.open)}>
      <summary>
        <span className="fold">{t("common.seeTheDigestItCarries")}</span>
      </summary>
      <div className="txt">{text}</div>
    </details>
  );
}
