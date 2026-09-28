import type { PackageKind, PackageRow, ReviewRow, VersionRow, RegistryUser } from "../types";
import type { PublishInput } from "../lib/validation";
import { ApiError } from "../http/errors";
import { FLAG_MAX_APPROVAL, FLAG_MIN_DOWN } from "../lib/ranking";

export interface ListParams {
  kind: PackageKind | "all";
  q: string;
  sort: "recommended" | "new" | "trending" | "installs";
  pinned: boolean;
  limit: number;
  offset: number;
  now: string;
}

export interface PublishResult {
  row: PackageRow;
  created: boolean;
  version: string;
}

export interface VersionListParams {
  limit: number;
  before?: string;
  beforeId?: number;
}

export interface VersionListResult {
  versions: VersionRow[];
  pageInfo: {
    limit: number;
    hasMore: boolean;
    nextBefore: string | null;
    nextBeforeId: number | null;
  };
}

// Mirrors CONTENT_DIGEST: the listed version has a reviewer-bound digest a
// client can install against. GLOB is case-sensitive, so uppercase hex fails as
// the regex does.
const PINNED_SQL = `EXISTS (
  SELECT 1 FROM package_versions v
  WHERE v.package_id = p.id AND v.version = p.latest_version
    AND length(v.content_hash) = 71 AND substr(v.content_hash, 1, 7) = 'sha256:'
    AND substr(v.content_hash, 8) NOT GLOB '*[^0-9a-f]*'
)`;

export class PackageRepo {
  constructor(private readonly db: D1Database) {}

  async list(p: ListParams): Promise<PackageRow[]> {
    const where: string[] = ["p.status = 'active'"];
    const binds: unknown[] = [];

    if (p.kind !== "all") {
      where.push(`p.kind = ?${binds.length + 1}`);
      binds.push(p.kind);
    }
    if (p.q) {
      const like = `%${p.q.toLowerCase()}%`;
      const a = binds.length + 1;
      where.push(`(lower(p.name) LIKE ?${a} OR lower(p.summary) LIKE ?${a + 1} OR lower(p.tags) LIKE ?${a + 2})`);
      binds.push(like, like, like);
    }
    if (p.pinned) where.push(PINNED_SQL);

    let select = `SELECT p.*, ${PINNED_SQL} AS pinned FROM packages p`;
    let order: string;
    if (p.sort === "trending") {
      select = `SELECT p.*, ${PINNED_SQL} AS pinned, COALESCE(e.c, 0) AS trend FROM packages p
        LEFT JOIN (
          SELECT package_id, SUM(count) AS c FROM package_install_daily
          WHERE date >= date(?${binds.length + 1}, '-6 day')
          GROUP BY package_id
        ) e ON e.package_id = p.id`;
      binds.push(p.now);
      order = "ORDER BY trend DESC, p.install_count DESC, p.created_at DESC";
    } else if (p.sort === "recommended") {
      order = "ORDER BY p.rec_score DESC, p.install_count DESC, p.created_at DESC, p.id DESC";
    } else if (p.sort === "installs") {
      order = "ORDER BY p.install_count DESC, p.created_at DESC";
    } else {
      order = "ORDER BY p.created_at DESC";
    }

    const sql = `${select} WHERE ${where.join(" AND ")} ${order} LIMIT ?${binds.length + 1} OFFSET ?${binds.length + 2}`;
    binds.push(p.limit, p.offset);

    const res = await this.db.prepare(sql).bind(...binds).all<PackageRow>();
    return res.results ?? [];
  }

  async bySlug(slug: string): Promise<PackageRow | null> {
    return this.db.prepare("SELECT * FROM packages WHERE slug = ?1").bind(slug).first<PackageRow>();
  }

  async versions(packageId: number, page: VersionListParams = { limit: 50 }): Promise<VersionListResult> {
    const cursor = page.before !== undefined && page.beforeId !== undefined;
    const sql = `SELECT id, version, source, content_hash, risk_level, created_at
         FROM package_versions WHERE package_id = ?1
         ${cursor ? "AND (created_at < ?2 OR (created_at = ?2 AND id < ?3))" : ""}
         ORDER BY created_at DESC, id DESC LIMIT ?${cursor ? "4" : "2"}`;
    const res = await this.db
      .prepare(sql)
      .bind(...(cursor ? [packageId, page.before, page.beforeId, page.limit + 1] : [packageId, page.limit + 1]))
      .all<VersionRow>();
    const rows = res.results ?? [];
    const hasMore = rows.length > page.limit;
    const versions = hasMore ? rows.slice(0, page.limit) : rows;
    const last = versions.at(-1);
    return {
      versions,
      pageInfo: {
        limit: page.limit,
        hasMore,
        nextBefore: hasMore ? last?.created_at ?? null : null,
        nextBeforeId: hasMore ? last?.id ?? null : null,
      },
    };
  }

  // Create a new package or append a version to an owned one. New packages and
  // updates from non-admins land as 'pending' (hidden until an admin approves);
  // visibility 'private' lands as 'private', outside the review queue entirely.
  // Every accepted update appends an immutable version, so its source, manifest,
  // metadata, and capability kind must all cross the same moderation boundary.
  // Republishing an existing version is refused (409).
  async publish(user: RegistryUser, input: PublishInput, now: string): Promise<PublishResult> {
    const slug = `${user.handle}/${input.name}`;
    const existing = await this.bySlug(slug);

    if (existing) {
      if (existing.publisher_id !== user.id && user.role !== "admin") {
        throw new ApiError(403, "not_owner", "That name belongs to another publisher.");
      }
      // A new version may change executable source or manifest content even
      // when its public kind stays the same. Only trusted admin updates bypass
      // re-review; publisher updates always lose verification until approved.
      const publisherNeedsReview = user.role !== "admin";
      const keepsStatus = !publisherNeedsReview && existing.status !== "private";
      const status = input.visibility === "private" ? "private" : keepsStatus ? existing.status : "pending";
      const verified = publisherNeedsReview ? 0 : existing.verified;
      const version = input.version || nextPatch(existing.latest_version);
      await this.insertVersion(existing.id, version, input, now);
      await this.db
        .prepare(
          `UPDATE packages SET kind = ?1, summary = ?2, description = ?3, source = ?4, install_kind = ?5,
             homepage = ?6, repo_url = ?7, tags = ?8, latest_version = ?9, updated_at = ?10,
             status = ?11, verified = ?12
           WHERE id = ?13`,
        )
        .bind(
          input.kind,
          input.summary,
          input.description,
          input.source,
          input.installKind,
          input.homepage,
          input.repoUrl,
          input.tags.join(","),
          version,
          now,
          status,
          verified,
          existing.id,
        )
        .run();
      const row = await this.bySlug(slug);
      if (!row) throw new ApiError(500, "publish_failed", "Package not found after update.");
      return { row, created: false, version };
    }

    const version = input.version || "0.1.0";
    const status = input.visibility === "private" ? "private" : user.role === "admin" ? "active" : "pending";
    const inserted = await this.db
      .prepare(
        `INSERT INTO packages
           (kind, scope_handle, name, slug, summary, description, source, install_kind,
            homepage, repo_url, tags, latest_version, status, publisher_id, created_at, updated_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15, ?15)
         RETURNING id`,
      )
      .bind(
        input.kind,
        user.handle,
        input.name,
        slug,
        input.summary,
        input.description,
        input.source,
        input.installKind,
        input.homepage,
        input.repoUrl,
        input.tags.join(","),
        version,
        status,
        user.id,
        now,
      )
      .first<{ id: number }>();
    if (!inserted) throw new ApiError(500, "publish_failed", "Insert returned no id.");
    await this.insertVersion(inserted.id, version, input, now);
    const row = await this.bySlug(slug);
    if (!row) throw new ApiError(500, "publish_failed", "Package not found after insert.");
    return { row, created: true, version };
  }

  private async insertVersion(packageId: number, version: string, input: PublishInput, now: string): Promise<void> {
    const res = await this.db
      .prepare(
        `INSERT OR IGNORE INTO package_versions
           (package_id, version, source, manifest, content_hash, risk_level, created_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)`,
      )
      .bind(packageId, version, input.source, input.manifest, input.contentHash, input.riskLevel, now)
      .run();
    if ((res.meta.changes ?? 0) === 0) {
      throw new ApiError(409, "version_exists", `Version ${version} is already published.`);
    }
  }

  // Everything one publisher has submitted, in every review state, newest first.
  async listByPublisher(publisherId: number, limit: number): Promise<PackageRow[]> {
    const res = await this.db
      .prepare("SELECT * FROM packages WHERE publisher_id = ?1 ORDER BY updated_at DESC, id DESC LIMIT ?2")
      .bind(publisherId, limit)
      .all<PackageRow>();
    return res.results ?? [];
  }

  // One publisher's own package in any state. Anyone else gets null, the same
  // answer as a slug that does not exist, so ownership never reveals existence.
  async ownedBySlug(slug: string, publisherId: number): Promise<PackageRow | null> {
    return this.db
      .prepare("SELECT * FROM packages WHERE slug = ?1 AND publisher_id = ?2")
      .bind(slug, publisherId)
      .first<PackageRow>();
  }

  // A private package enters the review queue only when its owner asks.
  async submitPrivate(slug: string, publisherId: number, now: string): Promise<PackageRow | null> {
    return this.db
      .prepare(
        `UPDATE packages SET status = 'pending', updated_at = ?1
         WHERE slug = ?2 AND publisher_id = ?3 AND status = 'private'
         RETURNING *`,
      )
      .bind(now, slug, publisherId)
      .first<PackageRow>();
  }

  // Admin: packages awaiting (or past) review, newest first.
  async listByStatus(status: string, limit: number): Promise<PackageRow[]> {
    const res = await this.db
      .prepare("SELECT * FROM packages WHERE status = ?1 ORDER BY created_at DESC LIMIT ?2")
      .bind(status, limit)
      .all<PackageRow>();
    return res.results ?? [];
  }

  // Admin: live packages the vote rules flag for another look (lib/ranking.ts
  // needsReview). Derived from the counts, so a recovering package drops out.
  async listFlagged(limit: number): Promise<ReviewRow[]> {
    const res = await this.db
      .prepare(
        `SELECT p.*, COALESCE(v.content_hash, '') AS content_hash FROM packages p
         LEFT JOIN package_versions v ON v.package_id = p.id AND v.version = p.latest_version
         WHERE p.status = 'active' AND p.down_count >= ?1
           AND CAST(p.up_count AS REAL) / (p.up_count + p.down_count) < ?2
         ORDER BY p.down_count DESC, p.created_at DESC LIMIT ?3`,
      )
      .bind(FLAG_MIN_DOWN, FLAG_MAX_APPROVAL, limit)
      .all<ReviewRow>();
    return res.results ?? [];
  }

  // Admin: move a package between statuses (reject, hide). A private package is
  // left alone: moving it would make it approvable without its owner's consent.
  async setStatus(slug: string, status: string, now: string): Promise<PackageRow | null> {
    const res = await this.db
      .prepare("UPDATE packages SET status = ?1, updated_at = ?2 WHERE slug = ?3 AND status != 'private'")
      .bind(status, now, slug)
      .run();
    if ((res.meta.changes ?? 0) === 0) return null;
    return this.bySlug(slug);
  }

  // Admin approval must be bound to the exact row the reviewer inspected.
  // The version protects publisher updates, while updated_at + status also
  // fence concurrent moderation actions. D1 evaluates the predicate and write
  // atomically, so a package cannot change between a preflight read and approval.
  async setStatusIfCurrent(
    slug: string,
    status: string,
    expectedVersion: string,
    expectedUpdatedAt: string,
    expectedStatus: string,
    now: string,
    reviewedHash = "",
  ): Promise<PackageRow | null> {
    // The pin a client installs against is the digest the reviewer computed,
    // never the publisher's claim: it is written (or cleared) on the reviewed
    // row while the package is still unpublished, under the same fence.
    await this.db
      .prepare(
        `UPDATE package_versions SET content_hash = ?1
         WHERE version = ?2 AND package_id = (
           SELECT id FROM packages WHERE slug = ?3 AND latest_version = ?2 AND updated_at = ?4 AND status = ?5
         )`,
      )
      .bind(reviewedHash, expectedVersion, slug, expectedUpdatedAt, expectedStatus)
      .run();
    return this.db
      .prepare(
        `UPDATE packages SET status = ?1, updated_at = ?2
         WHERE slug = ?3 AND latest_version = ?4 AND updated_at = ?5 AND status = ?6
         RETURNING *`,
      )
      .bind(status, now, slug, expectedVersion, expectedUpdatedAt, expectedStatus)
      .first<PackageRow>();
  }

  // Admin: bind a reviewed digest to the live version. Fenced like approval and
  // compare-and-set on the digest read here, so `previous` is exactly the value
  // replaced. The package's status and updated_at are left untouched.
  async repinIfCurrent(
    slug: string,
    expectedVersion: string,
    expectedUpdatedAt: string,
    contentHash: string,
  ): Promise<{ row: PackageRow; previous: string } | null> {
    const current = await this.db
      .prepare(
        `SELECT v.content_hash AS content_hash FROM package_versions v
         JOIN packages p ON p.id = v.package_id
         WHERE p.slug = ?1 AND v.version = ?2 AND p.latest_version = ?2 AND p.updated_at = ?3 AND p.status = 'active'`,
      )
      .bind(slug, expectedVersion, expectedUpdatedAt)
      .first<{ content_hash: string }>();
    if (!current) return null;
    const res = await this.db
      .prepare(
        `UPDATE package_versions SET content_hash = ?1
         WHERE version = ?2 AND content_hash = ?3 AND package_id = (
           SELECT id FROM packages WHERE slug = ?4 AND latest_version = ?2 AND updated_at = ?5 AND status = 'active'
         )`,
      )
      .bind(contentHash, expectedVersion, current.content_hash, slug, expectedUpdatedAt)
      .run();
    if ((res.meta.changes ?? 0) === 0) return null;
    const row = await this.bySlug(slug);
    return row ? { row, previous: current.content_hash } : null;
  }

  // Admin console: the review queue with each package's current-version digest.
  async listForReview(status: string, limit: number): Promise<ReviewRow[]> {
    const res = await this.db
      .prepare(
        `SELECT p.*, COALESCE(v.content_hash, '') AS content_hash FROM packages p
         LEFT JOIN package_versions v ON v.package_id = p.id AND v.version = p.latest_version
         WHERE p.status = ?1 ORDER BY p.created_at DESC LIMIT ?2`,
      )
      .bind(status, limit)
      .all<ReviewRow>();
    return res.results ?? [];
  }

  // Admin: grant or revoke the verified trust badge.
  async setVerified(slug: string, verified: boolean, now: string): Promise<PackageRow | null> {
    const res = await this.db
      .prepare("UPDATE packages SET verified = ?1, updated_at = ?2 WHERE slug = ?3 AND status != 'private'")
      .bind(verified ? 1 : 0, now, slug)
      .run();
    if ((res.meta.changes ?? 0) === 0) return null;
    return this.bySlug(slug);
  }
}

// Bump the patch component so an update without an explicit version still lands
// as a distinct, immutable version row. Non-semver latest values restart at 0.1.0.
function nextPatch(latest: string): string {
  const m = /^(\d+)\.(\d+)\.(\d+)$/.exec(latest.trim());
  if (!m) return "0.1.0";
  return `${m[1]}.${m[2]}.${Number(m[3]) + 1}`;
}
