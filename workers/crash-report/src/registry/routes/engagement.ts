import { Hono } from "hono";
import type { Context } from "hono";
import type { AppEnv } from "../env";
import type { PackageRow, RegistryUser } from "../types";
import { repos } from "../db";
import { rescore } from "../db/ranking";
import type { VoteValue } from "../db/votes";
import { requireAuth, currentUser } from "../http/auth";
import { writeRateLimit } from "../http/ratelimit";
import { ApiError } from "../http/errors";
import { approvalRate } from "../lib/ranking";
import { InstallPingSchema, VoteSchema, parseBody } from "../lib/validation";

// Votes, the legacy star toggle, and the anonymous install ping: the signals
// the recommended order is computed from (lib/ranking.ts).
const engagement = new Hono<AppEnv>();

const now = () => new Date().toISOString();

// Install-count thresholds worth announcing in the activity feed.
const MILESTONES = new Set([10, 50, 100, 500, 1000]);
const isMilestone = (n: number) => MILESTONES.has(n) || (n >= 1000 && n % 1000 === 0);

const slugOf = (c: Context<AppEnv>) => `${c.req.param("handle")}/${c.req.param("name")}`;

async function activePackage(c: Context<AppEnv>): Promise<PackageRow> {
  const row = await repos(c.env).packages.bySlug(slugOf(c));
  if (!row || row.status !== "active") throw new ApiError(404, "not_found", "No such package.");
  return row;
}

// Who may vote: a verified account that does not own the package. Admins get
// no exception — a moderator's own package is still their own.
function assertVoter(user: RegistryUser, pkg: PackageRow): void {
  if (!user.emailVerified) {
    throw new ApiError(403, "email_unverified", "Verify your email at id.reasonix.io before voting.");
  }
  if (pkg.publisher_id === user.id) {
    throw new ApiError(403, "own_package", "Publishers cannot vote on their own packages.");
  }
}

async function castVote(c: Context<AppEnv>, value: VoteValue) {
  const user = currentUser(c);
  const pkg = await activePackage(c);
  assertVoter(user, pkg);
  const { votes, events } = repos(c.env);
  const at = now();
  const tally = await votes.cast(pkg, user.id, value, at);
  await rescore(c.env.DB, at.slice(0, 10), pkg.id);
  // Only an account's first up-vote is announced: naming down-voters would turn
  // the feed into a pile-on, and re-announcing a toggled vote would spam it.
  if (tally.value === 1 && tally.previous !== 1 && !(await events.announced("star", pkg.id, user.handle))) {
    await events.log({ type: "star", packageId: pkg.id, actorHandle: user.handle, summary: `upvoted ${pkg.slug}`, now: at });
  }
  return tally;
}

const voteView = (value: VoteValue, up: number, down: number) => ({
  value,
  upCount: up,
  downCount: down,
  approvalRate: approvalRate(up, down),
});

engagement.get("/:handle/:name/vote", requireAuth, async (c) => {
  const pkg = await activePackage(c);
  const user = currentUser(c);
  const value = await repos(c.env).votes.get(pkg.id, user.id);
  return c.json({
    ...voteView(value, pkg.up_count, pkg.down_count),
    canVote: user.emailVerified && pkg.publisher_id !== user.id,
    own: pkg.publisher_id === user.id,
    emailVerified: user.emailVerified,
  });
});

engagement.post("/:handle/:name/vote", writeRateLimit, requireAuth, async (c) => {
  const { value } = await parseBody(c, VoteSchema);
  const tally = await castVote(c, value);
  return c.json(voteView(tally.value, tally.up, tally.down));
});

// Legacy star toggle, kept for clients that predate votes: it flips between a
// +1 vote and no vote, under the same rules.
engagement.post("/:handle/:name/star", writeRateLimit, requireAuth, async (c) => {
  const pkg = await activePackage(c);
  const current = await repos(c.env).votes.get(pkg.id, currentUser(c).id);
  const tally = await castVote(c, current === 1 ? 0 : 1);
  return c.json({ starred: tally.value === 1, count: tally.up });
});

// Anonymous install ping. Counted once per (installId, package, UTC day); a
// ping without a well-formed installId is acknowledged and never counted.
engagement.post("/:handle/:name/installed", writeRateLimit, async (c) => {
  const slug = slugOf(c);
  const body = InstallPingSchema.safeParse(await c.req.json().catch(() => null));
  const { packages: repo, installs, events } = repos(c.env);
  if (!body.success) {
    const row = await repo.bySlug(slug);
    if (!row || row.status !== "active") throw new ApiError(404, "not_found", "No such package.");
    return c.json({ ok: true, counted: false, installCount: row.install_count });
  }
  const at = now();
  const result = await installs.record(slug, body.data.installId, at);
  if (result === null) throw new ApiError(404, "not_found", "No such package.");
  if (result.counted) {
    await rescore(c.env.DB, at.slice(0, 10), result.packageId);
    if (isMilestone(result.count)) {
      await events.log({
        type: "milestone",
        packageId: result.packageId,
        actorHandle: result.scopeHandle,
        summary: `${slug} reached ${result.count} installs`,
        now: at,
      });
    }
  }
  return c.json({ ok: true, counted: result.counted, installCount: result.count });
});

export default engagement;
