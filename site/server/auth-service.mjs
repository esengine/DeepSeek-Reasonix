import { createHash, randomBytes, randomUUID, scrypt, timingSafeEqual } from "node:crypto";
import { promisify } from "node:util";

const scryptAsync = promisify(scrypt);
const COOKIE_NAME = "intelifar_session";
const ROLE_LEVEL = { viewer: 1, editor: 2, admin: 3, owner: 4 };
const SCRYPT_COST = 16_384;
const SCRYPT_BLOCK_SIZE = 8;
const SCRYPT_PARALLELIZATION = 1;
const KEY_LENGTH = 64;

function tokenHash(token) {
  return createHash("sha256").update(String(token)).digest("hex");
}

function headerValue(request, name) {
  if (typeof request?.headers?.get === "function") return request.headers.get(name) || "";
  return request?.headers?.[name] || request?.headers?.[name.toLowerCase()] || "";
}

function cookieToken(request) {
  const cookie = String(headerValue(request, "cookie"));
  for (const part of cookie.split(";")) {
    const [name, ...rest] = part.trim().split("=");
    if (name === COOKIE_NAME) return rest.join("=");
  }
  return "";
}

function publicUser(user) {
  return user ? { id: user.id, workspaceId: user.workspaceId, email: user.email, name: user.name, role: user.role, disabledAt: user.disabledAt ?? null } : null;
}

function invalidCredentials() {
  const error = new Error("Email or password is incorrect");
  error.code = "INVALID_CREDENTIALS";
  return error;
}

function normalizeEmail(value) {
  const email = String(value ?? "").trim().toLocaleLowerCase("en-US");
  return email.length <= 254 && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email) ? email : "";
}

export async function hashPassword(password) {
  const normalized = String(password ?? "");
  if (normalized.length < 12 || normalized.length > 128) throw new Error("Password must contain 12 to 128 characters");
  const salt = randomBytes(16);
  const key = await scryptAsync(normalized, salt, KEY_LENGTH, { N: SCRYPT_COST, r: SCRYPT_BLOCK_SIZE, p: SCRYPT_PARALLELIZATION, maxmem: 64 * 1024 * 1024 });
  return ["scrypt", SCRYPT_COST, SCRYPT_BLOCK_SIZE, SCRYPT_PARALLELIZATION, salt.toString("base64url"), Buffer.from(key).toString("base64url")].join("$");
}

export async function verifyPassword(password, encoded) {
  try {
    const [algorithm, cost, blockSize, parallelization, saltEncoded, hashEncoded] = String(encoded).split("$");
    if (algorithm !== "scrypt") return false;
    const expected = Buffer.from(hashEncoded, "base64url");
    if (expected.length !== KEY_LENGTH) return false;
    const actual = await scryptAsync(String(password ?? ""), Buffer.from(saltEncoded, "base64url"), expected.length, {
      N: Number(cost),
      r: Number(blockSize),
      p: Number(parallelization),
      maxmem: 64 * 1024 * 1024,
    });
    return timingSafeEqual(Buffer.from(actual), expected);
  } catch {
    return false;
  }
}

export function createAuthService(options) {
  const { store } = options;
  if (!store) throw new Error("Authentication store is required");
  const sessionTtlMs = Math.max(1_000, Number(options.sessionTtlMs ?? 8 * 60 * 60_000));
  const secureCookies = options.secureCookies === true;
  const now = options.now ?? (() => new Date());
  const dummyHashPromise = hashPassword(`unused-${randomBytes(18).toString("base64url")}`);

  function setCookie(token, maxAgeSeconds) {
    return [
      `${COOKIE_NAME}=${token}`,
      "Path=/",
      "HttpOnly",
      "SameSite=Lax",
      secureCookies ? "Secure" : "",
      `Max-Age=${Math.max(0, Math.floor(maxAgeSeconds))}`,
    ].filter(Boolean).join("; ");
  }

  return {
    async bootstrap(input) {
      const email = normalizeEmail(input.email);
      if (!email) throw new Error("A valid bootstrap email is required");
      store.ensureWorkspace({ id: String(input.workspaceId), name: String(input.workspaceName) });
      const existing = store.getUserByEmail(email);
      if (existing) return publicUser(existing);
      const passwordHash = await hashPassword(input.password);
      const id = input.userId || `USR-${createHash("sha256").update(email).digest("hex").slice(0, 16).toUpperCase()}`;
      return publicUser(store.createUser({ id, workspaceId: String(input.workspaceId), email, name: String(input.name || "空间所有者").slice(0, 80), role: "owner", passwordHash }));
    },
    async login(input) {
      const email = normalizeEmail(input?.email);
      const password = String(input?.password ?? "");
      const user = email ? store.getUserByEmail(email) : null;
      const encoded = user?.passwordHash || await dummyHashPromise;
      const valid = password.length <= 128 && await verifyPassword(password, encoded);
      if (!user || user.disabledAt || !valid) throw invalidCredentials();

      store.pruneSessions(now().toISOString());
      const token = randomBytes(32).toString("base64url");
      const expiresAt = new Date(now().getTime() + sessionTtlMs).toISOString();
      store.createSession({ id: `SES-${randomUUID()}`, tokenHash: tokenHash(token), userId: user.id, expiresAt, createdAt: now().toISOString() });
      return {
        session: { user: publicUser(user), workspace: { id: user.workspaceId } },
        setCookie: setCookie(token, sessionTtlMs / 1000),
        expiresAt,
      };
    },
    getSessionFromRequest(request) {
      const token = cookieToken(request);
      return token ? store.getSession(tokenHash(token), now().toISOString()) : null;
    },
    logout(request) {
      const token = cookieToken(request);
      return { revoked: token ? store.deleteSession(tokenHash(token)) : false, setCookie: setCookie("", 0) };
    },
    createInvitation(input) {
      const email = normalizeEmail(input?.email);
      const name = String(input?.name ?? "").trim();
      const role = String(input?.role ?? "viewer");
      if (!email) throw new Error("A valid invitation email is required");
      if (!name || name.length > 80) throw new Error("Invitation name must contain 1 to 80 characters");
      if (!["admin", "editor", "viewer"].includes(role)) {
        const error = new Error("Invitation role must be admin, editor, or viewer");
        error.code = "INVALID_ROLE";
        throw error;
      }
      const createdAt = now().toISOString();
      const ttlMs = Math.max(60_000, Math.min(30 * 24 * 60 * 60_000, Number(input.ttlMs ?? 7 * 24 * 60 * 60_000)));
      const token = randomBytes(32).toString("base64url");
      const invitation = store.createInvitation(String(input.workspaceId), {
        id: `INV-${randomUUID()}`,
        email,
        name,
        role,
        tokenHash: tokenHash(token),
        invitedBy: input.invitedBy || null,
        createdAt,
        expiresAt: new Date(now().getTime() + ttlMs).toISOString(),
      });
      return { invitation, token, activationPath: `/activate/#${token}` };
    },
    inspectInvitation(token) {
      const value = String(token ?? "");
      if (value.length < 32 || value.length > 128) return null;
      return store.getInvitationByTokenHash(tokenHash(value), now().toISOString());
    },
    async acceptInvitation(input) {
      const token = String(input?.token ?? "");
      const invitation = this.inspectInvitation(token);
      if (!invitation) {
        const error = new Error("Invitation is invalid or expired");
        error.code = "INVITATION_UNAVAILABLE";
        throw error;
      }
      const passwordHash = await hashPassword(input?.password);
      try {
        return store.acceptInvitation(tokenHash(token), {
          userId: `USR-${randomUUID()}`,
          passwordHash,
          acceptedAt: now().toISOString(),
        });
      } catch (error) {
        if (String(error?.code || "").startsWith("SQLITE_CONSTRAINT")) {
          const conflict = new Error("Invitation cannot be accepted for this account");
          conflict.code = "INVITATION_CONFLICT";
          throw conflict;
        }
        throw error;
      }
    },
    can(actualRole, minimumRole) {
      return Number(ROLE_LEVEL[actualRole] || 0) >= Number(ROLE_LEVEL[minimumRole] || Number.POSITIVE_INFINITY);
    },
    cookieName: COOKIE_NAME,
  };
}
