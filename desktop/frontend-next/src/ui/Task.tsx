import { t } from "../i18n";
import type { PlanStep } from "../state/session";
import type { TrajRow } from "../state/trajectory";
import { categoryOf } from "./icons";
import { Spans } from "./Trajectory";

const pad = (n: number) => String(n).padStart(2, "0");

const clock = (secs: number) => {
  const s = Math.max(0, Math.round(secs));
  return `${pad(Math.floor(s / 3600))}:${pad(Math.floor(s / 60) % 60)}:${pad(s % 60)}`;
};

const wall = (ms: number) => {
  const d = new Date(ms);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
};

// The tail is what is happening; the head is history the trajectory tab holds
// in full. A dashboard that grows without bound stops being a dashboard.
const TAIL = 8;

interface Props {
  goal: string;
  ask: string;
  plan: PlanStep[];
  rows: TrajRow[];
  t0: number;
  running: boolean;
  blocked: boolean;
  elapsed: number;
  onTrajectory: () => void;
  onLatest: () => void;
  onSummary: () => void;
}

export function Task({ goal, ask, plan, rows, t0, running, blocked, elapsed, onTrajectory, onLatest, onSummary }: Props) {
  const total = plan.length;
  const done = plan.filter((s) => s.done).length;
  const now = plan.findIndex((s) => !s.done);
  const state = blocked
    ? { text: t("等你"), tone: "accent" }
    : running
      ? { text: t("运行中"), tone: "net" }
      : total > 0 && done === total
        ? { text: t("已完成"), tone: "ok" }
        : { text: t("待命"), tone: undefined };
  // While a turn runs the tick owns the clock; once it stops the clock is what
  // the trajectory spans, because the turn's own start is cleared on the way out.
  const span = rows.length ? Math.max(...rows.map((r) => r.at + (r.dur ?? 0))) : 0;
  const tail = rows.length > TAIL ? rows.slice(-TAIL) : rows;

  return (
    <div className="tkv">
      <section className="tkcard tk-now">
        <div className="tk-h">
          <span>{t("当前任务")}</span>
          <span className="pill" data-tone={state.tone}>
            <i />
            {state.text}
          </span>
        </div>
        <h2>{goal || t("描述一个任务")}</h2>
        {ask && <p>{ask}</p>}
        <div className="tk-meta">
          {t0 > 0 && <span>{t("开始于 {at}", { at: wall(t0) })}</span>}
          <span>{t("运行时长 {clock}", { clock: clock(running ? elapsed : span) })}</span>
        </div>
      </section>

      <section className="tkcard tk-plan">
        <div className="tk-h">
          <span>{t("执行计划")}</span>
          <span className="c">{total ? `${done} / ${total}` : "—"}</span>
        </div>
        {total === 0 ? (
          <p className="tk-empty">{t("尚未制定")}</p>
        ) : (
          <ol>
            {plan.map((st, i) => (
              <li key={`${st.text}#${i}`} data-done={st.done ? "" : undefined} data-now={i === now ? "" : undefined}>
                <i className="mk" aria-hidden="true" />
                <span className="tx">{st.text}</span>
                {i === now && running && <span className="st">{t("进行中")}</span>}
              </li>
            ))}
          </ol>
        )}
      </section>

      <div className="tk-cols">
        <section className="tkcard tk-traj">
          <div className="tk-h">
            <span>{t("执行轨迹（实时）")}</span>
            <button className="tk-lk" onClick={onTrajectory}>
              {t("查看完整轨迹")}
            </button>
          </div>
          {tail.length === 0 ? (
            <p className="tk-empty">{t("还没有动静")}</p>
          ) : (
            <div className="tk-rows">
              {tail.map((r) => (
                <div className="tk-row" key={r.seq} data-c={r.kind === "model_round" ? "round" : r.tool ? categoryOf(r.tool) : "sys"}>
                  <i className="dot" aria-hidden="true" />
                  <span className="at">{t0 > 0 ? wall(t0 + r.at * 1000) : ""}</span>
                  <span className="tx">
                    <Spans of={r.payload} />
                  </span>
                </div>
              ))}
            </div>
          )}
        </section>

        <section className="tkcard tk-acts">
          <div className="tk-h">
            <span>{t("快速操作")}</span>
          </div>
          <button onClick={onTrajectory}>
            <span className="n">{t("查看执行轨迹")}</span>
            <span className="d">{t("查看完整运行日志与步骤详情")}</span>
          </button>
          <button onClick={onLatest}>
            <span className="n">{t("回到活动最新")}</span>
            <span className="d">{t("跳回这一轮正在写的地方")}</span>
          </button>
          <button data-action="task.summarize-phase" onClick={onSummary}>
            <span className="n">{t("生成阶段总结")}</span>
            <span className="d">{t("汇总当前进度与潜在风险")}</span>
          </button>
        </section>
      </div>
    </div>
  );
}
