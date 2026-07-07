// The subset of an account the registry needs: identity + namespace + trust.
export interface RegistryUser {
  id: number;
  handle: string;
  role: "member" | "admin";
  emailVerified: boolean;
}

export type PackageKind = "skill" | "mcp";

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
  install_kind: string;
  homepage: string;
  repo_url: string;
  tags: string;
  latest_version: string;
  install_count: number;
  star_count: number;
  verified: number;
  status: string;
  publisher_id: number;
  created_at: string;
  updated_at: string;
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
  installKind: string;
  homepage: string;
  repoUrl: string;
  tags: string[];
  latestVersion: string;
  installCount: number;
  starCount: number;
  verified: boolean;
  status: string;
  createdAt: string;
  updatedAt: string;
}

export interface VersionRow {
  version: string;
  source: string;
  content_hash: string;
  risk_level: string;
  created_at: string;
}

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
    installKind: row.install_kind,
    homepage: row.homepage,
    repoUrl: row.repo_url,
    tags: splitTags(row.tags),
    latestVersion: row.latest_version,
    installCount: row.install_count,
    starCount: row.star_count,
    verified: row.verified === 1,
    status: row.status,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}
