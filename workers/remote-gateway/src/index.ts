import {
  authenticateDevice,
  authorizeAttachmentDownload,
  authorizeAttachmentUpload,
  bearerToken,
  consumeGrant,
} from "./accounts";
import type { Env } from "./env";
import {
  offeredProtocols,
  REMOTE_AUTH_PROTOCOL_PREFIX,
  REMOTE_WEBSOCKET_PROTOCOL,
} from "./protocol";
export { RemoteSession } from "./session";

const DEVICE_PATH = /^\/v1\/devices\/([0-9a-f]{64})\/connect$/;
const ATTACHMENT_PATH = /^\/v1\/attachments\/([0-9a-f]{64})$/;
const SHA256_PATTERN = /^[0-9a-f]{64}$/;
const DEVICE_ADMISSION_MS = 30 * 60 * 1000;
const CONTROLLER_ADMISSION_MS = 15 * 60 * 1000;

function attachmentKey(objectId: string): string {
  return `encrypted/${objectId}`;
}

async function putAttachment(request: Request, env: Env, objectId: string, ticket: string): Promise<Response> {
  const contentLength = Number(request.headers.get("content-length"));
  const ciphertextSha256 = request.headers.get("x-reasonix-ciphertext-sha256")?.toLowerCase() ?? "";
  if (!Number.isSafeInteger(contentLength) || contentLength < 1 || !SHA256_PATTERN.test(ciphertextSha256)) {
    return jsonError(400, "invalid_attachment", "A content length and ciphertext SHA-256 are required.");
  }
  const authorized = await authorizeAttachmentUpload(env, {
    objectId,
    ticket,
    ciphertextBytes: contentLength,
    ciphertextSha256,
  });
  if (!authorized || authorized.attachment.objectId !== objectId) {
    return jsonError(401, "invalid_attachment_grant", "The upload grant is invalid or expired.");
  }
  if (contentLength > authorized.attachment.maxBytes || !request.body) {
    return jsonError(413, "attachment_too_large", "The encrypted attachment exceeds its grant.");
  }
  try {
    await env.ATTACHMENTS.put(attachmentKey(objectId), request.body, {
      httpMetadata: { contentType: "application/octet-stream", contentDisposition: "attachment" },
      customMetadata: { expiresAt: authorized.attachment.expiresAt },
      sha256: ciphertextSha256,
    });
  } catch {
    return jsonError(422, "ciphertext_mismatch", "The encrypted attachment did not match its declared hash.");
  }
  return Response.json({ attachment: { objectId, ciphertextBytes: contentLength, ciphertextSha256 } }, { status: 201 });
}

async function getAttachment(env: Env, objectId: string, ticket: string): Promise<Response> {
  const authorized = await authorizeAttachmentDownload(env, objectId, ticket);
  if (!authorized || authorized.attachment.objectId !== objectId) {
    return jsonError(401, "invalid_attachment_grant", "The download grant is invalid or expired.");
  }
  const object = await env.ATTACHMENTS.get(attachmentKey(objectId));
  if (!object || object.size !== authorized.attachment.ciphertextBytes) {
    return jsonError(404, "attachment_unavailable", "The encrypted attachment is unavailable.");
  }
  return new Response(object.body, {
    headers: {
      "cache-control": "private, no-store",
      "content-disposition": "attachment",
      "content-length": String(object.size),
      "content-type": "application/octet-stream",
      "x-content-type-options": "nosniff",
      "x-reasonix-ciphertext-sha256": authorized.attachment.ciphertextSha256,
    },
  });
}

function jsonError(status: number, code: string, message: string): Response {
  return Response.json({ error: { code, message } }, { status });
}

function requestOrigin(request: Request): string | null {
  const origin = request.headers.get("origin")?.trim();
  return origin || null;
}

function originAllowed(request: Request, env: Env): boolean {
  const origin = requestOrigin(request);
  if (!origin) return true;
  return env.ALLOWED_ORIGINS.split(",").map((item) => item.trim()).includes(origin);
}

function withAttachmentCors(request: Request, response: Response): Response {
  const origin = requestOrigin(request);
  if (!origin) return response;
  const headers = new Headers(response.headers);
  headers.set("access-control-allow-origin", origin);
  headers.set("access-control-expose-headers", "content-length,x-reasonix-ciphertext-sha256");
  headers.append("vary", "Origin");
  return new Response(response.body, { status: response.status, statusText: response.statusText, headers });
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
  const protocols = offeredProtocols(request);
  if (protocols.includes(REMOTE_WEBSOCKET_PROTOCOL)) {
    headers.set("sec-websocket-protocol", REMOTE_WEBSOCKET_PROTOCOL);
  } else {
    headers.delete("sec-websocket-protocol");
  }
  return env.REMOTE_SESSIONS.get(id).fetch(new Request(request, { headers }));
}

function connectionToken(request: Request): string | null {
  const headerToken = bearerToken(request);
  if (headerToken) return headerToken;
  const protocols = offeredProtocols(request);
  if (!protocols.includes(REMOTE_WEBSOCKET_PROTOCOL)) return null;
  const tickets = protocols
    .filter((value) => value.startsWith(REMOTE_AUTH_PROTOCOL_PREFIX))
    .map((value) => value.slice(REMOTE_AUTH_PROTOCOL_PREFIX.length));
  if (tickets.length !== 1 || !SHA256_PATTERN.test(tickets[0] ?? "")) return null;
  return tickets[0] ?? null;
}

const worker: ExportedHandler<Env> = {
  async fetch(request, env): Promise<Response> {
    const url = new URL(request.url);
    if (url.pathname === "/health" && request.method === "GET") {
      return Response.json({ ok: true, service: "reasonix-remote-gateway" });
    }
    if (!originAllowed(request, env)) {
      return jsonError(403, "origin_rejected", "This website is not allowed to use the remote gateway.");
    }
    const attachmentMatch = ATTACHMENT_PATH.exec(url.pathname);
    if (attachmentMatch?.[1] && request.method === "OPTIONS") {
      return withAttachmentCors(request, new Response(null, {
        status: 204,
        headers: {
          "access-control-allow-headers": "authorization,content-type,x-reasonix-ciphertext-sha256",
          "access-control-allow-methods": "GET,PUT,OPTIONS",
          "access-control-max-age": "600",
        },
      }));
    }
    if (attachmentMatch?.[1] && (request.method === "PUT" || request.method === "GET")) {
      const ticket = bearerToken(request);
      if (!ticket) return jsonError(401, "invalid_token", "A valid attachment grant is required.");
      if (!(await withinBudget(request, env, ticket))) {
        return jsonError(429, "rate_limited", "Too many attachment requests.");
      }
      const response = await (request.method === "PUT"
        ? putAttachment(request, env, attachmentMatch[1], ticket)
        : getAttachment(env, attachmentMatch[1], ticket));
      return withAttachmentCors(request, response);
    }
    if (request.headers.get("upgrade")?.toLowerCase() !== "websocket") {
      return jsonError(426, "upgrade_required", "A WebSocket connection is required.");
    }
    const token = connectionToken(request);
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
