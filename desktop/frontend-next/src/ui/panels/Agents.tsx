import type { Item } from "../../state/session";
import { seconds } from "../../i18n/format";
import { shortArgs } from "../args";
import { t } from "../../i18n";
import { delegateStatus, delegateStatusLabel } from "../delegation";

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
    <Grp id="agents" name={t("子代理")}>
      <Row
        k={t("数量")}
        v={<span className={live ? "lk" : undefined}>{t("运行中 {live} / 共 {total}", { live, total: agentsIn(tasks) })}</span>}
      />
      <div className="agents">
        {shown.map((x) => {
          const status = delegateStatus(x.running, x.tool);
          return (
          <div className="ag" key={x.id} data-status={status}>
            <i
              className="pip"
              data-settled={status === "done" ? "" : undefined}
              data-failed={status === "failed" || status === "refused" ? "" : undefined}
              style={x.running ? { background: "var(--net)", animation: "tick 1.6s ease-in-out infinite" } : undefined}
            />
            <span className="nm">
              {agentName(x)}
              {(x.tool.profile?.count ?? 1) > 1 && <b className="mult">×{x.tool.profile?.count}</b>}
            </span>
            <span className="rt">
              {status === "done" && x.tool.durationMs ? seconds(x.tool.durationMs, 0) : delegateStatusLabel(status)}
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
          );
        })}
      </div>
    </Grp>
  );
}
