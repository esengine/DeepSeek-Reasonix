import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { Item } from "../state/session";
import { activityVerb } from "./activityEarlier";
import { shortArgs } from "./args";
import { StudioIcon } from "./StudioIcon";

type Tool = Extract<Item, { t: "tool" }>["tool"];

const toolsOf = (items: readonly Item[]): Tool[] => items.flatMap((i) => (i.t === "tool" ? [i.tool] : i.t === "reads" ? i.tools : []));

/** How long the group's work took: from the first call starting to the last
 *  one ending when every call says when, else the calls' durations added up. */
export function groupSpan(items: readonly Item[]): number {
  const tools = toolsOf(items);
  if (tools.length > 0 && tools.every((x) => x.startedAt && x.endedAt)) {
    return Math.max(...tools.map((x) => x.endedAt!)) - Math.min(...tools.map((x) => x.startedAt!));
  }
  return tools.reduce((n, x) => n + (x.durationMs ?? 0), 0);
}

function useTicking(since: number | undefined): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!since) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [since]);
  return since ? Math.max(0, now - since) : 0;
}

/** A count that rolls up when it changes under the reader, and simply is
 *  what it is when it is first drawn. */
export function Bump({ value, children }: { value: number; children: string }) {
  const seen = useRef(value);
  const [moved, setMoved] = useState<number | null>(null);
  useEffect(() => {
    if (seen.current !== value) setMoved(value);
    seen.current = value;
  }, [value]);
  return (
    <span className="bump">
      <span key={value} data-moved={moved === value ? "" : undefined}>{children}</span>
    </span>
  );
}

function Now({ tool, startedAt }: { tool: Tool; startedAt?: number }) {
  const since = useRef(startedAt ?? Date.now()).current;
  const ms = useTicking(since);
  const verb = activityVerb(tool.resolvedName || tool.name);
  return (
    <span className="activity-now">
      <i className="step-spin" aria-hidden="true" />
      {verb ? t(verb) : tool.name} <code>{shortArgs(tool.args ?? "")}</code> · {t("common.n_s", { n: Math.floor(ms / 1000) })}
    </span>
  );
}

/** The group's title line: what it is doing now while it runs folded, how many
 *  run or failed while open, and once it is done what it came to. */
export function ActivityHead({ items, open, runs, failures, calls }: { items: Item[]; open: boolean; runs: number; failures: number; calls: number }) {
  const running = runs > 0;
  const now = running ? [...items].reverse().find((i): i is Extract<Item, { t: "tool" }> => i.t === "tool" && i.running) : undefined;
  const done = !running && failures === 0;
  const count = t("chat.count_actions", { count: calls });
  const span = done ? groupSpan(items) : 0;
  return (
    <summary>
      <StudioIcon name="down" className="activity-fold-icon" />
      <span className="activity-title">{t("chat.activity")}</span>
      {now && !open && <Now tool={now.tool} startedAt={now.tool.startedAt} />}
      {running && open && <span className="activity-running">{t("chat.n_running", { n: runs })}</span>}
      {failures > 0 && <span className="activity-errors">{t("chat.n_failed", { n: failures })}</span>}
      {done && <StudioIcon name="check" className="activity-ok" />}
      {now && !open && <span className="activity-fill" />}
      <span className="activity-count">
        <Bump value={calls}>{count}</Bump>
        {span >= 1000 && <> · {t("common.n_s", { n: Math.round(span / 1000) })}</>}
      </span>
    </summary>
  );
}

/** Each call with how many entering calls came before it in this draw, capped
 *  at five, so a burst arrives one row after another rather than all at once. */
export function staggered(items: Item[], owed: ReadonlySet<string>): [Item, number][] {
  let k = 0;
  return items.map((it) => [it, owed.has(it.id) ? Math.min(k++, 5) : 0]);
}
