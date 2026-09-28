import { approvalRate } from "./lib/ranking";

// The subset of an account the registry needs: identity + namespace + trust.
export interface RegistryUser {
  id: number;
  handle: string;
  role: "member" | "admin";
  emailVerified: boolean;
}

export type PackageKind = "skill" | "plugin" | "mcp" | "theme";
// What install_source is asked to run. A theme ships inside a plugin package,
// so it is its own listing category but never its own installer.
export type InstallerKind = "skill" | "plugin" | "mcp";
export type InstallKind = "auto" | InstallerKind;

export function installerFor(kind: PackageKind): InstallerKind {
  return kind === "theme" ? "plugin" : kind;
}

// A `packages` row as stored in D1.
export interface PackageRow {
  id: number;
  kind: PackageKind;
  scope_handle: string;
  name: string;
  slug: string;
  summary: string;
  description: string;
  source: string;
  install_kind: InstallKind;
  homepage: string;
  repo_url: string;
  tags: string;
  latest_version: string;
  install_count: number;
  star_count: number;
  up_count: number;
  down_count: number;
  rec_score: number;
  verified: number;
  status: string;
  publisher_id: number;
  created_at: string;
  updated_at: string;
  // Only the listing query computes it; see PackageRepo.list.
  pinned?: number;
}

// The public, camel-cased view served by the API.
export interface PackageDTO {
  kind: PackageKind;
  handle: string;
  name: string;
  slug: string;
  summary: string;
  description: string;
  source: string;
  installKind: InstallerKind;
  homepage: string;
  repoUrl: string;
  tags: string[];
  latestVersion: string;
  installCount: number;
  // Deprecated alias of upCount, kept for clients that still read stars.
  starCount: number;
  upCount: number;
  downCount: number;
  // up / (up + down); null while nobody has voted.
  approvalRate: number | null;
  // The materialized recommended score, 0..1 (lib/ranking.ts).
  score: number;
  verified: boolean;
  status: string;
  createdAt: string;
  updatedAt: string;
}

// A listing row also says whether its latest version can be installed from the
// market, so a client can filter without fetching every detail.
export interface ListedPackageDTO extends PackageDTO {
  pinned: boolean;
}

export function toListedPackageDTO(row: PackageRow): ListedPackageDTO {
  return { ...toPackageDTO(row), pinned: row.pinned === 1 };
}

export interface VersionRow {
  id: number;
  version: string;
  source: string;
  content_hash: string;
  risk_level: string;
  created_at: string;
}

// A review-queue row: the package plus its current version's reviewed digest.
export type ReviewRow = PackageRow & { content_hash: string };

export interface EventRow {
  type: string;
  slug: string | null;
  actor_handle: string;
  summary: string;
  created_at: string;
}

function splitTags(tags: string): string[] {
  return tags
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);
}

export function toPackageDTO(row: PackageRow): PackageDTO {
  return {
    kind: row.kind,
    handle: row.scope_handle,
    name: row.name,
    slug: row.slug,
    summary: row.summary,
    description: row.description,
    source: row.source,
    // Legacy rows may contain `auto` or a mismatched explicit installer. The
    // declared public kind is authoritative for every API consumer.
    installKind: installerFor(row.kind),
    homepage: row.homepage,
    repoUrl: row.repo_url,
    tags: splitTags(row.tags),
    latestVersion: row.latest_version,
    installCount: row.install_count,
    starCount: row.up_count,
    upCount: row.up_count,
    downCount: row.down_count,
    approvalRate: approvalRate(row.up_count, row.down_count),
    score: row.rec_score,
    verified: row.verified === 1,
    status: row.status,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}
