import type { TrajectoryAvailability, WireEvent } from "../port/wire";
import { seconds, tokens } from "../i18n/format";

export type Span = { t: string } | { b: string } | { n: string };

export interface TrajRow {
  seq: number;
  at: number;
  kind: string;
  payload: Span[];
  subs: Span[][];
  // 时间轴画的是事实，不是格式化过的字：一条记录代表的活动跑了多久（秒），
  // 以及是哪个工具跑的 —— 颜色由界面那层按类别决定。
  dur?: number;
  tool?: string;
  // Provider-reported completion tokens for a model round. Kept numeric so a
  // performance view never has to parse the abbreviated text drawn on screen.
  out?: number;
}

export interface TrajState {
  rows: TrajRow[];
  t0: number;
  // Activities still running, by the id the kernel gave them. A row is opened
  // when one starts and settled when it ends — the two halves are one line, not
  // two, and until the second half lands the line is the thing in flight.
  open: Record<string, number>;
  // Model rounds by the attempt id the kernel gave them, kept after they settle.
  // A round's usage arrives once the round has closed, so "whichever one is
  // still open" resolves to nothing and the tokens go on the floor.
  rounds: Record<string, number>;
  // What the replayed rows cover, as the host answered it. Undefined until that
  // read lands: before it does the table knows nothing about its own extent,
  // and saying "complete" on the way there is the guess this field exists to
  // stop.
  availability?: TrajectoryAvailability;
}

export const initialTraj: TrajState = { rows: [], t0: 0, open: {}, rounds: {} };


interface Made {
  kind: string;
  payload: Span[];
  subs?: Span[][];
  dur?: number;
  tool?: string;
  out?: number;
  // The key this record opens an activity under, settles, or adds to.
  open?: string;
  close?: string;
  touch?: string;
  // The model round this record belongs to, by attempt id, open or settled.
  round?: string;
  // Head for the row this opens when it joins no round — compaction and planner
  // calls bill tokens against no round at all, and a row of their own says so.
  standalone?: Span[];
  // The activity is still running: stretch its bar to now. What it printed
  // belongs to the transcript — this table records when, not what.
  grow?: boolean;
}

function record(ev: WireEvent): Made | null {
  switch (ev.kind) {
    case "turn_started":
      return { kind: "turn_started", payload: [{ t: "run begin" }] };

    case "message":
      return ev.itemId === "user"
        ? { kind: "event", payload: [{ t: "user_message" }] }
        : { kind: "event", payload: [{ t: "assistant_text" }] };

    case "tool_dispatch": {
      const tool = ev.tool;
      // A partial dispatch is one call streaming its arguments in, not a second
      // call; recording it doubles every row.
      if (!tool || tool.partial) return null;
      const head: Span[] = [{ t: "tool " }, { b: tool.resolvedName || tool.name }];
      if (tool.parentId) head.push({ t: " ↳ " }, { b: tool.parentId });
      return { kind: "tool", payload: head, tool: tool.resolvedName || tool.name, open: tool.id };
    }

    // Progress is the same call still running, so it lands on the line the call
    // already has rather than opening one of its own.
    case "tool_progress": {
      const tool = ev.tool;
      if (!tool?.id) return null;
      return { kind: "tool", payload: [], touch: tool.id, grow: true };
    }

    case "tool_result": {
      const tool = ev.tool;
      if (!tool) return null;
      const tail: Span[] = [];
      // The kernel measured this one; the wall clock here would also count the
      // trip back to the browser.
      if (tool.durationMs != null) tail.push({ t: " · " }, { n: seconds(tool.durationMs, 2) });
      if (tool.err) tail.push({ t: " · err=" }, { b: tool.err });
      return {
        kind: tool.err ? "protocol_recovery" : "tool",
        payload: tail,
        tool: tool.resolvedName || tool.name,
        dur: tool.durationMs != null ? tool.durationMs / 1000 : undefined,
        close: tool.id,
      };
    }

    // The round is where a turn's time actually goes — 98.8% of a measured
    // 50-second task — and it was the one activity the trajectory never drew.
    case "stream_attempt": {
      const sa = ev.streamAttempt;
      if (!sa?.id) return null;
      if (sa.action === "begin") {
        const head: Span[] = [{ t: "model_round" }];
        if ((sa.attempt ?? 1) > 1) head.push({ t: " · retry " }, { n: `${sa.attempt}/${sa.max ?? "?"}` });
        return { kind: "model_round", payload: head, open: sa.id };
      }
      const spent: Span[] =
        sa.action === "discard" ? [{ t: " · discarded" }, { t: " · " }, { b: sa.reason || "unknown" }] : [];
      return { kind: "model_round", payload: spent, close: sa.id };
    }

    case "usage": {
      const u = ev.usage;
      if (!u) return null;
      return {
        kind: "event",
        round: u.attemptId,
        out: u.completionTokens,
        standalone: [{ t: "usage" }],
        payload: [
          { t: " · hit " },
          { n: tokens(u.cacheHitTokens) },
          { t: " · miss " },
          { n: tokens(u.cacheMissTokens) },
          { t: " · out " },
          { n: tokens(u.completionTokens) },
          { t: " · src=" },
          { b: u.source || "executor" },
        ],
      };
    }

    case "guardian_assessment": {
      const g = ev.guardian;
      if (!g) return null;
      return {
        kind: "readiness_audit",
        payload: [{ t: "tool=" }, { b: g.tool }, { t: " · verdict=" }, { b: g.outcome }],
        subs: g.rationale ? [[{ t: g.rationale }]] : [],
        tool: g.tool,
      };
    }

    case "approval_request":
      return ev.approval
        ? {
            kind: "delegation_admission",
            payload: [{ t: "approval tool=" }, { b: ev.approval.tool }, { t: " · " + ev.approval.subject }],
            tool: ev.approval.tool,
          }
        : null;

    case "ask_request":
      return ev.ask
        ? { kind: "event", payload: [{ t: "ask_request · " }, { n: String(ev.ask.questions.length) }, { t: " 个问题" }] }
        : null;

    case "compaction_started":
      return { kind: "outcome_progress", payload: [{ t: "compaction started" }] };

    case "compaction_done":
      return {
        kind: "outcome_progress",
        payload: [{ t: "compaction done · " }, { n: String(ev.compaction?.messages ?? 0) }, { t: " 条" }],
      };

    case "retrying":
      return {
        kind: "protocol_recovery",
        payload: [
          { t: "retry " },
          { n: `${ev.retryAttempt ?? 0}/${ev.retryMax ?? 0}` },
          { t: " · scope=" },
          { b: ev.retryScope ?? "stream" },
          ...(ev.retryCause
            ? [{ t: " · cause=" }, { b: ev.retryCause }, ...(ev.retryStatus ? [{ t: ` ${ev.retryStatus}` }] : [])]
            : []),
        ],
      };

    case "steer":
      return { kind: "memory_recall", payload: [{ t: "steer delivered · " }, { b: ev.text ?? "" }] };

    case "todo_progress": {
      const p = ev.todoProgress;
      if (!p) return null;
      // The three revisions are the point: a kind alone reads as activity.
      return {
        kind: "outcome_progress",
        payload: [
          { t: "todo " }, { b: p.kind },
          { t: " · content " }, { n: String(p.contentRevision ?? 0) },
          { t: " · plan " }, { n: String(p.planRevision ?? 0) },
          { t: " · progress " }, { n: String(p.progressRevision ?? 0) },
        ],
      };
    }

    case "context_maintenance": {
      const m = ev.maintenance;
      if (!m) return { kind: "outcome_progress", payload: [{ t: ev.text || "context maintenance" }] };
      const payload: Span[] = [{ t: "context maintenance · " }, { b: m.status ?? "" }];
      if (m.boundary) payload.push({ t: " · " }, { b: m.boundary });
      // A verdict without its code is the line that made the last runaway
      // unreadable: it says maintenance happened, never what it decided.
      if (m.code) payload.push({ t: " · " }, { b: m.code });
      if (m.inputTokens) payload.push({ t: " · in " }, { n: String(m.inputTokens) });
      if (m.resultTokens) payload.push({ t: " → " }, { n: String(m.resultTokens) });
      if (m.triggerTokens) payload.push({ t: " · trigger " }, { n: String(m.triggerTokens) });
      return { kind: "outcome_progress", payload };
    }

    case "completion_summary": {
      const c = ev.completion;
      if (!c) return null;
      return {
        kind: "completion_report",
        payload: [
          { t: "verdict=" },
          { b: c.verdict },
          { t: " · mutations " },
          { n: String(c.mutations) },
          { t: " · checks " },
          { n: `${c.checks_passed}/${c.checks_passed + c.checks_failed}` },
        ],
        subs: c.review ? [[{ t: c.review }]] : [],
      };
    }

    case "notice":
      return {
        kind: ev.code ? "protocol_recovery" : "event",
        payload: [{ t: "notice " }, { b: ev.level ?? "info" }, { t: " · " + (ev.text ?? "") }],
      };

    case "turn_done": {
      const head: Span[] = [{ t: ev.err ? "turn_done · err=" + ev.err : "turn_done" }];
      if (ev.cancelled) head.push({ t: " · " }, { b: "cancelled by user" });
      return { kind: "event", payload: head };
    }

    default:
      return null;
  }
}

export function reduceTraj(
  s: TrajState,
  ev:
    | WireEvent
    | { kind: "__clear" }
    | { kind: "__user"; text: string }
    | { kind: "__coverage"; availability: TrajectoryAvailability },
): TrajState {
  if (ev.kind === "__clear") return initialTraj;
  if (ev.kind === "__coverage") return { ...s, availability: ev.availability };
  // The kernel emits no event for your own turn, so the row that opens every
  // trajectory has to be written here or it is missing entirely.
  const r: Made | null =
    ev.kind === "__user"
      ? { kind: "event", payload: [{ t: "user_message · " }, { b: ev.text.slice(0, 60) }] }
      : record(ev as WireEvent);
  if (!r) return s;
  const now = Date.now();
  const t0 = s.t0 || now;
  const at = (now - t0) / 1000;

  // Settling or adding to a line already on the page: the activity is one row
  // from the moment it starts, so its end is an edit, not another entry.
  // A round is addressed by its attempt id whether or not it is still running,
  // which is what lets usage find it after it settled.
  const key = r.close ?? r.touch ?? r.round;
  if (key !== undefined) {
    const i = r.round !== undefined ? s.rounds[key] : s.open[key];
    if (i !== undefined && s.rows[i]) {
      const row = s.rows[i];
      const rows = s.rows.slice();
      rows[i] = {
        ...row,
        payload: r.payload.length ? [...row.payload, ...r.payload] : row.payload,
        subs: r.subs?.length ? [...row.subs, ...r.subs] : row.subs,
        // A tool reports its own duration; a round is measured by how long the
        // reader waited for it.
        dur: r.close ? (r.dur ?? at - row.at) : r.grow ? at - row.at : row.dur,
        tool: row.tool ?? r.tool,
        out: (row.out ?? 0) + (r.out ?? 0),
      };
      if (!r.close) return { ...s, rows };
      const open = { ...s.open };
      delete open[key];
      return { ...s, rows, open };
    }
    // Settling or growing something never opened is a frame missing its other
    // half, not a new activity. A record naming a round is different: the round
    // may be off the page, and the record still has something to report.
    if (r.close ?? r.touch) return s;
  }

  const payload = r.standalone ? [...r.standalone, ...r.payload] : r.payload;
  const rows = [
    ...s.rows,
    { seq: s.rows.length + 1, at, kind: r.kind, payload, subs: r.subs ?? [], dur: r.dur, tool: r.tool, out: r.out },
  ];
  const next: TrajState = { ...s, t0, rows };
  if (r.open) {
    next.open = { ...s.open, [r.open]: rows.length - 1 };
    // Kept past the close: this is the row a late usage report has to find.
    if (r.kind === "model_round") next.rounds = { ...s.rounds, [r.open]: rows.length - 1 };
  }
  return next;
}
