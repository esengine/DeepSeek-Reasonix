import { current, t } from "../i18n";
import type { MarketCache } from "../port/port";

function savedAt(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(current() === "zh" ? "zh-CN" : "en", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

export function CacheNotice({ cache, action, onRetry }: { cache: MarketCache; action: string; onRetry: () => void }) {
  const when = savedAt(cache.cachedAt);
  return (
    <div className="find" data-cached="" role="status">
      <span className="t">{t("ext.showing_cached_data")}</span>
      <span className="why">
        {cache.cause === "unreachable"
          ? t("ext.the_community_market_could_not", { when })
          : t("ext.the_community_market_is_not", { when })}
      </span>
      <div className="acts">
        <button className="act" data-action={action} onClick={onRetry}>{t("common.retry")}</button>
      </div>
    </div>
  );
}
