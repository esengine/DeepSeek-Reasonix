import { authenticateDevice, bearerToken, consumeGrant } from "./accounts";
import type { Env } from "./env";
export { RemoteSession } from "./session";

const DEVICE_PATH = /^\/v1\/devices\/([0-9a-f]{64})\/connect$/;
const DEVICE_ADMISSION_MS = 30 * 60 * 1000;
const CONTROLLER_ADMISSION_MS = 15 * 60 * 1000;

function jsonError(status: number, code: string, message: string): Response {
  return Response.json({ error: { code, message } }, { status });
}

async function withinBudget(request: Request, env: Env, token: string): Promise<boolean> {
  if (!env.GATEWAY_LIMITER) return true;
  const ip = request.headers.get("cf-connecting-ip") ?? "unknown";
  return (await env.GATEWAY_LIMITER.limit({ key: `${ip}:${token.slice(0, 16)}` })).success;
}

function forwardToSession(
  request: Request,
  env: Env,
  targetDeviceId: string,
  role: "device" | "controller",
  userId: number,
  scopes: string[],
  admissionMs: number,
): Promise<Response> {
  const id = env.REMOTE_SESSIONS.idFromName(targetDeviceId);
  const headers = new Headers(request.headers);
  headers.set("x-reasonix-role", role);
  headers.set("x-reasonix-user-id", String(userId));
  headers.set("x-reasonix-scopes", scopes.join(","));
  headers.set("x-reasonix-expires-at", String(Date.now() + admissionMs));
  headers.delete("authorization");
  return env.REMOTE_SESSIONS.get(id).fetch(new Request(request, { headers }));
}

const worker: ExportedHandler<Env> = {
  async fetch(request, env): Promise<Response> {
    const url = new URL(request.url);
    if (url.pathname === "/health" && request.method === "GET") {
      return Response.json({ ok: true, service: "reasonix-remote-gateway" });
    }
    if (request.headers.get("upgrade")?.toLowerCase() !== "websocket") {
      return jsonError(426, "upgrade_required", "A WebSocket connection is required.");
    }
    const token = bearerToken(request);
    if (!token) return jsonError(401, "invalid_token", "A valid connection credential is required.");
    if (!(await withinBudget(request, env, token))) {
      return jsonError(429, "rate_limited", "Too many connection attempts.");
    }

    const deviceMatch = DEVICE_PATH.exec(url.pathname);
    if (deviceMatch?.[1]) {
      const authenticated = await authenticateDevice(env, deviceMatch[1], token);
      if (!authenticated || authenticated.device.id !== deviceMatch[1]) {
        return jsonError(401, "invalid_device", "The device credential is invalid or revoked.");
      }
      return forwardToSession(
        request, env, authenticated.device.id, "device", authenticated.userId,
        authenticated.device.capabilities,
        DEVICE_ADMISSION_MS,
      );
    }

    if (url.pathname === "/v1/sessions/connect") {
      const consumed = await consumeGrant(env, token);
      if (!consumed) return jsonError(401, "invalid_grant", "The connection grant is invalid or expired.");
      return forwardToSession(
        request, env, consumed.grant.targetDeviceId, "controller",
        consumed.grant.userId, consumed.grant.scopes,
        CONTROLLER_ADMISSION_MS,
      );
    }

    return jsonError(404, "not_found", "Not found.");
  },
};

export default worker;
