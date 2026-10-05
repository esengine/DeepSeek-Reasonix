import { describe, expect, it } from "vitest";
import { controllerIdFor } from "../auth/controllerProof";
import { World, newPhone } from "../testing/world";

const hasNoGrantFields = (body: unknown) =>
  expect(JSON.stringify(body)).not.toMatch(/ticket|"grant"|scopes|capabilities/);

async function setup() {
  const world = new World();
  const owner = await world.addAccount();
  const host = await world.addHost(owner.id);
  return { world, owner, host };
}

describe("controller challenge", () => {
  it("issues a 60 second nonce stored only as a hash", async () => {
    const { world, owner, host } = await setup();
    const response = await world.call("POST", "/me/remote-controllers/challenge", { cookie: owner.cookie, body: { host: host.id } });
    expect(response.status).toBe(200);
    const { nonce, expiresAt } = response.body;
    expect(nonce).toMatch(/^[0-9a-f]{64}$/);
    expect(Date.parse(expiresAt) - Date.now()).toBeLessThanOrEqual(60_000);
    expect(Date.parse(expiresAt) - Date.now()).toBeGreaterThan(55_000);
    const rows = world.rows<Record<string, unknown>>("SELECT * FROM remote_controller_challenges");
    expect(rows).toHaveLength(1);
    expect(JSON.stringify(rows)).not.toContain(nonce);
    expect(rows[0]).toMatchObject({ user_id: owner.id, host_device_id: host.id, session_hash: owner.sessionHash, purpose: "enroll" });
  });

  it("needs a signed-in browser session and one of the caller's own active hosts", async () => {
    const { world, owner, host } = await setup();
    const stranger = await world.addAccount();
    expect((await world.call("POST", "/me/remote-controllers/challenge", { body: { host: host.id } })).status).toBe(401);
    const foreign = await world.call("POST", "/me/remote-controllers/challenge", { cookie: stranger.cookie, body: { host: host.id } });
    const missing = await world.call("POST", "/me/remote-controllers/challenge", { cookie: owner.cookie, body: { host: "f".repeat(64) } });
    expect(foreign.status).toBe(404);
    expect(foreign.body).toEqual(missing.body);
    expect(foreign.body.error.code).toBe("device_not_found");
    const cli = await world.addSession(owner.id, "cli");
    const refused = await world.call("POST", "/me/remote-controllers/challenge", { cookie: cli.cookie, body: { host: host.id } });
    expect(refused.status).toBe(403);
    expect(refused.body.error.code).toBe("remote_reauth_required");
    expect(world.rows("SELECT 1 FROM remote_controller_challenges")).toHaveLength(0);
  });

  it("asks for a fresh sign-in once the session is older than the reauth window", async () => {
    const { world, owner, host } = await setup();
    world.store.raw.prepare("UPDATE sessions SET created_at = ?1").run(new Date(Date.now() - 25 * 3600_000).toISOString());
    const refused = await world.call("POST", "/me/remote-controllers/challenge", { cookie: owner.cookie, body: { host: host.id } });
    expect(refused.body.error.code).toBe("remote_reauth_required");
  });
});

describe("controller enrollment proof", () => {
  it("auto-enrolls the first device with a derived id, ordinal 1 and no grant", async () => {
    const { world, owner, host } = await setup();
    const phone = await newPhone();
    const response = await world.enroll(owner, phone, host.id);
    expect(response.status).toBe(200);
    expect(response.body.controller).toMatchObject({
      id: await controllerIdFor(host.id, phone.thumbprint), ordinal: 1, name: "iPhone Safari", state: "active",
    });
    hasNoGrantFields(response.body);
  });

  it("burns a nonce on first use, so a replayed enrollment is refused", async () => {
    const { world, owner, host } = await setup();
    const phone = await newPhone();
    const nonce = await world.challenge(owner, host.id);
    expect((await world.enroll(owner, phone, host.id, { nonce })).status).toBe(200);
    const replay = await world.enroll(owner, phone, host.id, { nonce });
    expect(replay.status).toBe(403);
    expect(replay.body.error.code).toBe("challenge_invalid");
  });

  it("refuses an expired nonce and does not let it be used again", async () => {
    const { world, owner, host } = await setup();
    const phone = await newPhone();
    const nonce = await world.challenge(owner, host.id);
    world.store.raw.prepare("UPDATE remote_controller_challenges SET expires_at = ?1").run(new Date(Date.now() - 1000).toISOString());
    const late = await world.enroll(owner, phone, host.id, { nonce });
    expect(late.status).toBe(403);
    expect(late.body.error.code).toBe("challenge_expired");
    expect(world.rows("SELECT 1 FROM remote_controllers")).toHaveLength(0);
    expect((await world.enroll(owner, phone, host.id, { nonce })).body.error.code).toBe("challenge_invalid");
  });

  it("binds a nonce to its user, host and session without letting a stranger burn it", async () => {
    const { world, owner, host } = await setup();
    const otherHost = await world.addHost(owner.id);
    const stranger = await world.addAccount();
    const strangerHost = await world.addHost(stranger.id);
    const otherSession = await world.addSession(owner.id);
    const phone = await newPhone();
    const nonce = await world.challenge(owner, host.id);

    const asStranger = await world.enroll(stranger, phone, strangerHost.id, { nonce });
    expect(asStranger.body.error.code).toBe("challenge_invalid");
    const onStrangerHost = await world.enroll(stranger, phone, host.id, { nonce });
    expect(onStrangerHost.status).toBe(404);
    const onOtherHost = await world.enroll(owner, phone, otherHost.id, { nonce });
    expect(onOtherHost.body.error.code).toBe("challenge_invalid");
    const fromOtherSession = await world.enroll(otherSession, phone, host.id, { nonce });
    expect(fromOtherSession.body.error.code).toBe("challenge_invalid");

    const real = await world.enroll(owner, phone, host.id, { nonce });
    expect(real.status).toBe(200);
  });

  it.each([
    ["another user id", { userId: 999 }],
    ["another host id", { hostDeviceId: "e".repeat(64) }],
    ["another thumbprint", { thumbprint: "A".repeat(43) }],
    ["another nonce", { nonce: "9".repeat(64) }],
  ])("refuses a signature over %s and still burns the nonce", async (_name, signAs) => {
    const { world, owner, host } = await setup();
    const phone = await newPhone();
    const nonce = await world.challenge(owner, host.id);
    const refused = await world.enroll(owner, phone, host.id, { nonce, signAs });
    expect(refused.status).toBe(403);
    expect(refused.body.error.code).toBe("invalid_proof");
    expect(world.rows("SELECT 1 FROM remote_controllers")).toHaveLength(0);
    expect((await world.enroll(owner, phone, host.id, { nonce })).body.error.code).toBe("challenge_invalid");
  });

  it("refuses a proof made with a different key than the one submitted", async () => {
    const { world, owner, host } = await setup();
    const real = await newPhone();
    const attacker = await newPhone();
    const nonce = await world.challenge(owner, host.id);
    const forged = await world.enroll(owner, real, host.id, { nonce, extra: { publicKey: attacker.jwk } });
    expect(forged.body.error.code).toBe("invalid_proof");
  });

  it("never trusts a client-supplied thumbprint or id", async () => {
    const { world, owner, host } = await setup();
    const phone = await newPhone();
    const withThumbprint = await world.enroll(owner, phone, host.id, { extra: { keyThumbprint: phone.thumbprint } });
    expect(withThumbprint.body.error.code).toBe("invalid_input");
    const wrongId = await world.enroll(owner, phone, host.id, { extra: { controllerId: `rc_${"A".repeat(43)}` } });
    expect(wrongId.status).toBe(422);
    expect(wrongId.body.error.code).toBe("controller_id_mismatch");
    const rightId = await world.enroll(owner, phone, host.id, { extra: { controllerId: await controllerIdFor(host.id, phone.thumbprint) } });
    expect(rightId.status).toBe(200);
  });

  it.each([
    ["a private member", (jwk: any) => ({ ...jwk, d: jwk.x })],
    ["ext", (jwk: any) => ({ ...jwk, ext: true })],
    ["key_ops", (jwk: any) => ({ ...jwk, key_ops: ["verify"] })],
    ["an extra member", (jwk: any) => ({ ...jwk, kid: "1" })],
    ["another curve", (jwk: any) => ({ ...jwk, crv: "P-384" })],
    ["a short coordinate", (jwk: any) => ({ ...jwk, x: "AAAA" })],
  ])("rejects a JWK with %s", async (_name, mutate) => {
    const { world, owner, host } = await setup();
    const phone = await newPhone();
    const response = await world.enroll(owner, phone, host.id, { extra: { publicKey: mutate(phone.jwk) } });
    expect(response.status).toBe(422);
    expect(response.body.error.code).toBe("invalid_input");
    expect(world.rows("SELECT 1 FROM remote_controllers")).toHaveLength(0);
  });

  it("rejects a point that is not on the curve", async () => {
    const { world, owner, host } = await setup();
    const phone = await newPhone();
    const response = await world.enroll(owner, phone, host.id, { extra: { publicKey: { ...phone.jwk, y: phone.jwk.x } } });
    expect(response.status).toBe(422);
    expect(response.body.error.code).toBe("invalid_key");
  });

  it("clamps an unknown user-agent class instead of storing or showing it", async () => {
    const { world, owner, host } = await setup();
    const response = await world.enroll(owner, await newPhone(), host.id, { uaClass: "<script>alert(1)</script>" });
    expect(response.body.controller.name).toBe("Other browser");
    expect(world.rows<{ ua_class: string }>("SELECT ua_class FROM remote_controllers")[0]?.ua_class).toBe("other");
  });
});

describe("controller enrollment state", () => {
  it("returns the same row to a racing second tab", async () => {
    const { world, owner, host } = await setup();
    const phone = await newPhone();
    const [one, two] = await Promise.all([world.enroll(owner, phone, host.id), world.enroll(owner, phone, host.id)]);
    expect(one.status).toBe(200);
    expect(two.body.controller).toMatchObject({ id: one.body.controller.id, ordinal: 1, state: "active" });
    expect(world.rows("SELECT 1 FROM remote_controllers")).toHaveLength(1);
    const again = await world.enroll(owner, phone, host.id);
    expect(again.body.controller.ordinal).toBe(1);
  });

  it("holds a further device as pending with no ordinal, grant or scopes", async () => {
    const { world, owner, host } = await setup();
    const first = await newPhone();
    const second = await newPhone();
    const enrolled = (await world.enroll(owner, first, host.id)).body.controller;
    const held = await world.enroll(owner, second, host.id, { claimsId: enrolled.id });
    expect(held.status).toBe(403);
    expect(held.body.error.code).toBe("controller_unconfirmed");
    expect(held.body.controller).toMatchObject({
      id: await controllerIdFor(host.id, second.thumbprint), state: "pending", ordinal: null, claimsId: enrolled.id,
    });
    hasNoGrantFields(held.body);
    expect(await world.repos.remoteControllers.activeById(owner.id, host.id, held.body.controller.id)).toBeNull();
    const again = await world.enroll(owner, second, host.id);
    expect(again.body.controller.id).toBe(held.body.controller.id);
    expect(world.rows("SELECT 1 FROM remote_controllers WHERE state = 'pending'")).toHaveLength(1);
  });

  it("treats a claim that names no active row of this owner and host as no claim", async () => {
    const { world, owner, host } = await setup();
    const other = await world.addAccount();
    const otherHost = await world.addHost(other.id);
    const theirs = (await world.enroll(other, await newPhone(), otherHost.id)).body.controller;
    await world.enroll(owner, await newPhone(), host.id);
    const unknown = await world.enroll(owner, await newPhone(), host.id, { claimsId: `rc_${"B".repeat(43)}` });
    const foreign = await world.enroll(owner, await newPhone(), host.id, { claimsId: theirs.id });
    expect(unknown.body.error.code).toBe("controller_unconfirmed");
    expect(foreign.body.error.code).toBe("controller_unconfirmed");
    expect(unknown.body.controller.claimsId).toBeNull();
    expect(foreign.body.controller.claimsId).toBeNull();
  });

  it("caps pending requests per host and keeps an existing one readable", async () => {
    const { world, owner, host } = await setup();
    await world.enroll(owner, await newPhone(), host.id);
    const held = [] as Awaited<ReturnType<typeof newPhone>>[];
    for (let index = 0; index < 3; index += 1) {
      const phone = await newPhone();
      held.push(phone);
      const session = await world.addSession(owner.id);
      expect((await world.enroll(session, phone, host.id)).body.error.code).toBe("controller_unconfirmed");
    }
    const fourth = await world.enroll(await world.addSession(owner.id), await newPhone(), host.id);
    expect(fourth.status).toBe(429);
    expect(fourth.body.error.code).toBe("pending_limit");
    expect((await world.enroll(await world.addSession(owner.id), held[0]!, host.id)).body.error.code).toBe("controller_unconfirmed");
    expect(world.rows("SELECT 1 FROM remote_controllers WHERE state = 'pending'")).toHaveLength(3);
  });

  it("keeps ordinals monotonic and never reuses one after a revoke", async () => {
    const { world, owner, host } = await setup();
    const ordinals: number[] = [];
    const ids: string[] = [];
    for (let index = 0; index < 3; index += 1) {
      const phone = await newPhone();
      const response = await world.enroll(owner, phone, host.id);
      const controller = response.body.controller;
      const id = controller.id as string;
      if (controller.state === "pending") {
        const resolved = await world.hostCall("POST", `/host/controllers/${id}/resolve`, host, { action: "add" });
        ordinals.push(resolved.body.controller.ordinal);
      } else {
        ordinals.push(controller.ordinal);
      }
      ids.push(id);
    }
    expect(ordinals).toEqual([1, 2, 3]);
    await world.call("POST", `/me/remote-controllers/${ids[2]}/revoke`, { cookie: owner.cookie, body: {} });
    await world.call("POST", `/me/remote-controllers/${ids[1]}/revoke`, { cookie: owner.cookie, body: {} });
    const next = await world.enroll(owner, await newPhone(), host.id);
    const resolved = await world.hostCall("POST", `/host/controllers/${next.body.controller.id}/resolve`, host, { action: "add" });
    expect(resolved.body.controller.ordinal).toBe(4);
    expect(world.rows<{ ordinal: number }>("SELECT ordinal FROM remote_controllers WHERE state = 'revoked' ORDER BY ordinal").map((r) => r.ordinal)).toEqual([2, 3]);
  });

  it("scopes ordinals to each host", async () => {
    const { world, owner, host } = await setup();
    const second = await world.addHost(owner.id);
    expect((await world.enroll(owner, await newPhone(), host.id)).body.controller.ordinal).toBe(1);
    expect((await world.enroll(owner, await newPhone(), second.id)).body.controller.ordinal).toBe(1);
  });
});

describe("controller revocation is terminal", () => {
  it("refuses to re-enroll a revoked key and never clears revoked_at", async () => {
    const { world, owner, host } = await setup();
    const phone = await newPhone();
    const id = (await world.enroll(owner, phone, host.id)).body.controller.id as string;
    const revoked = await world.call("POST", `/me/remote-controllers/${id}/revoke`, { cookie: owner.cookie, body: {} });
    expect(revoked.status).toBe(200);
    const before = world.rows<{ revoked_at: string }>("SELECT revoked_at FROM remote_controllers")[0]!.revoked_at;
    expect(before).toBeTruthy();

    const again = await world.enroll(owner, phone, host.id);
    expect(again.status).toBe(403);
    expect(again.body.error.code).toBe("controller_revoked");
    expect(again.body.controller.state).toBe("revoked");
    const rows = world.rows<{ state: string; revoked_at: string }>("SELECT state, revoked_at FROM remote_controllers");
    expect(rows).toEqual([{ state: "revoked", revoked_at: before }]);

    const aliased = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
    const x = phone.jwk.x;
    const alias = x.slice(0, -1) + aliased[aliased.indexOf(x.slice(-1)) ^ 1];
    const sneaky = await world.enroll(owner, phone, host.id, { extra: { publicKey: { ...phone.jwk, x: alias } } });
    expect(sneaky.status).toBe(422);
    expect(sneaky.body.error.code).toBe("invalid_key");
    expect(world.rows("SELECT 1 FROM remote_controllers")).toHaveLength(1);
  });

  it("holds a new key as pending after the only device was revoked, even from a stolen session", async () => {
    const { world, owner, host } = await setup();
    const id = (await world.enroll(owner, await newPhone(), host.id)).body.controller.id as string;
    const thief = await world.addSession(owner.id);
    await world.call("POST", `/me/remote-controllers/${id}/revoke`, { cookie: thief.cookie, body: {} });
    const attempt = await world.enroll(thief, await newPhone(), host.id);
    expect(attempt.status).toBe(403);
    expect(attempt.body.error.code).toBe("controller_unconfirmed");
    expect(world.rows("SELECT 1 FROM remote_controllers WHERE state = 'active'")).toHaveLength(0);
  });

  it("still auto-enrolls the first device of a host that never had one", async () => {
    const { world, owner, host } = await setup();
    const fresh = await world.addHost(owner.id);
    expect((await world.enroll(owner, await newPhone(), fresh.id)).body.controller.state).toBe("active");
    expect(host.id).not.toBe(fresh.id);
  });

  it("makes revoking idempotent and answers other owners with not found", async () => {
    const { world, owner, host } = await setup();
    const stranger = await world.addAccount();
    const id = (await world.enroll(owner, await newPhone(), host.id)).body.controller.id as string;
    expect((await world.call("POST", `/me/remote-controllers/${id}/revoke`, { cookie: stranger.cookie, body: {} })).status).toBe(404);
    expect(world.rows("SELECT 1 FROM remote_controllers WHERE state = 'active'")).toHaveLength(1);
    expect((await world.call("POST", `/me/remote-controllers/${id}/revoke`, { cookie: owner.cookie, body: {} })).status).toBe(200);
    expect((await world.call("POST", `/me/remote-controllers/${id}/revoke`, { cookie: owner.cookie, body: {} })).status).toBe(200);
    expect((await world.call("POST", `/me/remote-controllers/rc_${"C".repeat(43)}/revoke`, { cookie: owner.cookie, body: {} })).status).toBe(404);
  });

  it("lists only the owner's controllers for the owner's host", async () => {
    const { world, owner, host } = await setup();
    const stranger = await world.addAccount();
    const id = (await world.enroll(owner, await newPhone(), host.id)).body.controller.id as string;
    const listed = await world.call("GET", `/me/remote-controllers?host=${host.id}`, { cookie: owner.cookie });
    expect(listed.body.controllers.map((c: any) => c.id)).toEqual([id]);
    expect(JSON.stringify(listed.body)).not.toContain("publicKey");
    expect((await world.call("GET", `/me/remote-controllers?host=${host.id}`, { cookie: stranger.cookie })).status).toBe(404);
    expect((await world.call("GET", `/me/remote-controllers?host=${host.id}`)).status).toBe(401);
  });
});

describe("account changes revoke every enrollment", () => {
  async function enrolled() {
    const ctx = await setup();
    const first = (await ctx.world.enroll(ctx.owner, await newPhone(), ctx.host.id)).body.controller.id as string;
    const pending = (await ctx.world.enroll(ctx.owner, await newPhone(), ctx.host.id)).body.controller.id as string;
    return { ...ctx, first, pending };
  }
  const states = (world: World) => world.rows<{ state: string; revoked_at: string | null }>("SELECT state, revoked_at FROM remote_controllers");

  it("on a password change", async () => {
    const { world, owner } = await enrolled();
    const response = await world.call("POST", "/me/password", { cookie: owner.cookie, body: { currentPassword: "correct horse battery", newPassword: "another long password" } });
    expect(response.status).toBe(200);
    expect(states(world).every((row) => row.state === "revoked" && row.revoked_at)).toBe(true);
  });

  it("on a password reset", async () => {
    const { world, owner } = await enrolled();
    const token = await world.repos.emailTokens.issue(owner.id, "reset", 60_000);
    const response = await world.call("POST", "/auth/reset", { body: { token, password: "another long password" } });
    expect(response.status).toBe(200);
    expect(states(world).every((row) => row.state === "revoked")).toBe(true);
  });

  it("on account deletion, and leaves other accounts alone", async () => {
    const { world, owner } = await enrolled();
    const bystander = await world.addAccount();
    const bystanderHost = await world.addHost(bystander.id);
    await world.enroll(bystander, await newPhone(), bystanderHost.id);
    expect((await world.call("DELETE", "/me", { cookie: owner.cookie })).status).toBe(200);
    const rows = world.rows<{ user_id: number; state: string }>("SELECT user_id, state FROM remote_controllers");
    expect(rows.filter((row) => row.user_id === owner.id).every((row) => row.state === "revoked")).toBe(true);
    expect(rows.filter((row) => row.user_id === bystander.id).map((row) => row.state)).toEqual(["active"]);
  });

  it("keeps a revoked enrollment revoked after the host registers again", async () => {
    const { world, owner, host, first } = await enrolled();
    await world.call("POST", "/me/password", { cookie: owner.cookie, body: { currentPassword: "correct horse battery", newPassword: "another long password" } });
    const key = world.rows<{ public_key: string }>("SELECT public_key FROM remote_devices")[0]!.public_key;
    const again = await world.repos.remoteDevices.register({ userId: owner.id, name: "Home", platform: "macos", publicKey: key, capabilities: ["terminal"] });
    expect(again.device.id).toBe(host.id);
    expect(world.rows<{ state: string }>("SELECT state FROM remote_controllers WHERE id = ?1", first)[0]?.state).toBe("revoked");
  });
});
