import type { Env } from "./env";

const TIMEOUT_MS = 2000;

export type OpsWaiter = Pick<ExecutionContext, "waitUntil">;

export interface OpsEvent {
  t: "submitted" | "replied" | "status";
  receipt: string;
  category?: string;
  status?: string;
}

const LABEL: Record<OpsEvent["t"], string> = {
  submitted: "feedback submitted",
  replied: "feedback user reply",
  status: "feedback status changed",
};

export async function emitOps(env: Env, event: OpsEvent): Promise<void> {
  const base = env.OPS_EVENTS_URL;
  const token = env.OPS_EMIT_TOKEN;
  if (!base || !token || !base.startsWith("https://")) return;
  const extra: Record<string, string> = { receipt: event.receipt };
  if (event.category) extra.category = event.category;
  if (event.status) extra.status = event.status;
  try {
    const res = await fetch(`${base.replace(/\/+$/, "")}/emit`, {
      method: "POST",
      headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
      body: JSON.stringify({ src: "feedback", t: event.t, title: LABEL[event.t], extra }),
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
    await res.body?.cancel();
  } catch {
    // best effort: the relay being down must never reach the feedback caller
  }
}

export function announce(ctx: OpsWaiter | undefined, env: Env, event: OpsEvent): void {
  const work = emitOps(env, event);
  ctx?.waitUntil(work);
}
