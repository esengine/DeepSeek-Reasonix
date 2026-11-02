import { useEffect, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, CompactionSettings } from "../port/port";

const tokens = (n: number) => (n >= 1000 ? `${Math.round(n / 1000)}k` : String(n));

export function Compaction({ port }: { port: AgentPort }) {
  const [box, setBox] = useState<CompactionSettings | null>(null);
  const [used, setUsed] = useState<number | null>(null);
  useEffect(() => {
    port
      .compaction()
      .then(setBox)
      .catch(() => setBox(null));
    // The threshold is a number until it is next to the session's own usage,
    // and then it is a distance. A gauge that fails to load costs the reader
    // the distance, not the setting.
    port.context().then((c) => setUsed(c.used)).catch(() => setUsed(null));
  }, [port]);

  if (!box) return <div className="empty">{t("无法读取压缩配置。")}</div>;

  const win = box.context_window;
  const capacity = Math.round(win * box.ratio);
  const off = win === 0;
  const pct = off || !used ? 0 : Math.min((used / box.trigger) * 100, 100);

  return (
    <>
      <div className="cmp">
        {off ? (
          <p className="note">{t("该来源未声明上下文窗口，因此不会自动整理上下文。请先在右侧「上下文」中填写该模型的窗口大小。")}</p>
        ) : (
          <>
            <div className="cmpg">
              <div className="r">
                <span className="k">{t("当前")}</span>
                <b>{used === null ? "—" : tokens(used)}</b>
              </div>
              <div className="r">
                <span className="k">{t("下次整理")}</span>
                <b>{tokens(box.trigger)}</b>
              </div>
              <div className="r">
                <span className="k">{t("模型窗口")}</span>
                <b>{tokens(win)}</b>
              </div>
            </div>
            {used !== null && (
              <div className="cmpbar" role="presentation">
                <i style={{ width: `${pct}%` }} />
              </div>
            )}
            <p className="note">{t("本会话预计在 {n} tokens 左右自动整理上下文。", { n: tokens(box.trigger) })}</p>
          </>
        )}
      </div>

      <div className="lrow">
            <span className="tx">
              <span className="lb">{t("容量保护")}</span>
              <span className="ds">
                {t("模型窗口的 {p}% 自动整理。中转站未提供容量时使用 160k，可在上下文面板改为实际值。", { p: String(Math.round(box.ratio * 100)) })}
              </span>
            </span>
            <span className="sc">{off ? "—" : tokens(capacity)}</span>
      </div>
    </>
  );
}
