import { t } from "../../i18n";
import { pct, tokens } from "../../i18n/format";
import type { Metrics } from "../../state/session";
import { Warn } from "../glyphs";

/** How much of the prompt the endpoint did not have to re-read.
 *
 *  The ratio is session-cumulative, which is the one shape that cannot do the
 *  job the size of it used to claim: the longer a session runs, the less a
 *  broken turn can move it, so a number that large was noticed for saying
 *  nothing. It reads as a figure beside the block's name now. What actually
 *  answers "why did this turn suddenly cost more" is the prefix having moved,
 *  and that is what surfaces — only when it has. */
export function Cache({ metrics }: { metrics: Metrics }) {
  const up = metrics.hit + metrics.miss;
  // Nothing has been asked yet, so there is nothing to report about it.
  if (!up) return null;
  const rate = (metrics.hit / up) * 100;
  // Both are drawn from the kernel's own figures, with nothing between: these
  // move once per request, and easing between two of them would put ratios on
  // screen that no request ever produced.
  const moved = metrics.prefixChanged;
  const body = metrics.bodyChanged && !!metrics.carriedMessages;

  return (
    <div className="block" data-b="cache">
      <h3 className="lbl">
        {t("前缀缓存")}
        <span className="c">{pct(rate / 100, 1)}</span>
      </h3>
      {/* The prefix moving is the fact worth interrupting for: it is what turns
          a cheap turn into an expensive one, and the only one here a reader can
          do anything about. Unchanged is the ordinary case and says nothing. */}
      {(moved || body) && (
        <p className="cachemoved" title={metrics.prefixReasons.join(" · ") || undefined}>
          <i aria-hidden="true"><Warn /></i>
          {moved ? t("前缀变了") : t("正文变了")}
        </p>
      )}
      <div className="bar">
        <i className="c" style={{ flexGrow: Math.max(rate, 0.4) }} />
        <i className="f" style={{ flexGrow: Math.max(100 - rate, 0.4) }} />
      </div>
      <div className="nums">
        <span>{t("命中")}<b>{tokens(metrics.hit)}</b></span>
        <span>{t("未命中")}<b>{tokens(metrics.miss)}</b></span>
        {!!metrics.toolSchema && <span>{t("工具 schema")}<b>{tokens(metrics.toolSchema)}</b></span>}
      </div>
      {!!metrics.prefixHash && (
        <div className="nums" style={{ justifyContent: "space-between" }}>
          <span title={metrics.prefixHash}>
            {t("前缀哈希")}<b>{metrics.prefixHash.slice(0, 4)}…{metrics.prefixHash.slice(-4)}</b>
          </span>
        </div>
      )}
    </div>
  );
}
