import { t } from "../../i18n";
import { Spark } from "../Spark";
import { useTrail } from "../num";
import type { Stats } from "./derive";
import { Grp, Row } from "./kit";

interface Props {
  rate: number;
  done: boolean;
  stats: Stats;
}

/** What keeping the run moving is costing, for the parts of it that have
 *  something to report. A row per counter, standing at zero whether or not
 *  anything had happened, spent the same width on a clean run as on a broken
 *  one — and a reader who has learned to skip six zeroes skips the seventh. */
export function Runtime({ rate, done, stats }: Props) {
  const trail = useTrail(rate, !done);
  // The one fact here a reader can act on, and the only one that is about them
  // rather than about the run. It leads, and it is toned as a request rather
  // than as telemetry.
  const waiting = stats.waiting > 0;
  const speaks = waiting || stats.failed > 0 || stats.external > 0 || !done;
  if (!speaks) return null;

  return (
    <Grp
      id="runtime"
      name={t("运行指标")}
      // Demoted from a row: how many calls a turn made is a true cumulative
      // fact and almost never the thing that decides what to do next.
      aside={stats.tools > 0 ? t("{n} 次调用", { n: stats.tools }) : undefined}
    >
      {waiting && <Row k={t("待确认")} v={stats.waiting} tone="act" />}
      {/* Throughput is about a turn in flight; once one is over it has nothing
          left to say, and D1 already made it fall silent within a turn. */}
      {!done && (
        <Row
          k={t("吞吐（输出）")}
          v={
            <span className="rate" data-live={rate >= 1 ? "" : undefined}>
              <Spark points={trail} />
              <span className="v">{rate >= 1 ? Math.round(rate) : "—"}</span>
              <span className="u">tok/s</span>
            </span>
          }
        />
      )}
      {stats.failed > 0 && <Row k={t("失败步数")} v={stats.failed} tone="err" />}
      {stats.external > 0 && <Row k={t("外部请求")} v={stats.external} />}
    </Grp>
  );
}
