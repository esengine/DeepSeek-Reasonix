import { Hono } from "hono";
import type { AppEnv } from "../env";
import { toPackageDTO } from "../types";
import { repos } from "../db";
import { currentUser, requireAdmin } from "../http/auth";
import { writeRateLimit } from "../http/ratelimit";
import { ApiError } from "../http/errors";
import { z } from "zod";
import { CONTENT_DIGEST } from "../lib/validation";
import { repinReviewedDigest } from "../pin";
import { rescore } from "../db/ranking";

const admin = new Hono<AppEnv>();

const now = () => new Date().toISOString();

const ApprovalRevisionSchema = z.object({
  expectedVersion: z.string().min(1).max(64),
  expectedUpdatedAt: z.string().min(1).max(64),
  expectedStatus: z.enum(["pending", "hidden", "rejected"]),
  contentHash: z.union([z.literal(""), z.string().regex(CONTENT_DIGEST)]).default(""),
});

const PinRevisionSchema = z.object({
  expectedVersion: z.string().min(1).max(64),
  expectedUpdatedAt: z.string().min(1).max(64),
  expectedStatus: z.literal("active"),
  contentHash: z.string().regex(CONTENT_DIGEST),
});

admin.use("*", requireAdmin);

// Review queue. ?status defaults to pending; also accepts rejected/hidden/active,
// and flagged: live packages the vote rules send back for another look.
admin.get("/packages", async (c) => {
  const status = new URL(c.req.url).searchParams.get("status") || "pending";
  const { packages: repo } = repos(c.env);
  const rows = status === "flagged" ? await repo.listFlagged(200) : await repo.listByStatus(status, 200);
  return c.json({ packages: rows.map(toPackageDTO) });
});

// Recompute every recommended score now instead of at the next cron run — the
// step that follows the votes migration.
admin.post("/rescore", writeRateLimit, async (c) => {
  const updated = await rescore(c.env.DB, now().slice(0, 10));
  return c.json({ updated });
});

// Approve a pending package → live. Its publish event is emitted here (on first
// approval), so the activity feed only ever announces public packages.
admin.post("/packages/:handle/:name/approve", writeRateLimit, async (c) => {
  const slug = `${c.req.param("handle")}/${c.req.param("name")}`;
  const { packages: repo, events } = repos(c.env);
  const revision = ApprovalRevisionSchema.safeParse(await c.req.json().catch(() => null));
  if (!revision.success) {
    throw new ApiError(400, "invalid_review_revision", "Approval requires the reviewed package revision.");
  }
  const approvedAt = now();
  const row = await repo.setStatusIfCurrent(
    slug,
    "active",
    revision.data.expectedVersion,
    revision.data.expectedUpdatedAt,
    revision.data.expectedStatus,
    approvedAt,
    revision.data.contentHash,
  );
  if (!row) {
    const current = await repo.bySlug(slug);
    if (!current) throw new ApiError(404, "not_found", "No such package.");
    throw new ApiError(
      409,
      "stale_review",
      "Package changed since it was reviewed. Refresh and review the latest version.",
    );
  }
  await events.log({
    type: "publish",
    packageId: row.id,
    actorHandle: row.scope_handle,
    summary: `published ${row.slug}@${row.latest_version}`,
    now: approvedAt,
  });
  return c.json({ package: toPackageDTO(row) });
});

// Bind (or replace) the reviewed digest on a live package's current version,
// for packages approved before a digest could be recorded. Status is unchanged.
admin.post("/packages/:handle/:name/pin", writeRateLimit, async (c) => {
  const slug = `${c.req.param("handle")}/${c.req.param("name")}`;
  const revision = PinRevisionSchema.safeParse(await c.req.json().catch(() => null));
  if (!revision.success) {
    throw new ApiError(
      400,
      "invalid_pin",
      "Pinning requires the live package revision and a sha256:<64 lowercase hex> content digest.",
    );
  }
  const result = await repinReviewedDigest(c.env.DB, {
    slug,
    expectedVersion: revision.data.expectedVersion,
    expectedUpdatedAt: revision.data.expectedUpdatedAt,
    contentHash: revision.data.contentHash,
    actor: currentUser(c).handle,
    now: now(),
  });
  if (!result) {
    const current = await repos(c.env).packages.bySlug(slug);
    if (!current) throw new ApiError(404, "not_found", "No such package.");
    throw new ApiError(
      409,
      "stale_review",
      "Package changed since it was reviewed. Refresh and review the latest version.",
    );
  }
  return c.json({
    package: toPackageDTO(result.row),
    previousContentHash: result.previous,
    contentHash: revision.data.contentHash,
  });
});

admin.post("/packages/:handle/:name/reject", writeRateLimit, async (c) => {
  const slug = `${c.req.param("handle")}/${c.req.param("name")}`;
  const row = await repos(c.env).packages.setStatus(slug, "rejected", now());
  if (!row) throw new ApiError(404, "not_found", "No such package.");
  return c.json({ package: toPackageDTO(row) });
});

// Take a previously-approved package back down.
admin.post("/packages/:handle/:name/hide", writeRateLimit, async (c) => {
  const slug = `${c.req.param("handle")}/${c.req.param("name")}`;
  const row = await repos(c.env).packages.setStatus(slug, "hidden", now());
  if (!row) throw new ApiError(404, "not_found", "No such package.");
  return c.json({ package: toPackageDTO(row) });
});

// Grant or revoke the verified badge. Body {verified:false} revokes; default grants.
admin.post("/packages/:handle/:name/verify", writeRateLimit, async (c) => {
  const slug = `${c.req.param("handle")}/${c.req.param("name")}`;
  const body = (await c.req.json().catch(() => ({}))) as { verified?: boolean };
  const row = await repos(c.env).packages.setVerified(slug, body.verified !== false, now());
  if (!row) throw new ApiError(404, "not_found", "No such package.");
  return c.json({ package: toPackageDTO(row) });
});

export default admin;
