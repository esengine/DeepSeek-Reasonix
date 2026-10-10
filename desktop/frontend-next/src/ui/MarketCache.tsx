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
      <span className="t">{t("显示的是缓存数据")}</span>
      <span className="why">
        {cache.cause === "unreachable"
          ? t("无法连接社区市场，以下是 {when} 保存的内容，可能已过期。", { when })
          : t("社区市场暂时无法正常回应，以下是 {when} 保存的内容，可能已过期。", { when })}
      </span>
      <div className="acts">
        <button className="act" data-action={action} onClick={onRetry}>{t("重试")}</button>
      </div>
    </div>
  );
}
