import { describe, expect, it } from "vitest";
import { cleanTitle, frame, MAX_FRAME_BYTES, type OpsEvent } from "./events";
import { mapGithubEvent, verifySignature } from "./github";
import { HubCore, RING_MAX, type Store } from "./hub_core";
import { validateEmit } from "./index";

const opts = { ignoreLogins: new Set(["github-actions[bot]", "esengine"]), ciWorkflows: new Set(["CI"]) };
const user = { login: "someone", type: "User" };

function memoryStore(): Store & { data: Map<string, unknown> } {
  const data = new Map<string, unknown>();
  return {
    data,
    async get<T>(k: string) { return data.get(k) as T | undefined; },
    async put(k: string, v: unknown) { data.set(k, v); },
    async delete(keys: string | string[]) { for (const k of Array.isArray(keys) ? keys : [keys]) data.delete(k); },
    async list<T>(o: { prefix: string; start?: string; limit?: number }) {
      const keys = [...data.keys()].filter((k) => k.startsWith(o.prefix) && (!o.start || k >= o.start)).sort().slice(0, o.limit ?? 1000);
      return new Map(keys.map((k) => [k, data.get(k) as T]));
    },
  };
}

async function sign(secret: string, body: string): Promise<string> {
  const key = await crypto.subtle.importKey("raw", new TextEncoder().encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const mac = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(body));
  return `sha256=${[...new Uint8Array(mac)].map((b) => b.toString(16).padStart(2, "0")).join("")}`;
}

describe("signature", () => {
  it("accepts the right MAC and rejects wrong, missing and tampered bodies", async () => {
    const body = '{"a":1}';
    const good = await sign("s3cret", body);
    expect(await verifySignature("s3cret", body, good)).toBe(true);
    expect(await verifySignature("s3cret", body, await sign("other", body))).toBe(false);
    expect(await verifySignature("s3cret", '{"a":2}', good)).toBe(false);
    expect(await verifySignature("s3cret", body, null)).toBe(false);
    expect(await verifySignature("", body, good)).toBe(false);
    expect(await verifySignature("s3cret", body, "sha1=abc")).toBe(false);
  });
});

describe("mapping", () => {
  const repo = { full_name: "o/r" };
  it("maps an opened issue and never copies the body", () => {
    const e = mapGithubEvent("issues", { action: "opened", repository: repo, sender: user, issue: { number: 7, title: "Bug\nwith\u0000 control", body: "SECRET BODY", html_url: "https://github.com/o/r/issues/7" } }, opts)!;
    expect(e).toMatchObject({ src: "github", t: "issue.opened", n: 7, by: "someone", repo: "o/r", title: "Bug with control" });
    expect(JSON.stringify(e)).not.toContain("SECRET");
  });
  it("ignores bot and owner comments, keeps user comments", () => {
    const issue = { number: 1, title: "t", html_url: "u" };
    const mk = (sender: object) => mapGithubEvent("issue_comment", { action: "created", repository: repo, sender, issue, comment: { html_url: "c", body: "x" } }, opts);
    expect(mk({ login: "github-actions[bot]", type: "Bot" })).toBeNull();
    expect(mk({ login: "esengine", type: "User" })).toBeNull();
    expect(mk(user)?.t).toBe("comment");
  });
  it("maps merged and closed pull requests and head sha", () => {
    const pr = (merged: boolean) => mapGithubEvent("pull_request", { action: "closed", repository: repo, sender: user, pull_request: { number: 3, title: "x", merged, draft: false, base: { ref: "studio" }, head: { sha: "0123456789abcdef" }, html_url: "u" } }, opts)!;
    expect(pr(true).t).toBe("pr.merged");
    expect(pr(false).t).toBe("pr.closed");
    expect(pr(true).extra).toMatchObject({ base: "studio", head: "012345678" });
  });
  it("emits CI results only for the configured workflows", () => {
    const run = (name: string) => mapGithubEvent("workflow_run", { action: "completed", repository: repo, sender: user, workflow_run: { name, conclusion: "failure", head_branch: "b", head_sha: "abcdef0123456", html_url: "u", pull_requests: [{ number: 9 }] } }, opts);
    expect(run("CI")).toMatchObject({ t: "ci.failure", n: 9 });
    expect(run("CodeQL")).toBeNull();
  });
  it("maps repository advisories to ids only, never the summary or description", () => {
    const adv = { ghsa_id: "GHSA-aaaa-bbbb-cccc", severity: "high", state: "draft", summary: "SECRET SUMMARY", description: "SECRET DETAIL", html_url: "https://github.com/o/r/security/advisories/GHSA-aaaa-bbbb-cccc" };
    const mk = (action: string) => mapGithubEvent("repository_advisory", { action, repository: repo, sender: user, repository_advisory: adv }, opts);
    const e = mk("reported")!;
    expect(e).toMatchObject({ t: "advisory.reported", repo: "o/r", url: adv.html_url, extra: { ghsa: "GHSA-aaaa-bbbb-cccc", severity: "high", state: "draft" } });
    expect(e.title).toBeUndefined();
    expect(JSON.stringify(e)).not.toContain("SECRET");
    expect(mk("published")?.t).toBe("advisory.published");
    expect(mk("withdrawn")).toBeNull();
    expect(mapGithubEvent("repository_advisory", { action: "reported", repository: repo, sender: user }, opts)?.extra).toMatchObject({ ghsa: "", severity: "" });
  });
  it("ignores unlisted events and actions", () => {
    expect(mapGithubEvent("push", { repository: repo }, opts)).toBeNull();
    expect(mapGithubEvent("issues", { action: "labeled", repository: repo, sender: user, issue: {} }, opts)).toBeNull();
  });
});

describe("frames", () => {
  it("strips controls, truncates and bounds the frame", () => {
    expect(cleanTitle("a b\n c")).toBe("a b c");
    expect(cleanTitle("x".repeat(500))!.length).toBe(120);
    const big: OpsEvent = { id: 1, ts: "2026-10-03T00:00:00.000Z", src: "github", t: "issue.opened", repo: "o/r", n: 1, title: "y".repeat(120), by: "b", url: "https://github.com/o/r/issues/1", extra: { k: "z".repeat(80) } };
    expect(new TextEncoder().encode(frame(big)).length).toBeLessThanOrEqual(MAX_FRAME_BYTES);
  });
});

describe("hub core", () => {
  it("assigns increasing ids, dedupes deliveries and replays only undelivered events", async () => {
    const store = memoryStore();
    const core = new HubCore(store, () => Date.parse("2026-10-03T00:00:00Z"));
    const a = await core.push({ src: "x", t: "a" }, { delivery: "d1" });
    const dup = await core.push({ src: "x", t: "a" }, { delivery: "d1" });
    const b = await core.push({ src: "x", t: "b" });
    expect(a.event!.id).toBe(1);
    expect(dup).toMatchObject({ accepted: false, reason: "duplicate" });
    expect(b.event!.id).toBe(2);
    expect((await core.replay("auto")).map((e) => e.t)).toEqual(["a", "b"]);
    await core.markDelivered(1);
    expect((await core.replay("auto")).map((e) => e.t)).toEqual(["b"]);
    expect((await core.replay("0")).map((e) => e.t)).toEqual(["a", "b"]);
  });
  it("keeps at most the ring size and drops events older than seven days", async () => {
    const store = memoryStore();
    let now = Date.parse("2026-10-03T00:00:00Z");
    const core = new HubCore(store, () => now);
    for (let i = 0; i < RING_MAX + 20; i += 1) await core.push({ src: "x", t: "e" });
    expect((await core.replay("0")).length).toBe(RING_MAX);
    now += 8 * 24 * 3600 * 1000;
    await core.push({ src: "x", t: "fresh" });
    const ids = (await core.replay("0")).map((e) => e.id);
    expect(ids.length).toBeLessThan(RING_MAX + 1);
  });
  it("limits emits to 60 per minute", async () => {
    const core = new HubCore(memoryStore(), () => 1_000_000);
    let last = 0;
    for (let i = 0; i < 61; i += 1) last = (await core.push({ src: "x", t: "e" }, { rateKey: "emit" })).accepted ? last : i;
    expect(last).toBe(60);
  });
});

describe("emit validation", () => {
  it("accepts a compact event and rejects bad types, urls and big extras", () => {
    expect(validateEmit({ t: "feedback.submitted", src: "feedback", extra: { receipt: "FB-1", category: "bug" } })).not.toBeNull();
    expect(validateEmit({ t: "Bad Type", src: "feedback" })).toBeNull();
    expect(validateEmit({ t: "a", src: "b", url: "http://x" })).toBeNull();
    expect(validateEmit({ t: "a", src: "b", extra: { nested: { x: 1 } } })).toBeNull();
    expect(validateEmit({ t: "a", src: "b", extra: Object.fromEntries(Array.from({ length: 11 }, (_, i) => [`k${i}`, 1])) })).toBeNull();
    expect(validateEmit("x")).toBeNull();
  });
});
