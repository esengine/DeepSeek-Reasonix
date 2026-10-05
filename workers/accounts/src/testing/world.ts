import app from "../app";
import type { Bindings } from "../env";
import { repos } from "../db";
import { hashPassword } from "../auth/crypto";
import { enrollProofMessage, thumbprintOf, type ControllerJwk } from "../auth/controllerProof";
import { sqliteD1, type SqliteD1 } from "./sqliteD1";

// Workers expose timingSafeEqual on SubtleCrypto; Node does not.
const subtle = crypto.subtle as unknown as { timingSafeEqual?: (a: Uint8Array, b: Uint8Array) => boolean };
subtle.timingSafeEqual ??= (a, b) => a.byteLength === b.byteLength && a.every((byte, index) => byte === b[index]);

export const PASSWORD = "correct horse battery";

export interface Account {
  id: number;
  cookie: string;
  sessionHash: string;
}

export interface Host {
  id: string;
  credential: string;
}

export interface Phone {
  jwk: ControllerJwk;
  thumbprint: string;
  sign(message: Uint8Array): Promise<string>;
}

function base64url(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export async function newPhone(): Promise<Phone> {
  const pair = (await crypto.subtle.generateKey({ name: "ECDSA", namedCurve: "P-256" }, true, ["sign", "verify"])) as CryptoKeyPair;
  const exported = (await crypto.subtle.exportKey("jwk", pair.publicKey)) as JsonWebKey;
  const jwk: ControllerJwk = { kty: "EC", crv: "P-256", x: exported.x as string, y: exported.y as string };
  return {
    jwk,
    thumbprint: await thumbprintOf(jwk),
    async sign(message) {
      const signature = await crypto.subtle.sign({ name: "ECDSA", hash: "SHA-256" }, pair.privateKey, message);
      return base64url(new Uint8Array(signature));
    },
  };
}

export interface Response2 {
  status: number;
  body: any;
}

export class World {
  readonly store: SqliteD1;
  readonly env: Bindings;
  private counter = 0;

  constructor(options: { fail?: (sql: string) => boolean; bindings?: Partial<Bindings> } = {}) {
    this.store = sqliteD1({ fail: options.fail });
    this.env = {
      DB: this.store.db,
      APP_ORIGIN: "https://reasonix.io",
      ACCOUNT_ORIGIN: "https://id.reasonix.io",
      REMOTE_GATEWAY_ORIGIN: "https://remote.reasonix.io",
      ALLOWED_ORIGINS: "https://reasonix.io",
      COOKIE_DOMAIN: ".reasonix.io",
      EMAIL_PROVIDER: "stub",
      MAIL_FROM: "Reasonix <test@example.com>",
      SESSION_PEPPER: "pepper",
      ...options.bindings,
    };
  }

  get repos() {
    return repos(this.env);
  }

  async addAccount(): Promise<Account & { email: string }> {
    this.counter += 1;
    const email = `user${this.counter}@example.com`;
    const user = await this.repos.users.create({
      handle: `user${this.counter}`, email, passwordHash: await hashPassword(PASSWORD), displayName: "", role: "member",
    });
    return { ...(await this.addSession(user.id)), email };
  }

  async addSession(userId: number, kind: "web" | "cli" = "web"): Promise<Account> {
    const token = await this.repos.sessions.create(userId, { kind });
    const resolved = await this.repos.sessions.resolveSession(token);
    return { id: userId, cookie: `rxid=${token}`, sessionHash: resolved!.session.id };
  }

  async addHost(userId: number): Promise<Host> {
    this.counter += 1;
    const key = base64url(crypto.getRandomValues(new Uint8Array(32)));
    const { device, deviceCredential } = await this.repos.remoteDevices.register({
      userId, name: `host ${this.counter}`, platform: "macos", publicKey: key, capabilities: ["terminal", "tasks"],
    });
    return { id: device.id, credential: deviceCredential };
  }

  async call(method: string, path: string, init: { cookie?: string; body?: unknown; headers?: Record<string, string> } = {}): Promise<Response2> {
    const headers: Record<string, string> = { ...(init.headers ?? {}) };
    if (init.cookie) headers.cookie = init.cookie;
    if (init.body !== undefined) headers["content-type"] = "application/json";
    const response = await app.request(path, {
      method, headers, ...(init.body !== undefined ? { body: JSON.stringify(init.body) } : {}),
    }, this.env);
    const text = await response.text();
    return { status: response.status, body: text ? JSON.parse(text) : null };
  }

  hostCall(method: string, path: string, host: Host, body?: unknown): Promise<Response2> {
    return this.call(method, path, {
      body,
      headers: { "x-reasonix-device-id": host.id, "x-reasonix-device-credential": host.credential },
    });
  }

  async challenge(account: Account, hostId: string): Promise<string> {
    const response = await this.call("POST", "/me/remote-controllers/challenge", {
      cookie: account.cookie, body: { host: hostId },
    });
    if (response.status !== 200) throw new Error(`challenge failed: ${JSON.stringify(response.body)}`);
    return response.body.nonce as string;
  }

  async enroll(
    account: Account,
    phone: Phone,
    hostId: string,
    options: {
      nonce?: string;
      claimsId?: string;
      uaClass?: string;
      signAs?: { userId?: number; hostDeviceId?: string; thumbprint?: string; nonce?: string };
      extra?: Record<string, unknown>;
    } = {},
  ): Promise<Response2> {
    const nonce = options.nonce ?? (await this.challenge(account, hostId));
    const signed = options.signAs ?? {};
    const message = enrollProofMessage({
      nonce: signed.nonce ?? nonce,
      userId: signed.userId ?? account.id,
      hostDeviceId: signed.hostDeviceId ?? hostId,
      thumbprint: signed.thumbprint ?? phone.thumbprint,
    });
    return this.call("POST", "/me/remote-controllers/enroll", {
      cookie: account.cookie,
      body: {
        host: hostId, nonce, publicKey: phone.jwk, signature: await phone.sign(message),
        uaClass: options.uaClass ?? "ios-safari",
        ...(options.claimsId ? { claimsId: options.claimsId } : {}),
        ...options.extra,
      },
    });
  }

  rows<T = Record<string, unknown>>(sql: string, ...params: unknown[]): T[] {
    return this.store.raw.prepare(sql).all(...params) as T[];
  }
}
