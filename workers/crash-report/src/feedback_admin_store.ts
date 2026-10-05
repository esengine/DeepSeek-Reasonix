import type { Env } from "./env";
import type { FeedbackRow, Status } from "./feedback_types";

export async function load(env: Env, receipt: string): Promise<FeedbackRow | null> {
  return env.DB.prepare("SELECT * FROM feedback WHERE receipt = ?").bind(receipt).first<FeedbackRow>();
}

export async function readJson(request: Request): Promise<unknown> {
  try {
    return await request.json();
  } catch {
    return undefined;
  }
}

// Compare-and-set on the status the caller read, so two writers cannot both win.
export function setStateStatement(env: Env, receipt: string, from: Status, sets: string, binds: unknown[]): D1PreparedStatement {
  return env.DB.prepare(`UPDATE feedback SET ${sets}, updated_at = ? WHERE receipt = ? AND status = ?`).bind(...binds, new Date().toISOString(), receipt, from);
}

export async function setState(env: Env, receipt: string, from: Status, sets: string, binds: unknown[]): Promise<boolean> {
  const res = await setStateStatement(env, receipt, from, sets, binds).run();
  return (res.meta?.changes ?? 0) > 0;
}

export function listLimit(url: URL, fallback = 20, max = 50): number {
  const asked = Number(url.searchParams.get("limit") ?? fallback);
  return Math.min(max, Math.max(1, Number.isFinite(asked) ? Math.trunc(asked) : fallback));
}
