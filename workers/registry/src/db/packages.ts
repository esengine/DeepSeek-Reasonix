import type { PackageRow, VersionRow, RegistryUser } from "../types";
import type { PublishInput } from "../lib/validation";
import { ApiError } from "../http/errors";

const TRENDING_WINDOW_MS = 7 * 24 * 60 * 60 * 1000;

export interface ListParams {
  kind: "skill" | "mcp" | "all";
  q: string;
  sort: "new" | "trending" | "installs";
  limit: number;
  offset: number;
  now: string;
}

export interface PublishResult {
  row: PackageRow;
  created: boolean;
  version: string;
}

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

    let select = "SELECT p.* FROM packages p";
    let order: string;
    if (p.sort === "trending") {
      const windowStart = new Date(new Date(p.now).getTime() - TRENDING_WINDOW_MS).toISOString();
      select = `SELECT p.*, COALESCE(e.c, 0) AS trend FROM packages p
        LEFT JOIN (
          SELECT package_id, COUNT(*) AS c FROM events
          WHERE type = 'install' AND created_at > ?${binds.length + 1}
          GROUP BY package_id
        ) e ON e.package_id = p.id`;
      binds.push(windowStart);
      order = "ORDER BY trend DESC, p.install_count DESC, p.created_at DESC";
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

  async versions(packageId: number): Promise<VersionRow[]> {
    const res = await this.db
      .prepare(
        `SELECT version, source, content_hash, risk_level, created_at
         FROM package_versions WHERE package_id = ?1 ORDER BY created_at DESC`,
      )
      .bind(packageId)
      .all<VersionRow>();
    return res.results ?? [];
  }

  // Create a new package or append a version to an owned one. New packages from
  // non-admins land as 'pending' (hidden until an admin approves); versions of an
  // already-approved package stay live. Republishing an existing version is
  // refused (409) so version history stays immutable.
  async publish(user: RegistryUser, input: PublishInput, now: string): Promise<PublishResult> {
    const slug = `${user.handle}/${input.name}`;
    const existing = await this.bySlug(slug);

    if (existing) {
      if (existing.publisher_id !== user.id && user.role !== "admin") {
        throw new ApiError(403, "not_owner", "That name belongs to another publisher.");
      }
      const version = input.version || nextPatch(existing.latest_version);
      await this.insertVersion(existing.id, version, input, now);
      await this.db
        .prepare(
          `UPDATE packages SET summary = ?1, description = ?2, source = ?3, install_kind = ?4,
             homepage = ?5, repo_url = ?6, tags = ?7, latest_version = ?8, updated_at = ?9
           WHERE id = ?10`,
        )
        .bind(
          input.summary,
          input.description,
          input.source,
          input.installKind,
          input.homepage,
          input.repoUrl,
          input.tags.join(","),
          version,
          now,
          existing.id,
        )
        .run();
      const row = await this.bySlug(slug);
      if (!row) throw new ApiError(500, "publish_failed", "Package not found after update.");
      return { row, created: false, version };
    }

    const version = input.version || "0.1.0";
    const status = user.role === "admin" ? "active" : "pending";
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

  // Best-effort install tally. Returns the new count, or null when no active
  // package matches the slug.
  async recordInstall(slug: string): Promise<number | null> {
    const res = await this.db
      .prepare("UPDATE packages SET install_count = install_count + 1 WHERE slug = ?1 AND status = 'active'")
      .bind(slug)
      .run();
    if ((res.meta.changes ?? 0) === 0) return null;
    const row = await this.bySlug(slug);
    return row?.install_count ?? null;
  }

  // Toggle a star. Returns the resulting state, or null when the slug is unknown.
  async toggleStar(slug: string, userId: number, now: string): Promise<{ starred: boolean; count: number } | null> {
    const pkg = await this.bySlug(slug);
    if (!pkg || pkg.status !== "active") return null;

    const added = await this.db
      .prepare("INSERT OR IGNORE INTO stars (package_id, user_id, created_at) VALUES (?1, ?2, ?3)")
      .bind(pkg.id, userId, now)
      .run();

    if ((added.meta.changes ?? 0) > 0) {
      await this.db.prepare("UPDATE packages SET star_count = star_count + 1 WHERE id = ?1").bind(pkg.id).run();
      return { starred: true, count: pkg.star_count + 1 };
    }
    await this.db.prepare("DELETE FROM stars WHERE package_id = ?1 AND user_id = ?2").bind(pkg.id, userId).run();
    await this.db
      .prepare("UPDATE packages SET star_count = MAX(0, star_count - 1) WHERE id = ?1")
      .bind(pkg.id)
      .run();
    return { starred: false, count: Math.max(0, pkg.star_count - 1) };
  }

  // Admin: packages awaiting (or past) review, newest first.
  async listByStatus(status: string, limit: number): Promise<PackageRow[]> {
    const res = await this.db
      .prepare("SELECT * FROM packages WHERE status = ?1 ORDER BY created_at DESC LIMIT ?2")
      .bind(status, limit)
      .all<PackageRow>();
    return res.results ?? [];
  }

  // Admin: move a package between statuses (approve → active, reject, hide).
  async setStatus(slug: string, status: string, now: string): Promise<PackageRow | null> {
    const res = await this.db
      .prepare("UPDATE packages SET status = ?1, updated_at = ?2 WHERE slug = ?3")
      .bind(status, now, slug)
      .run();
    if ((res.meta.changes ?? 0) === 0) return null;
    return this.bySlug(slug);
  }

  // Admin: grant or revoke the verified trust badge.
  async setVerified(slug: string, verified: boolean, now: string): Promise<PackageRow | null> {
    const res = await this.db
      .prepare("UPDATE packages SET verified = ?1, updated_at = ?2 WHERE slug = ?3")
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
