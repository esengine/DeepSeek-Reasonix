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
    case "unbacked_claim": return t("声称过但账本不支持");
    case "unproven_criterion": return t("验收项没有证据");
    case "missing_check": return t("预期的检查从未通过");
    case "failed_verification": return t("验证失败");
    case "stale_verification": return t("验证早于最后一次改动");
    case "inconclusive_verification": return t("已运行，但退出码来自命令中的其他片段");
    case "unverified_change": return t("有改动；没有主机能读作证据的检查");
    case "unreviewed_change": return t("改动后未再检查");
    case "declared_unverified": return t("自己申报未验证");
    case "mod_effects_unobserved": return t("有 Claude Code mod 在运行；它写了什么不在主机观测范围内");
    case "workspace_claim_yielded": return t("你在等待期间，另一个会话改动了本会话占用的路径；之前读到的内容可能已过时");
    case "outside_observed_root": return t("写在了主机观测范围之外；这里没有任何证据能说明它留下了什么");
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
  if (r.changes?.length) out.push(t("{n} 个文件", { n: r.changes.length }));
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
      <span className="rc-t">{t("没有未经验证的部分")}</span>
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
          <span className="nm">{t("交付验收")}</span>
          <span className="tag">{t("主机")}</span>
        </div>
        <div className="out">
          {clean && <Verified r={r} />}
          {shown.map((g, i) => (
            <div className="find" data-lvl="warn" key={`${g.kind}:${g.detail ?? i}`} style={{ "--i": i } as React.CSSProperties}>
              <span className="t">{gapPhrase(g.kind)}</span>
              {g.detail && <code className="gapd" title={g.detail}>{gapDetail(g.detail)}</code>}
            </div>
          ))}
          {rest > 0 && <div className="rc-more">{t("另有 {n} 项", { n: rest })}</div>}
          {declared.length > 0 && (
            <>
              {/* Volunteered, not found — so it carries no severity. */}
              <div className="rc-hd">{t("这一轮自己说明的")}</div>
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
  const text = r.gaps?.length ? t("有未验证的改动") : t("这一轮自己说明了风险或未验证项");
  const label = `${warn ? t("警告") : t("提示")}：${text}`;
  return (
    <div className="rc" data-severity={r.severity}>
      <button
        type="button" className="rc-ok" data-action="chrome.receipt-line" aria-expanded={open} aria-label={label}
        title={open ? t("收起交付验收") : t("点击查看交付验收：改了什么、哪些没有验证")}
        onClick={onToggle}
      >
        <span className="rc-tick" style={{ "--rc-c": warn ? "var(--warn-ink)" : "var(--muted)" } as React.CSSProperties}>{warn ? "!" : "•"}</span>
        <span className="rc-t">{text}</span>
      </button>
    </div>
  );
}
