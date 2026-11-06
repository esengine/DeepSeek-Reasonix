import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { GitHistory } from "../port/workspace";
import { layoutGraph, type GraphRow } from "../state/gitgraph";
import { GitCommitDetail } from "./GitCommitDetail";
import { StudioIcon } from "./StudioIcon";
import { WorkbenchState } from "./WorkbenchState";

// The board writes a commit's age as a bare count and unit, no "ago".
function age(at: number, now: number): string {
  const minutes = Math.floor(Math.max(0, now - at * 1000) / 60000);
  if (minutes < 1) return t("workbench.history_now");
  if (minutes < 60) return t("workbench.history_min", { n: minutes });
  const hours = Math.floor(minutes / 60);
  return hours < 24 ? t("workbench.history_hour", { n: hours }) : t("workbench.history_day", { n: Math.floor(hours / 24) });
}

const ROW = 28;
const COL = 12;
const PAD = 8;
const x = (lane: number) => PAD + lane * COL;

type Read = { k: "idle" } | { k: "loading" } | { k: "ready"; history: GitHistory } | { k: "failed"; why: string };

// The commit graph beside the changes: what the project's history looks like,
// read from git on request and never changed from here. It reads when opened and
// again on demand, so a closed section costs nothing.
export function WorkbenchHistory({ port, repo, branch }: { port: AgentPort; repo?: string; branch?: string }) {
  const [openId, setOpenId] = useState("");
  const [shown, setShown] = useState(true);
  const [read, setRead] = useState<Read>({ k: "idle" });
  const latest = useRef(0);
  const load = useCallback(async () => {
    const id = ++latest.current;
    setRead({ k: "loading" });
    try {
      const history = await port.workspaceGitLog(100, repo);
      if (id === latest.current) setRead({ k: "ready", history });
    } catch (e) {
      if (id === latest.current) setRead({ k: "failed", why: reason(e) });
    }
  }, [port, repo]);
  useEffect(() => {
    if (shown && read.k === "idle") void load();
  }, [shown, read.k, load]);
  useEffect(() => () => { latest.current++; }, []);

  const history = read.k === "ready" ? read.history : null;
  const graph = history?.repo ? layoutGraph(history.commits) : null;
  const now = Date.now();
  return (
    <details className="workbench-history" open={shown} onToggle={(e) => setShown(e.currentTarget.open)}>
      <summary>
        <StudioIcon name="down" className="gh-fold" />
        <span>{t("workbench.commit_history")}</span>
        {branch && <small className="gh-branch">{branch}</small>}
      </summary>
      {read.k === "loading" && <p className="gh-note" role="status">{t("common.loading")}</p>}
      {read.k === "failed" && (
        <p className="gh-note refine-error" role="alert">
          {read.why}
          <button type="button" className="btn sm" data-action="workbench.history-reload" onClick={() => void load()}>{t("common.read_again")}</button>
        </p>
      )}
      {history && !history.repo && <WorkbenchState icon="branch" title={t("workbench.this_folder_is_not_a")} detail={t("workbench.changes_and_history_need_git")} />}
      {graph && graph.rows.length === 0 && <p className="gh-note">{t("workbench.no_commits_yet")}</p>}
      {graph && graph.rows.length > 0 && (
        <ol className="gh-list">
          {graph.rows.map((row) => (
            <li key={row.commit.id} className="gh-item">
              <button type="button" className="gh-row" data-action="workbench.commit" data-target={row.commit.id} aria-expanded={openId === row.commit.id}
                title={`${row.commit.id.slice(0, 7)}  ${row.commit.subject}`} onClick={() => setOpenId(openId === row.commit.id ? "" : row.commit.id)}>
                {graph.lanes > 1 && <Lanes row={row} width={PAD * 2 + Math.max(graph.lanes - 1, 0) * COL} />}
                <code className="gh-hash">{row.commit.id.slice(0, 7)}</code>
                <span className="gh-subject">{row.commit.subject}</span>
                {row.commit.refs.map((ref) => (
                  <em className="gh-ref" key={ref} data-kind={ref.startsWith("tag: ") ? "tag" : "branch"}>{ref}</em>
                ))}
                <small className="gh-meta" title={row.commit.author}>{age(row.commit.at, now)}</small>
              </button>
              {openId === row.commit.id && <GitCommitDetail port={port} id={row.commit.id} repo={repo} />}
            </li>
          ))}
          {history?.truncated && <li className="gh-more">{t("workbench.only_the_most_recent_commits")}</li>}
        </ol>
      )}
    </details>
  );
}

const color = (lane: number) => `var(--cat-${(lane % 5) + 1})`;
// A line from (x1, y1) to (x2, y2): straight down a lane, a gentle S across lanes.
const line = (x1: number, y1: number, x2: number, y2: number) =>
  x1 === x2 ? `M${x1} ${y1}V${y2}` : `M${x1} ${y1}C${x1} ${(y1 + y2) / 2} ${x2} ${(y1 + y2) / 2} ${x2} ${y2}`;

function Lanes({ row, width }: { row: GraphRow; width: number }) {
  const mid = ROW / 2;
  return (
    <svg className="gh-lanes" width={width} height={ROW} viewBox={`0 0 ${width} ${ROW}`} aria-hidden="true">
      {row.through.map((lane) => <path key={`t${lane}`} d={line(x(lane), 0, x(lane), ROW)} stroke={color(lane)} />)}
      {row.from.map((lane) => <path key={`f${lane}`} d={line(x(lane), 0, x(row.lane), mid)} stroke={color(lane)} />)}
      {row.to.map((lane, n) => <path key={`o${n}`} d={line(x(row.lane), mid, x(lane), ROW)} stroke={color(lane)} />)}
      <circle cx={x(row.lane)} cy={mid} r={4} fill={color(row.lane)} data-merge={row.commit.parents.length > 1 ? "" : undefined} />
    </svg>
  );
}
