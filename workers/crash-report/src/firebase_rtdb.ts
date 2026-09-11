import type { Env } from "./env";

const TOKEN_URL = "https://oauth2.googleapis.com/token";
const TOKEN_SCOPE = [
  "https://www.googleapis.com/auth/userinfo.email",
  "https://www.googleapis.com/auth/firebase.database",
].join(" ");
const TOKEN_REFRESH_SKEW_MS = 5 * 60 * 1000;
const REQUEST_TIMEOUT_MS = 5_000;

type Fetcher = typeof fetch;

type CachedToken = {
  clientEmail: string;
  value: string;
  expiresAt: number;
};

let cachedToken: CachedToken | undefined;
let tokenRequest: Promise<CachedToken> | undefined;

export type FirebaseCrashSample = Record<string, unknown> & {
  eventId: string;
  receivedAt: string;
  groupCount: number;
  writerGeneration: number;
  sampleEpoch: number;
};

export type FirebaseCrashSampleInput = Record<string, unknown> & {
  eventId: string;
  receivedAt: string;
};

export type FirebaseSampleMarker = {
  marker: "compacted" | "archiving";
  groupCount: number;
  writerGeneration: number;
  sampleEpoch: number;
};

export type FirebaseCrashGroupMeta = {
  fingerprint: string;
  kind: string;
  count: number;
  firstSeen: string;
  lastSeen: string;
  firstVersion: string;
  lastVersion: string;
  status: string;
  title: string;
  source: string;
  label: string;
  errorType: string;
  topFrame: string;
  severity: string;
  lastOS: string;
  lastArch: string;
  lastBuildCommit: string;
  lastChannel: string;
  regressedAt: string;
  writerGeneration?: number;
  sampleEpoch?: number;
  sampleState?: "active" | "compacted" | "archiving" | "archived";
};

type FirebaseFencedMeta = FirebaseCrashGroupMeta & {
  writerGeneration: number;
  sampleEpoch: number;
  sampleState: "active" | "compacted" | "archiving" | "archived";
};

export type FirebaseCrashGroup = {
  meta?: FirebaseCrashGroupMeta;
  samples?: {
    first?: FirebaseCrashSample;
    latest?: Record<string, FirebaseCrashSample | FirebaseSampleMarker> |
      Array<FirebaseCrashSample | FirebaseSampleMarker>;
  };
};

function base64url(data: Uint8Array | string): string {
  const bytes = typeof data === "string" ? new TextEncoder().encode(data) : data;
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  }
  return btoa(binary).replace(/=/g, "").replace(/\+/g, "-").replace(/\//g, "_");
}

function privateKeyBytes(pem: string): Uint8Array {
  const normalized = pem.replace(/\\n/g, "\n").trim();
  const encoded = normalized
    .replace("-----BEGIN PRIVATE KEY-----", "")
    .replace("-----END PRIVATE KEY-----", "")
    .replace(/\s/g, "");
  if (!encoded) throw new Error("firebase private key is empty");
  const binary = atob(encoded);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

async function serviceAccountAssertion(env: Env, now: number): Promise<string> {
  const clientEmail = env.FIREBASE_CLIENT_EMAIL?.trim();
  const privateKey = env.FIREBASE_PRIVATE_KEY;
  if (!clientEmail || !privateKey) throw new Error("firebase service account is not configured");
  const issuedAt = Math.floor(now / 1000);
  const header = base64url(JSON.stringify({ alg: "RS256", typ: "JWT" }));
  const claims = base64url(JSON.stringify({
    iss: clientEmail,
    scope: TOKEN_SCOPE,
    aud: TOKEN_URL,
    iat: issuedAt,
    exp: issuedAt + 3600,
  }));
  const unsigned = `${header}.${claims}`;
  const key = await crypto.subtle.importKey(
    "pkcs8",
    privateKeyBytes(privateKey),
    { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const signature = await crypto.subtle.sign(
    "RSASSA-PKCS1-v1_5",
    key,
    new TextEncoder().encode(unsigned),
  );
  return `${unsigned}.${base64url(new Uint8Array(signature))}`;
}

async function requestAccessToken(env: Env, fetcher: Fetcher, now: number): Promise<CachedToken> {
  const assertion = await serviceAccountAssertion(env, now);
  const body = new URLSearchParams({
    grant_type: "urn:ietf:params:oauth:grant-type:jwt-bearer",
    assertion,
  });
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
  try {
    const response = await fetcher(TOKEN_URL, {
      method: "POST",
      headers: { "content-type": "application/x-www-form-urlencoded" },
      body,
      signal: controller.signal,
    });
    if (!response.ok) throw new Error(`firebase oauth failed with ${response.status}`);
    const payload = await response.json() as { access_token?: unknown; expires_in?: unknown };
    if (typeof payload.access_token !== "string" || !payload.access_token) {
      throw new Error("firebase oauth response omitted access_token");
    }
    const expiresIn = typeof payload.expires_in === "number" ? payload.expires_in : 3600;
    return {
      clientEmail: env.FIREBASE_CLIENT_EMAIL!.trim(),
      value: payload.access_token,
      expiresAt: now + Math.max(60, expiresIn) * 1000,
    };
  } finally {
    clearTimeout(timeout);
  }
}

async function accessToken(env: Env, fetcher: Fetcher, now = Date.now()): Promise<string> {
  const clientEmail = env.FIREBASE_CLIENT_EMAIL?.trim() ?? "";
  if (
    cachedToken?.clientEmail === clientEmail &&
    cachedToken.expiresAt - TOKEN_REFRESH_SKEW_MS > now
  ) {
    return cachedToken.value;
  }
  if (!tokenRequest) {
    tokenRequest = requestAccessToken(env, fetcher, now).finally(() => {
      tokenRequest = undefined;
    });
  }
  cachedToken = await tokenRequest;
  return cachedToken.value;
}

function databaseURL(env: Env): string {
  const raw = env.FIREBASE_DATABASE_URL?.trim();
  if (!raw) throw new Error("firebase database URL is not configured");
  const url = new URL(raw);
  if (url.protocol !== "https:" || !(
    url.hostname.endsWith(".firebaseio.com") ||
    url.hostname.endsWith(".firebasedatabase.app")
  )) {
    throw new Error("firebase database URL is not an approved Realtime Database host");
  }
  url.pathname = url.pathname.replace(/\/$/, "");
  url.search = "";
  url.hash = "";
  return url.toString().replace(/\/$/, "");
}

async function firebaseFetch(
  env: Env,
  path: string,
  init: RequestInit,
  fetcher: Fetcher,
  retryAuth = true,
): Promise<Response> {
  const token = await accessToken(env, fetcher);
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
  try {
    const queryOffset = path.indexOf("?");
    const resource = queryOffset === -1 ? path : path.slice(0, queryOffset);
    const query = queryOffset === -1 ? "" : path.slice(queryOffset);
    const response = await fetcher(`${databaseURL(env)}/${resource}.json${query}`, {
      ...init,
      signal: controller.signal,
      headers: {
        "content-type": "application/json",
        ...init.headers,
        authorization: `Bearer ${token}`,
      },
    });
    if (response.status === 401 && retryAuth) {
      cachedToken = undefined;
      return firebaseFetch(env, path, init, fetcher, false);
    }
    return response;
  } finally {
    clearTimeout(timeout);
  }
}

export function firebaseConfigured(env: Env): boolean {
  return Boolean(
    env.FIREBASE_DATABASE_URL?.trim() &&
    env.FIREBASE_CLIENT_EMAIL?.trim() &&
    env.FIREBASE_PRIVATE_KEY,
  );
}

export class FirebaseFenceError extends Error {
  constructor() {
    super("firebase conditional write was fenced by a newer writer");
    this.name = "FirebaseFenceError";
  }
}

type FencedValue = {
  writerGeneration?: unknown;
  sampleEpoch?: unknown;
  groupCount?: unknown;
  count?: unknown;
  marker?: unknown;
};

function numberField(value: FencedValue | null, field: keyof FencedValue): number {
  const raw = value?.[field];
  return typeof raw === "number" && Number.isFinite(raw) ? raw : -1;
}

async function conditionalPut(
  env: Env,
  path: string,
  candidate: FencedValue,
  mayWrite: (current: FencedValue | null) => "write" | "complete" | "fenced",
  fetcher: Fetcher,
  beforeWrite: () => Promise<boolean>,
): Promise<void> {
  for (let attempt = 0; attempt < 3; attempt++) {
    const currentResponse = await firebaseFetch(env, path, {
      method: "GET",
      headers: { "X-Firebase-ETag": "true" },
    }, fetcher);
    if (!currentResponse.ok) {
      throw new Error(`firebase database conditional read failed with ${currentResponse.status}`);
    }
    const etag = currentResponse.headers.get("etag");
    if (!etag) throw new Error("firebase database conditional read omitted ETag");
    const raw = await currentResponse.json() as unknown;
    const current = raw && typeof raw === "object" && !Array.isArray(raw) ? raw as FencedValue : null;
    const decision = mayWrite(current);
    if (decision === "complete") return;
    if (decision === "fenced") throw new FirebaseFenceError();
    if (!await beforeWrite()) throw new FirebaseFenceError();
    const write = await firebaseFetch(env, `${path}?print=silent`, {
      method: "PUT",
      headers: { "If-Match": etag },
      body: JSON.stringify(candidate),
    }, fetcher);
    if (write.status === 412) continue;
    if (!write.ok) throw new Error(`firebase database conditional write failed with ${write.status}`);
    return;
  }
  throw new Error("firebase database conditional write retry limit reached");
}

function metaDecision(candidate: FirebaseFencedMeta) {
  return (current: FencedValue | null): "write" | "complete" | "fenced" => {
    if (!current) return "write";
    const generation = numberField(current, "writerGeneration");
    const epoch = numberField(current, "sampleEpoch");
    const count = numberField(current, "count");
    if (generation > candidate.writerGeneration || epoch > candidate.sampleEpoch) return "fenced";
    if (
      generation === candidate.writerGeneration && epoch === candidate.sampleEpoch &&
      count >= candidate.count
    ) return "complete";
    return "write";
  };
}

function sampleDecision(candidate: FirebaseCrashSample | FirebaseSampleMarker, first: boolean) {
  return (current: FencedValue | null): "write" | "complete" | "fenced" => {
    if (!current) return "write";
    const generation = numberField(current, "writerGeneration");
    const epoch = numberField(current, "sampleEpoch");
    const count = numberField(current, "groupCount");
    if (epoch > candidate.sampleEpoch || generation > candidate.writerGeneration) return "fenced";
    if (epoch < candidate.sampleEpoch) return "write";
    if ("marker" in candidate) {
      return generation === candidate.writerGeneration && current.marker === candidate.marker
        ? "complete" : "write";
    }
    if (first && current.marker === undefined) return "complete";
    if (generation === candidate.writerGeneration && count >= candidate.groupCount) return "complete";
    if (!first && current.marker === undefined && count >= candidate.groupCount) return "complete";
    return "write";
  };
}

export async function writeFirebaseCrashGroup(
  env: Env,
  meta: FirebaseCrashGroupMeta,
  sample: FirebaseCrashSampleInput,
  latestSlot: number | null,
  firstSample: boolean,
  writerGeneration: number,
  sampleEpoch: number,
  beforeWrite: () => Promise<boolean> = async () => true,
  fetcher: Fetcher = fetch,
): Promise<void> {
  if (latestSlot !== null && (!Number.isInteger(latestSlot) || latestSlot < 0 || latestSlot > 4)) {
    throw new Error("firebase latest sample slot is invalid");
  }
  const fencedMeta: FirebaseFencedMeta = {
    ...meta,
    writerGeneration,
    sampleEpoch,
    sampleState: "active",
  };
  const fencedSample: FirebaseCrashSample = {
    ...sample,
    groupCount: meta.count,
    writerGeneration,
    sampleEpoch,
  };
  const root = `groups/${encodeURIComponent(meta.fingerprint)}`;
  await conditionalPut(env, `${root}/meta`, fencedMeta, metaDecision(fencedMeta), fetcher, beforeWrite);
  if (firstSample) {
    await conditionalPut(
      env, `${root}/samples/first`, fencedSample, sampleDecision(fencedSample, true), fetcher, beforeWrite,
    );
  }
  if (latestSlot !== null) {
    await conditionalPut(
      env, `${root}/samples/latest/${latestSlot}`, fencedSample, sampleDecision(fencedSample, false), fetcher,
      beforeWrite,
    );
  }
}

export async function readFirebaseCrashGroup(
  env: Env,
  fingerprint: string,
  fetcher: Fetcher = fetch,
): Promise<FirebaseCrashGroup | null> {
  const response = await firebaseFetch(
    env,
    `groups/${encodeURIComponent(fingerprint)}`,
    { method: "GET" },
    fetcher,
  );
  if (!response.ok) throw new Error(`firebase database read failed with ${response.status}`);
  const value = await response.json() as unknown;
  if (value === null) return null;
  if (typeof value !== "object" || Array.isArray(value)) {
    throw new Error("firebase database group response is invalid");
  }
  return value as FirebaseCrashGroup;
}

export async function writeFirebaseGroupMeta(
  env: Env,
  fingerprint: string,
  meta: FirebaseCrashGroupMeta,
  writerGeneration: number,
  sampleEpoch: number,
  sampleState: FirebaseFencedMeta["sampleState"],
  beforeWrite: () => Promise<boolean> = async () => true,
  fetcher: Fetcher = fetch,
): Promise<void> {
  const candidate: FirebaseFencedMeta = { ...meta, writerGeneration, sampleEpoch, sampleState };
  await conditionalPut(
    env,
    `groups/${encodeURIComponent(fingerprint)}/meta`,
    candidate,
    metaDecision(candidate),
    fetcher,
    beforeWrite,
  );
}

export async function writeFirebaseSampleMarkers(
  env: Env,
  fingerprint: string,
  groupCount: number,
  writerGeneration: number,
  sampleEpoch: number,
  marker: FirebaseSampleMarker["marker"],
  includeFirst: boolean,
  beforeWrite: () => Promise<boolean> = async () => true,
  fetcher: Fetcher = fetch,
): Promise<void> {
  const candidate: FirebaseSampleMarker = {
    marker, groupCount, writerGeneration, sampleEpoch,
  };
  const root = `groups/${encodeURIComponent(fingerprint)}/samples`;
  const paths = Array.from({ length: 5 }, (_, slot) => `${root}/latest/${slot}`);
  if (includeFirst) paths.unshift(`${root}/first`);
  for (const path of paths) {
    await conditionalPut(env, path, candidate, sampleDecision(candidate, false), fetcher, beforeWrite);
  }
}

export async function deleteFirebaseCrashGroup(
  env: Env,
  fingerprint: string,
  fetcher: Fetcher = fetch,
): Promise<void> {
  const response = await firebaseFetch(
    env,
    `groups/${encodeURIComponent(fingerprint)}`,
    { method: "DELETE" },
    fetcher,
  );
  if (!response.ok) throw new Error(`firebase database delete failed with ${response.status}`);
}

export async function deleteFirebaseCrashGroupConditional(
  env: Env,
  fingerprint: string,
  beforeDelete: () => Promise<boolean>,
  fetcher: Fetcher = fetch,
): Promise<void> {
  const path = `groups/${encodeURIComponent(fingerprint)}`;
  for (let attempt = 0; attempt < 3; attempt++) {
    const current = await firebaseFetch(env, path, {
      method: "GET",
      headers: { "X-Firebase-ETag": "true" },
    }, fetcher);
    if (!current.ok) throw new Error(`firebase database delete precondition read failed with ${current.status}`);
    const etag = current.headers.get("etag");
    if (!etag) throw new Error("firebase database delete precondition read omitted ETag");
    if (!await beforeDelete()) throw new FirebaseFenceError();
    const response = await firebaseFetch(env, `${path}?print=silent`, {
      method: "DELETE",
      headers: { "If-Match": etag },
    }, fetcher);
    if (response.status === 412) continue;
    if (!response.ok) throw new Error(`firebase database conditional delete failed with ${response.status}`);
    return;
  }
  throw new Error("firebase database conditional delete retry limit reached");
}

export function resetFirebaseAuthForTests(): void {
  cachedToken = undefined;
  tokenRequest = undefined;
}
