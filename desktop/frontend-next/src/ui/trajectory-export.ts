import type { Span, TrajRow } from "../state/trajectory";
import type { TrajectoryAvailability } from "../port/wire";

const flat = (spans: Span[]) => spans.map((x) => ("b" in x ? x.b : "n" in x ? x.n : x.t)).join("");

export const spanText = (spans: Span[]) => flat(spans);

export function serialiseTrajectory(rows: TrajRow[], availability?: TrajectoryAvailability) {
  return JSON.stringify(
    {
      exported: new Date().toISOString(),
      availability,
      span: rows.length ? Number(Math.max(...rows.map((row) => row.at + (row.dur ?? 0))).toFixed(3)) : 0,
      rows: rows.map((row) => ({
        seq: row.seq,
        at: Number(row.at.toFixed(3)),
        dur: row.dur === undefined ? undefined : Number(row.dur.toFixed(3)),
        kind: row.kind,
        tool: row.tool,
        text: flat(row.payload),
        detail: row.subs.map(flat),
      })),
    },
    null,
    2,
  );
}

export const trajectoryFileName = () =>
  `trajectory-${new Date().toISOString().slice(0, 19).replace("T", "-").replace(/:/g, "")}.json`;
