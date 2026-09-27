import type { AuthenticatedDevice, ConsumedGrant, Env } from "./env";

const TOKEN_PATTERN = /^[0-9a-f]{64}$/;

export function bearerToken(request: Request): string | null {
  const header = request.headers.get("authorization")?.trim() ?? "";
  const token = /^Bearer\s+([0-9a-f]{64})$/i.exec(header)?.[1]?.toLowerCase();
  return token && TOKEN_PATTERN.test(token) ? token : null;
}

async function accountRequest<T>(env: Env, path: string, body: unknown): Promise<T | null> {
  if (!env.REMOTE_GATEWAY_TOKEN) return null;
  try {
    const response = await fetch(`${env.ACCOUNT_ORIGIN.replace(/\/+$/, "")}${path}`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-reasonix-gateway-token": env.REMOTE_GATEWAY_TOKEN,
      },
      body: JSON.stringify(body),
    });
    if (!response.ok) return null;
    return await response.json<T>();
  } catch {
    return null;
  }
}

export function authenticateDevice(env: Env, deviceId: string, credential: string): Promise<AuthenticatedDevice | null> {
  return accountRequest(env, "/remote/devices/authenticate", { deviceId, deviceCredential: credential });
}

export function consumeGrant(env: Env, ticket: string): Promise<ConsumedGrant | null> {
  return accountRequest(env, "/remote/grants/consume", { ticket });
}
