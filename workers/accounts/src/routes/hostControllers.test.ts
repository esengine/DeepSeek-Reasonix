import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { World, newPhone, type Account, type Host } from "../testing/world";
import { applyMigrations, migrationsDir, sqliteD1 } from "../testing/sqliteD1";

async function setup() {
  const world = new World();
  const owner = await world.addAccount();
  const host = await world.addHost(owner.id);
  const first = (await world.enroll(owner, await newPhone(), host.id)).body.controller;
  return { world, owner, host, first };
}

async function requestPending(world: World, owner: Account, host: Host, claimsId?: string) {
  const session = await world.addSession(owner.id);
  const phone = await newPhone();
  const response = await world.enroll(session, phone, host.id, { claimsId });
  expect(response.body.error.code).toBe("controller_unconfirmed");
  return { session, phone, id: response.body.controller.id as string };
}

const resolve = (world: World, host: Host, id: string, body: unknown) =>
  world.hostCall("POST", `/host/controllers/${id}/resolve`, host, body);

describe("host-credential authority", () => {
  it("lists pending requests with the key the host must verify", async () => {
    const { world, owner, host } = await setup();
    const pending = await requestPending(world, owner, host);
    const listed = await world.hostCall("GET", "/host/controllers", host);
    expect(listed.status).toBe(200);
    expect(listed.body.controllers).toHaveLength(1);
    expect(listed.body.controllers[0]).toMatchObject({
      id: pending.id, state: "pending", keyThumbprint: pending.phone.thumbprint, publicKey: pending.phone.jwk,
    });
    expect(listed.body.controllers[0].replaceCandidate).toBeTruthy();
  });

  it("refuses an account session, a wrong credential and another host's credential", async () => {
    const { world, owner, host } = await setup();
    const sibling = await world.addHost(owner.id);
    const stranger = await world.addAccount();
    const strangerHost = await world.addHost(stranger.id);
    const pending = await requestPending(world, owner, host);
    const path = `/host/controllers/${pending.id}/resolve`;

    const asUser = await world.call("POST", path, { cookie: owner.cookie, body: { action: "add" } });
    expect(asUser.status).toBe(401);
    expect(asUser.body.error.code).toBe("invalid_device");
    const wrong = await world.hostCall("POST", path, { ...host, credential: "0".repeat(64) }, { action: "add" });
    expect(wrong.status).toBe(401);
    expect((await world.hostCall("POST", path, sibling, { action: "add" })).status).toBe(404);
    expect((await world.hostCall("POST", path, strangerHost, { action: "add" })).status).toBe(404);
    expect(world.rows("SELECT 1 FROM remote_controllers WHERE state = 'pending'")).toHaveLength(1);
    expect((await world.call("GET", "/host/controllers", { cookie: owner.cookie })).status).toBe(401);
  });

  it("refuses a revoked host's credential", async () => {
    const { world, owner, host } = await setup();
    const pending = await requestPending(world, owner, host);
    await world.call("DELETE", `/me/devices/${host.id}`, { cookie: owner.cookie });
    expect((await resolve(world, host, pending.id, { action: "add" })).status).toBe(401);
  });
});

describe("resolving a pending controller", () => {
  it("adds it as a new active device with the next ordinal and leaves the old one", async () => {
    const { world, owner, host, first } = await setup();
    const pending = await requestPending(world, owner, host, first.id);
    const response = await resolve(world, host, pending.id, { action: "add" });
    expect(response.status).toBe(200);
    expect(response.body.controller).toMatchObject({ id: pending.id, state: "active", ordinal: 2 });
    expect(world.rows<{ state: string }>("SELECT state FROM remote_controllers WHERE id = ?1", first.id)[0]?.state).toBe("active");
    expect((await resolve(world, host, pending.id, { action: "add" })).body.error.code).toBe("controller_not_pending");
  });

  it("replaces the claimed device: it is revoked and nothing is inherited", async () => {
    const { world, owner, host, first } = await setup();
    const pending = await requestPending(world, owner, host, first.id);
    const response = await resolve(world, host, pending.id, { action: "replace", replaceId: first.id });
    expect(response.status).toBe(200);
    expect(response.body.controller).toMatchObject({ id: pending.id, state: "active", ordinal: 2 });
    expect(response.body.revoked.id).toBe(first.id);
    const old = world.rows<{ state: string; ordinal: number; revoked_at: string }>("SELECT state, ordinal, revoked_at FROM remote_controllers WHERE id = ?1", first.id)[0]!;
    expect(old).toMatchObject({ state: "revoked", ordinal: 1 });
    expect(old.revoked_at).toBeTruthy();
  });

  it("refuses a replacement target that is not an active device of this host", async () => {
    const { world, owner, host } = await setup();
    const pending = await requestPending(world, owner, host);
    const other = await world.addHost(owner.id);
    const foreign = (await world.enroll(owner, await newPhone(), other.id)).body.controller;
    const response = await resolve(world, host, pending.id, { action: "replace", replaceId: foreign.id });
    expect(response.status).toBe(409);
    expect(response.body.error.code).toBe("replace_target_invalid");
    expect(world.rows<{ state: string }>("SELECT state FROM remote_controllers WHERE id = ?1", pending.id)[0]?.state).toBe("pending");
    expect(world.rows<{ state: string }>("SELECT state FROM remote_controllers WHERE id = ?1", foreign.id)[0]?.state).toBe("active");
  });

  it("rejects: the request becomes revoked for good and its key cannot enroll again", async () => {
    const { world, owner, host } = await setup();
    const pending = await requestPending(world, owner, host);
    const rejected = await resolve(world, host, pending.id, { action: "reject" });
    expect(rejected.status).toBe(200);
    expect(rejected.body.controller.state).toBe("revoked");
    const again = await world.enroll(await world.addSession(owner.id), pending.phone, host.id);
    expect(again.body.error.code).toBe("controller_revoked");
    expect(world.rows("SELECT 1 FROM remote_controllers WHERE state = 'active'")).toHaveLength(1);
  });

  it("locks pending creation for a requester session after repeated rejects, not for the account", async () => {
    const { world, owner, host } = await setup();
    const requester = await world.addSession(owner.id);
    for (let index = 0; index < 3; index += 1) {
      world.store.raw.exec("DELETE FROM remote_rate_counters WHERE counter_key LIKE 'enroll:%' OR counter_key LIKE 'challenge:%'");
      const response = await world.enroll(requester, await newPhone(), host.id);
      expect(response.body.error.code).toBe("controller_unconfirmed");
      await resolve(world, host, response.body.controller.id, { action: "reject" });
    }
    world.store.raw.exec("DELETE FROM remote_rate_counters WHERE counter_key LIKE 'enroll:%' OR counter_key LIKE 'challenge:%'");
    const locked = await world.enroll(requester, await newPhone(), host.id);
    expect(locked.status).toBe(429);
    expect(locked.body.error.code).toBe("pending_locked");
    const other = await world.addSession(owner.id);
    expect((await world.enroll(other, await newPhone(), host.id)).body.error.code).toBe("controller_unconfirmed");
  });

  it("expires a pending request on resolve and on listing, revoking it", async () => {
    const { world, owner, host } = await setup();
    const one = await requestPending(world, owner, host);
    const two = await requestPending(world, owner, host);
    world.store.raw.prepare("UPDATE remote_controllers SET expires_at = ?1 WHERE state = 'pending'").run(new Date(Date.now() - 1000).toISOString());
    const late = await resolve(world, host, one.id, { action: "add" });
    expect(late.status).toBe(410);
    expect(late.body.error.code).toBe("pending_expired");
    expect(world.rows<{ state: string }>("SELECT state FROM remote_controllers WHERE id = ?1", one.id)[0]?.state).toBe("revoked");
    const listed = await world.hostCall("GET", "/host/controllers", host);
    expect(listed.body.controllers).toEqual([]);
    expect(world.rows<{ state: string }>("SELECT state FROM remote_controllers WHERE id = ?1", two.id)[0]?.state).toBe("revoked");
  });

  it("requires an explicit replaceId for replace", async () => {
    const { world, owner, host } = await setup();
    const pending = await requestPending(world, owner, host);
    const response = await resolve(world, host, pending.id, { action: "replace" });
    expect(response.status).toBe(422);
    expect(response.body.error.code).toBe("invalid_input");
    expect(world.rows<{ state: string }>("SELECT state FROM remote_controllers WHERE id = ?1", pending.id)[0]?.state).toBe("pending");
  });

  it("refuses an unknown action", async () => {
    const { world, owner, host } = await setup();
    const pending = await requestPending(world, owner, host);
    expect((await resolve(world, host, pending.id, { action: "approve" })).body.error.code).toBe("invalid_input");
  });
});

describe("the device cap", () => {
  async function fillToCap() {
    const ctx = await setup();
    const ids = [ctx.first.id as string];
    for (let index = 1; index < 8; index += 1) {
      const pending = await requestPending(ctx.world, ctx.owner, ctx.host);
      await resolve(ctx.world, ctx.host, pending.id, { action: "add" });
      ids.push(pending.id);
    }
    return { ...ctx, ids };
  }

  it("sends the ninth device to pending and refuses to add it, but allows replacing the least recently used", async () => {
    const { world, owner, host, ids } = await fillToCap();
    expect(world.rows("SELECT 1 FROM remote_controllers WHERE state = 'active'")).toHaveLength(8);
    world.store.raw.prepare("UPDATE remote_controllers SET last_seen_at = ?1 WHERE id = ?2").run(new Date(Date.now() - 3600_000).toISOString(), ids[3]);

    const ninth = await requestPending(world, owner, host);
    const listed = await world.hostCall("GET", "/host/controllers", host);
    expect(listed.body.controllers[0].replaceCandidate).toBe(ids[3]);
    const added = await resolve(world, host, ninth.id, { action: "add" });
    expect(added.status).toBe(409);
    expect(added.body.error.code).toBe("controller_cap_reached");
    expect(world.rows<{ state: string }>("SELECT state FROM remote_controllers WHERE id = ?1", ninth.id)[0]?.state).toBe("pending");

    const replaced = await resolve(world, host, ninth.id, { action: "replace", replaceId: ids[3] });
    expect(replaced.status).toBe(200);
    expect(replaced.body.revoked.id).toBe(ids[3]);
    expect(replaced.body.controller.ordinal).toBe(9);
    expect(world.rows("SELECT 1 FROM remote_controllers WHERE state = 'active'")).toHaveLength(8);
  });

  it("does not count a device idle for a month, so it can be added beside it", async () => {
    const { world, owner, host, ids } = await fillToCap();
    world.store.raw.prepare("UPDATE remote_controllers SET last_seen_at = ?1 WHERE id = ?2").run(new Date(Date.now() - 40 * 86400_000).toISOString(), ids[0]);
    const ninth = await requestPending(world, owner, host);
    const added = await resolve(world, host, ninth.id, { action: "add" });
    expect(added.status).toBe(200);
    expect(world.rows<{ state: string }>("SELECT state FROM remote_controllers WHERE id = ?1", ids[0])[0]?.state).toBe("active");
  });
});

describe("host-initiated revoke", () => {
  it("revokes only controllers of its own host", async () => {
    const { world, owner, host, first } = await setup();
    const sibling = await world.addHost(owner.id);
    expect((await world.hostCall("POST", `/host/controllers/${first.id}/revoke`, sibling, {})).status).toBe(404);
    expect(world.rows<{ state: string }>("SELECT state FROM remote_controllers")[0]?.state).toBe("active");
    expect((await world.hostCall("POST", `/host/controllers/${first.id}/revoke`, host, {})).status).toBe(200);
    expect(world.rows<{ state: string }>("SELECT state FROM remote_controllers")[0]?.state).toBe("revoked");
  });
});

describe("rate limits are D1 counters", () => {
  it("limits challenges to 30 a minute per session without any limiter binding", async () => {
    const { world, owner, host } = await setup();
    const session = await world.addSession(owner.id);
    const results = [];
    for (let index = 0; index < 31; index += 1) {
      results.push((await world.call("POST", "/me/remote-controllers/challenge", { cookie: session.cookie, body: { host: host.id } })).status);
    }
    expect(results.slice(0, 29).every((status) => status === 200)).toBe(true);
    expect(results[29]).toBe(200);
    expect(results[30]).toBe(429);
    const refused = await world.call("POST", "/me/remote-controllers/challenge", { cookie: session.cookie, body: { host: host.id } });
    expect(refused.body.error.code).toBe("rate_limited");
    const other = await world.addSession(owner.id);
    expect((await world.call("POST", "/me/remote-controllers/challenge", { cookie: other.cookie, body: { host: host.id } })).status).toBe(200);
  });

  it("limits enrollment attempts to 5 an hour per session, counting failures", async () => {
    const { world, owner, host } = await setup();
    const phone = await newPhone();
    const session = await world.addSession(owner.id);
    const statuses = [];
    for (let index = 0; index < 6; index += 1) {
      statuses.push((await world.enroll(session, phone, host.id, { signAs: { userId: 999 } })).status);
    }
    expect(statuses.slice(0, 5)).toEqual([403, 403, 403, 403, 403]);
    expect(statuses[5]).toBe(429);
  });

  it("starts a new window when the old one has passed", async () => {
    const { world } = await setup();
    const counters = world.repos.rateCounters;
    const start = Date.parse("2026-10-05T00:00:00.000Z");
    expect((await counters.hit("probe:1", 60_000, start)).count).toBe(1);
    expect((await counters.hit("probe:1", 60_000, start + 10_000)).count).toBe(2);
    expect((await counters.hit("probe:1", 60_000, start + 61_000)).count).toBe(1);
    expect((await counters.hit("probe:2", 60_000, start)).count).toBe(1);
  });

  it("fails closed when the counter store cannot be written", async () => {
    const world = new World({ fail: (sql) => sql.includes("remote_rate_counters") });
    const owner = await world.addAccount();
    const host = await world.addHost(owner.id);
    const challenge = await world.call("POST", "/me/remote-controllers/challenge", { cookie: owner.cookie, body: { host: host.id } });
    expect(challenge.status).toBe(503);
    expect(challenge.body.error.code).toBe("rate_limit_unavailable");
    expect(world.rows("SELECT 1 FROM remote_controller_challenges")).toHaveLength(0);
    const enroll = await world.enroll(owner, await newPhone(), host.id, { nonce: "a".repeat(64) });
    expect(enroll.status).toBe(503);
    expect(world.rows("SELECT 1 FROM remote_controllers")).toHaveLength(0);
  });

  it("fails closed when the database binding or the key is missing", async () => {
    const counters = (await import("../db/rateCounters")).RateCounterRepo;
    await expect(new counters(undefined as unknown as D1Database).hit("k", 1000, Date.now())).rejects.toThrow();
    const { enforceRateLimit } = await import("../http/d1RateLimit");
    const rule = { name: "x", limit: 5, windowMs: 1000 };
    await expect(enforceRateLimit({ DB: undefined as unknown as D1Database }, rule, "subject"))
      .rejects.toMatchObject({ status: 503, code: "rate_limit_unavailable" });
    const { db } = sqliteD1();
    await expect(enforceRateLimit({ DB: db }, rule, "")).rejects.toMatchObject({ status: 503, code: "rate_limit_unavailable" });
  });

  it("limits host resolve calls per host", async () => {
    const { world, host } = await setup();
    let last = 0;
    for (let index = 0; index < 61; index += 1) {
      last = (await world.hostCall("GET", "/host/controllers", host)).status;
    }
    expect(last).toBe(429);
  });
});

describe("migration 0007", () => {
  const sql = readFileSync(`${migrationsDir()}0007_remote_controllers.sql`, "utf8");

  it("is additive and idempotent", () => {
    expect(sql.replace(/--.*$/gm, "")).not.toMatch(/^\s*(?:DROP|ALTER|DELETE|UPDATE|INSERT)\b/im);
    const { raw } = sqliteD1();
    expect(() => raw.exec(sql)).not.toThrow();
    expect(() => raw.exec(sql)).not.toThrow();
  });

  it("applies on top of the earlier schema and keeps existing rows", () => {
    const { raw } = sqliteD1({ migrate: false });
    applyMigrations(raw, "0006_remote_grant_sessions.sql");
    raw.exec("INSERT INTO users (handle, email, created_at, updated_at) VALUES ('a', 'a@x', 't', 't')");
    raw.exec(sql);
    expect(raw.prepare("SELECT COUNT(*) AS n FROM users").get()).toEqual({ n: 1 });
  });

  it("refuses an active row without an ordinal, a revoked row without a time, and a duplicate ordinal", () => {
    const { raw } = sqliteD1();
    const insert = (id: string, thumb: string, ordinal: number | null, state: string, revokedAt: string | null) =>
      raw.prepare(`INSERT INTO remote_controllers (id, user_id, host_device_id, key_thumbprint, public_key, ordinal, name, state, requester_session_hash, created_at, last_seen_at, revoked_at)
        VALUES (?1, 1, 'h', ?2, '{}', ?3, 'n', ?4, 's', 't', 't', ?5)`).run(id, thumb, ordinal, state, revokedAt);
    expect(() => insert("a", "t1", null, "active", null)).toThrow();
    expect(() => insert("b", "t2", null, "revoked", null)).toThrow();
    expect(() => insert("c", "t3", 1, "active", null)).not.toThrow();
    expect(() => insert("d", "t4", 1, "active", null)).toThrow();
    expect(() => insert("e", "t3", 2, "active", null)).toThrow();
  });

  it("refuses at table level to revive a revoked row or change its revoked_at", () => {
    const { raw } = sqliteD1();
    raw.prepare(`INSERT INTO remote_controllers (id, user_id, host_device_id, key_thumbprint, public_key, ordinal, name, state, requester_session_hash, created_at, last_seen_at, revoked_at)
      VALUES ('r', 1, 'h', 't', '{}', 1, 'n', 'revoked', 's', 't', 't', '2026-01-01')`).run();
    expect(() => raw.exec("UPDATE remote_controllers SET state = 'active', revoked_at = NULL")).toThrow();
    expect(() => raw.exec("UPDATE remote_controllers SET revoked_at = '2027-01-01'")).toThrow();
    expect(() => raw.exec("UPDATE remote_controllers SET state = 'pending'")).toThrow();
    expect(() => raw.exec("UPDATE remote_controllers SET last_seen_at = 'later'")).not.toThrow();
  });
});
