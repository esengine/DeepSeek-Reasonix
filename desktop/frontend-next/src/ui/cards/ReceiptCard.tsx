import type { Receipt } from "../../port/wire";
import { t } from "../../i18n";
import { Sym } from "../Sym";

// The end-of-turn receipt. It reports evidence, never scope: the host knows
// what it could and could not verify, and it knows nothing about how much of
// the ask is left. A card that turns "unproven" into "half-finished" contradicts
// the turn the user just watched, so no verdict noun is rendered at all —
// what is missing is listed, and what carried a clean answer is sourced.
//
// Weights, lightest first: a clean turn settles into one ghost line; anything
// the host has to say is one of its cards, drawn like the rest of them, with
// each item a .find so a receipt and a notice read as one runtime speaking.

// A switch of literal t() calls rather than a lookup table: the catalogue gate
// reads t("…") out of the source, and a table's values are invisible to it —
// which is how the card this replaced shipped an all-Chinese English window.
// An unknown kind prints as itself; dropping one is the failure this card
// exists to prevent. declared_unverified is retired but stored sessions replay
// the words they were written with.
function gapPhrase(kind: string): string {
  switch (kind) {
    case "unbacked_claim": return t("chat.claimed_but_the_ledger_does");
    case "unproven_criterion": return t("chat.an_acceptance_criterion_has_no");
    case "missing_check": return t("chat.an_expected_check_never_passed");
    case "failed_verification": return t("chat.verification_failed");
    case "stale_verification": return t("chat.the_verification_predates_the_last");
    case "inconclusive_verification": return t("chat.it_ran_but_the_exit");
    case "unverified_change": return t("chat.changed_no_check_the_host");
    case "unreviewed_change": return t("chat.changed_never_looked_at_again");
    case "declared_unverified": return t("chat.declared_unverified_by_the_turn");
    case "mod_effects_unobserved": return t("chat.a_claude_code_mod_was");
    case "workspace_claim_yielded": return t("chat.another_session_wrote_under_this");
    case "outside_observed_root": return t("chat.written_outside_the_folder_this");
    default: return kind;
  }
}

// A receipt long enough to scroll is a receipt nobody reads; the count keeps
// the total honest.
const MAX_GAPS = 5;

// A command is evidence, not a document: the card shows where it starts and the
// whole of it stays in the hover title.
const DETAIL_CHARS = 120;
function gapDetail(detail: string): string {
  const first = detail.split("\n").find((line) => line.trim() !== "") ?? "";
  const more = first.trim() !== detail.trim();
  const line = first.trim();
  return line.length > DETAIL_CHARS ? `${line.slice(0, DETAIL_CHARS)}…` : more ? `${line} …` : line;
}

// What carried a clean verdict, so "verified" is never an unsourced assertion.
function evidence(r: Receipt): string[] {
  const out: string[] = [];
  if (r.changes?.length) out.push(t("common.n_files", { n: r.changes.length }));
  for (const v of r.verifications ?? []) {
    if (v.passed && !v.stale && !v.inconclusive) out.push(v.command);
  }
  return out;
}

// Verified is the settling line: what carried the clean answer, sourced.
function Verified({ r }: { r: Receipt }) {
  const parts = evidence(r);
  return (
    <div className="rc-ok">
      <span className="rc-tick">✓</span>
      <span className="rc-t">{t("chat.nothing_left_unverified")}</span>
      {parts.length > 0 && <span className="rc-src">{parts.join(" · ")}</span>}
    </div>
  );
}

export function ReceiptCard({ r }: { r: Receipt }) {
  const gaps = r.gaps ?? [];
  const declared = [...(r.unverified ?? []), ...(r.risks ?? [])];
  const clean = gaps.length === 0;

  if (clean && declared.length === 0) return <div className="rc"><Verified r={r} /></div>;

  const shown = gaps.slice(0, MAX_GAPS);
  const rest = gaps.length - shown.length;
  return (
    <div className="call" data-k="host" data-lvl={gaps.length > 0 ? "warn" : undefined}>
      <div className="g">
        <Sym glyph="✓" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{t("chat.delivery_receipt")}</span>
          <span className="tag">{t("chat.host")}</span>
        </div>
        <div className="out">
          {clean && <Verified r={r} />}
          {shown.map((g, i) => (
            <div className="find" data-lvl="warn" key={`${g.kind}:${g.detail ?? i}`} style={{ "--i": i } as React.CSSProperties}>
              <span className="t">{gapPhrase(g.kind)}</span>
              {g.detail && <code className="gapd" title={g.detail}>{gapDetail(g.detail)}</code>}
            </div>
          ))}
          {rest > 0 && <div className="rc-more">{t("chat.and_n_more", { n: rest })}</div>}
          {declared.length > 0 && (
            <>
              {/* Volunteered, not found — so it carries no severity. */}
              <div className="rc-hd">{t("chat.this_turn_said_so_itself")}</div>
              {declared.map((d, i) => (
                <div className="find" key={d} style={{ "--i": gaps.length + i } as React.CSSProperties}>
                  <span className="t">{d}</span>
                </div>
              ))}
            </>
          )}
        </div>
      </div>
    </div>
  );
}

// What stands in for the card while it is hidden: one line, only when the
// kernel graded the receipt. The grade is the kernel's; this chooses a glyph
// and a colour for it. The words follow what the receipt holds: host-found gaps
// are unproven work, while declarations alone are the turn's own caveats.
export function ReceiptHint({ r, open, onToggle }: { r: Receipt; open: boolean; onToggle: () => void }) {
  if (!r.severity) return null;
  const warn = r.severity === "warn";
  const text = r.gaps?.length ? t("chat.unverified_changes") : t("chat.this_turn_declared_risks_or");
  const label = `${warn ? t("chat.warning") : t("chat.notice")}：${text}`;
  return (
    <div className="rc" data-severity={r.severity}>
      <button
        type="button" className="rc-ok" data-action="chrome.receipt-line" aria-expanded={open} aria-label={label}
        title={open ? t("chat.collapse_the_delivery_receipt") : t("chat.click_to_see_the_delivery")}
        onClick={onToggle}
      >
        <span className="rc-tick" style={{ "--rc-c": warn ? "var(--warn-ink)" : "var(--muted)" } as React.CSSProperties}>{warn ? "!" : "•"}</span>
        <span className="rc-t">{text}</span>
      </button>
    </div>
  );
}
