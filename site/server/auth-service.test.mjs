import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { createAuthService, hashPassword, verifyPassword } from "./auth-service.mjs";
import { createPlatformStore } from "./platform-store.mjs";

async function fixture(options = {}) {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-auth-"));
  const store = createPlatformStore({ dbPath: path.join(directory, "auth.sqlite") });
  const auth = createAuthService({ store, secureCookies: false, ...options });
  await auth.bootstrap({ workspaceId: "WS-A", workspaceName: "澜图科技", email: "owner@example.com", password: "Correct-Horse-2026", name: "林越" });
  return { store, auth, async close() { store.close(); await rm(directory, { recursive: true, force: true }); } };
}

test("hashes passwords with scrypt and verifies without retaining plaintext", async () => {
  const encoded = await hashPassword("Correct-Horse-2026");
  assert.match(encoded, /^scrypt\$/);
  assert.doesNotMatch(encoded, /Correct-Horse/);
  assert.equal(await verifyPassword("Correct-Horse-2026", encoded), true);
  assert.equal(await verifyPassword("Wrong-Horse-2026", encoded), false);
});

test("bootstraps an owner and creates an opaque HttpOnly session", async () => {
  const fx = await fixture();
  try {
    const loggedIn = await fx.auth.login({ email: "owner@example.com", password: "Correct-Horse-2026" });
    assert.match(loggedIn.setCookie, /^intelifar_session=/);
    assert.match(loggedIn.setCookie, /HttpOnly/);
    assert.match(loggedIn.setCookie, /SameSite=Lax/);
    assert.doesNotMatch(loggedIn.setCookie, /Correct-Horse/);
    const token = loggedIn.setCookie.match(/^intelifar_session=([^;]+)/)[1];
    const stored = fx.store.unsafeDatabaseForTests.prepare("SELECT token_hash FROM sessions").get();
    assert.notEqual(stored.token_hash, token);

    const request = { headers: { cookie: `intelifar_session=${token}` } };
    const session = fx.auth.getSessionFromRequest(request);
    assert.equal(session.user.role, "owner");
    assert.equal(session.workspace.id, "WS-A");
  } finally {
    await fx.close();
  }
});

test("returns one generic failure for unknown users and bad passwords", async () => {
  const fx = await fixture();
  try {
    for (const input of [
      { email: "missing@example.com", password: "Wrong-Horse-2026" },
      { email: "owner@example.com", password: "Wrong-Horse-2026" },
    ]) {
      await assert.rejects(() => fx.auth.login(input), (error) => error.code === "INVALID_CREDENTIALS" && error.message === "Email or password is incorrect");
    }
  } finally {
    await fx.close();
  }
});

test("expires sessions and enforces the simple role hierarchy", async () => {
  let clock = Date.parse("2026-08-10T08:00:00.000Z");
  const fx = await fixture({ now: () => new Date(clock), sessionTtlMs: 1_000 });
  try {
    const loggedIn = await fx.auth.login({ email: "owner@example.com", password: "Correct-Horse-2026" });
    const token = loggedIn.setCookie.match(/^intelifar_session=([^;]+)/)[1];
    assert.equal(fx.auth.getSessionFromRequest({ headers: { cookie: `intelifar_session=${token}` } }).user.role, "owner");
    clock += 2_000;
    assert.equal(fx.auth.getSessionFromRequest({ headers: { cookie: `intelifar_session=${token}` } }), null);

    assert.equal(fx.auth.can("owner", "editor"), true);
    assert.equal(fx.auth.can("editor", "viewer"), true);
    assert.equal(fx.auth.can("viewer", "editor"), false);
  } finally {
    await fx.close();
  }
});

test("logout revokes the current token and emits an expiring cookie", async () => {
  const fx = await fixture();
  try {
    const loggedIn = await fx.auth.login({ email: "owner@example.com", password: "Correct-Horse-2026" });
    const token = loggedIn.setCookie.match(/^intelifar_session=([^;]+)/)[1];
    const request = { headers: { cookie: `intelifar_session=${token}` } };
    const result = fx.auth.logout(request);
    assert.equal(result.revoked, true);
    assert.match(result.setCookie, /Max-Age=0/);
    assert.equal(fx.auth.getSessionFromRequest(request), null);
  } finally {
    await fx.close();
  }
});

test("creates a one-time invitation and lets the recipient set their own password", async () => {
  let clock = Date.parse("2026-08-10T08:00:00.000Z");
  const fx = await fixture({ now: () => new Date(clock) });
  try {
    const created = fx.auth.createInvitation({ workspaceId: "WS-A", email: "editor@example.com", name: "陈澈", role: "editor", invitedBy: fx.store.getUserByEmail("owner@example.com").id });
    assert.match(created.token, /^[A-Za-z0-9_-]{40,}$/);
    assert.doesNotMatch(JSON.stringify(created.invitation), new RegExp(created.token));
    assert.equal(fx.auth.inspectInvitation(created.token).email, "editor@example.com");
    const accepted = await fx.auth.acceptInvitation({ token: created.token, password: "Editor-Horse-2026" });
    assert.equal(accepted.user.role, "editor");
    assert.equal(fx.auth.inspectInvitation(created.token), null);
    const login = await fx.auth.login({ email: "editor@example.com", password: "Editor-Horse-2026" });
    assert.match(login.setCookie, /HttpOnly/);
    await assert.rejects(() => fx.auth.acceptInvitation({ token: created.token, password: "Another-Horse-2026" }), (error) => error.code === "INVITATION_UNAVAILABLE");
  } finally {
    await fx.close();
  }
});

test("expires invitations and never permits an invited owner", async () => {
  let clock = Date.parse("2026-08-10T08:00:00.000Z");
  const fx = await fixture({ now: () => new Date(clock) });
  try {
    assert.throws(() => fx.auth.createInvitation({ workspaceId: "WS-A", email: "owner2@example.com", name: "第二所有者", role: "owner" }), (error) => error.code === "INVALID_ROLE");
    const created = fx.auth.createInvitation({ workspaceId: "WS-A", email: "viewer@example.com", name: "访客", role: "viewer", ttlMs: 60_000 });
    clock += 61_000;
    assert.equal(fx.auth.inspectInvitation(created.token), null);
  } finally {
    await fx.close();
  }
});
