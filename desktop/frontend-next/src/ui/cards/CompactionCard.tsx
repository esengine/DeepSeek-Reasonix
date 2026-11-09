import { StudioIcon } from "../StudioIcon";
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
  if (c.trigger === "manual") return t("chat.triggered_by_hand");
  if (c.trigger !== "auto") return c.trigger ?? "";
  if (!c.triggerTokens) return t("chat.automatic_a_context_threshold_was");
  const n = tokens(c.triggerTokens);
  if (c.boundary === "capacity") return t("chat.automatic_input_reached_the_windows", { n });
  return t("chat.automatic_input_reached_n", { n });
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
        <span className="k"><StudioIcon name="check" /></span>
        <span>
          {missing === 0 ? (
            <>{tx("chat.all_n_changes_made_here", { n: <b>{required}</b> })}</>
          ) : (
            <>{tx("chat.kept_required_changes_are_in", { kept: <b>{kept}</b>, required })}</>
          )}
          {c.coverageBackstopped && t("chat.gaps_kept_by_the_host")}
        </span>
      </div>
      {missing > 0 && (
        <div className="row">
          <span className="x"><StudioIcon name="close" /></span>
          <span>
            {tx("chat.n_more_are_index_entries", { n: <b>{missing}</b> })}
          </span>
        </div>
      )}
    </div>
  );
}

const PART: Record<string, string> = {
  system: "common.system_prompt",
  tools: "common.tool_definitions",
  digest: "ext.summary",
  user: "kernel.user_messages",
  reply: "common.model_replies",
  tool_output: "common.tool_output",
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
          {tx("chat.n_still_held_after_the", { n: <b>{tokens(r.total)}</b>, top })}
          {big && big.tokens > 0 && <>{" · "}{big.tool ? t("chat.the_largest_is_a_tool", { tool: big.tool, n: tokens(big.tokens) }) : t("chat.the_largest_is_a_role", { role: t(PART[big.role === "assistant" ? "reply" : big.role === "tool" ? "tool_output" : "user"] ?? big.role), n: tokens(big.tokens) })}</>}
        </span>
      </div>
      {r.active > 0 && (
        <div className="row">
          <span className="k">·</span>
          <span>{t("chat.the_turn_in_progress_holds", { n: tokens(r.active) })}</span>
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
    <div className="call" data-k="host" data-light="">
      <div className="g">
        <Sym icon="collapseAll" />
      </div>
      <div className="c">
        <div className="hl">
          <span className={done ? "nm" : "nm shim"}>{t(!done ? "common.compacting" : aborted ? "chat.compaction_did_not_complete" : "chat.earlier_folded_into_digest")}</span>
          {c.trigger && <span className="tag">· {t(c.trigger === "manual" ? "chat.compaction_manual" : "chat.compaction_auto")}</span>}
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
                    <>{tx("chat.folding_n_messages_into_a", { n: <b>{c.messages}</b> })}</>
                  ) : (
                    t("chat.folding_into_a_digest")
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
              <div className="comp-n">
                {c.trigger && <>{why(c)}{" · "}</>}
                {c.messages ? (
                  <>{tx("chat.folded_n_messages", { n: <b>{c.messages}</b> })}</>
                ) : (
                  t("chat.this_pass_folded_nothing")
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
        <span className="fold">{t("chat.see_the_digest_it_carries")}</span>
      </summary>
      <div className="txt">{text}</div>
    </details>
  );
}
