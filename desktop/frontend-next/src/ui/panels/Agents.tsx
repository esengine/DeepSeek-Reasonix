import type { Item } from "../../state/session";
import { seconds } from "../../i18n/format";
import { shortArgs } from "../args";
import { t } from "../../i18n";
import { toolFailed } from "../cards/outcome";

import { agentsIn } from "./derive";
import { Grp, Row } from "./kit";
import { StudioIcon } from "../StudioIcon";

export const agentName = (x: Task) => x.tool.profile?.name || shortArgs(x.tool.args ?? "") || "task";

export type Task = Extract<Item, { t: "tool" }>;

// Same reason the file panel caps its rows: the rail reports, it does not
// enumerate. Running delegates come first — a finished one is history.
const SHOWN = 40;

export function Agents({ tasks, onOpen }: { tasks: Task[]; onOpen?: (taskId: string) => void }) {
  const live = agentsIn(tasks.filter((x) => x.running));
  const shown = tasks.length > SHOWN ? tasks.slice(-SHOWN) : tasks;
  // A session that has delegated nothing has nothing to say about delegates.
  if (tasks.length === 0) return null;

  return (
    <Grp id="agents" name={t("子代理")} aside={live ? t("{n} 个运行中", { n: live }) : undefined}>
      <Row
        k={t("数量")}
        v={live ? <span className="lk">{t("{n} 并行", { n: live })}</span> : agentsIn(tasks)}
      />
      <div className="agents">
        {shown.map((x) => (
          <div className="ag" key={x.id} data-status={x.running ? "running" : toolFailed(x.tool) ? "failed" : "done"}>
            <i
              className="pip"
              data-settled={x.running || toolFailed(x.tool) ? undefined : ""}
              data-failed={!x.running && toolFailed(x.tool) ? "" : undefined}
              style={x.running ? { background: "var(--net)", animation: "tick 1.6s ease-in-out infinite" } : undefined}
            />
            <span className="nm">
              {agentName(x)}
              {(x.tool.profile?.count ?? 1) > 1 && <b className="mult">×{x.tool.profile?.count}</b>}
            </span>
            <span className="rt">
              {x.running ? t("运行中") : toolFailed(x.tool) ? t("已中断") : x.tool.durationMs ? seconds(x.tool.durationMs, 0) : t("已交付")}
            </span>
            {onOpen && (
              <button
                type="button"
                className="ag-open"
                data-action="agent.open"
                data-target={x.id}
                title={t("查看完整记录")}
                aria-label={t("查看完整记录：{name}", { name: agentName(x) })}
                onClick={() => onOpen(x.id)}
              >
                <StudioIcon name="chevron" />
              </button>
            )}
          </div>
        ))}
      </div>
    </Grp>
  );
}
