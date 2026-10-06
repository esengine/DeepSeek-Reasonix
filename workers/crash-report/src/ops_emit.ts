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

export type OpsRelay = Pick<Env, "OPS_EVENTS_URL" | "OPS_EMIT_TOKEN">;

export async function post(relay: OpsRelay, body: unknown): Promise<void> {
  const base = relay.OPS_EVENTS_URL;
  const token = relay.OPS_EMIT_TOKEN;
  if (!base || !token || !base.startsWith("https://")) return;
  try {
    const res = await fetch(`${base.replace(/\/+$/, "")}/emit`, {
      method: "POST",
      headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
    await res.body?.cancel();
  } catch {
    // best effort: the relay being down must never reach the caller
  }
}

export async function emitOps(env: Env, event: OpsEvent): Promise<void> {
  const extra: Record<string, string> = { receipt: event.receipt };
  if (event.category) extra.category = event.category;
  if (event.status) extra.status = event.status;
  await post(env, { src: "feedback", t: event.t, title: LABEL[event.t], extra });
}

export function announce(ctx: OpsWaiter | undefined, env: Env, event: OpsEvent): void {
  const work = emitOps(env, event);
  ctx?.waitUntil(work);
}
