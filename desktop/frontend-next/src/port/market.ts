import type { PluginPlan } from "./plugin";

// The community registry as the market tab reads it. Every text field is the
// publisher's; what an install puts on disk is decided by the approved
// version's pinned digest and the plan the person confirms, never by these.
// A theme is its own category but installs as a plugin package carrying only themes.
export type MarketKind = "skill" | "plugin" | "mcp" | "theme";

// What this machine holds of a listed package, read off the market's own
// install record — and only while the things it installed are still there.
export interface MarketInstalled {
  version: string;
  contentHash: string;
}

export interface MarketPackage {
  kind: MarketKind;
  handle: string;
  name: string;
  slug: string;
  summary: string;
  description: string;
  homepage: string;
  repoUrl: string;
  tags: string[];
  latestVersion: string;
  installCount: number;
  starCount: number;
  upCount: number;
  downCount: number;
  // up / (up + down); null while nobody has voted, which is not zero approval.
  approvalRate: number | null;
  // The registry's recommended score, 0..1; the order itself is the registry's.
  score: number;
  verified: boolean;
  status: string;
  updatedAt: string;
  installed?: MarketInstalled;
  // The registry's word that the approved version is pinned; absent when it
  // did not say. The install still checks the digest itself.
  pinned?: boolean;
}

// The version a reviewer let through. contentHash is the pin: an install is
// refused unless the source still resolves to exactly this.
export interface MarketVersion {
  version: string;
  source: string;
  contentHash: string;
  riskLevel: string;
  createdAt: string;
}

export interface MarketDetail {
  package: MarketPackage;
  approved?: MarketVersion;
  // False means an install needs the person's trust (market.unpinned without it).
  pinned: boolean;
  installed?: MarketInstalled;
}

export interface MarketList {
  packages: MarketPackage[];
  limit: number;
  offset: number;
}

export interface MarketQuery {
  kind?: MarketKind | "";
  q?: string;
  sort?: "recommended" | "trending" | "new" | "installs";
  offset?: number;
  // Filtered by the registry, so paging stays whole.
  pinned?: boolean;
}

// version is the approved version the person was shown; the kernel refuses an
// install once a different one is approved (market.version_changed). trust
// accepts an unpinned version; digest is then the preview's contentDigest.
export interface MarketRequest {
  slug: string;
  version?: string;
  planId?: string;
  replace?: boolean;
  trust?: boolean;
  digest?: string;
}

// unreviewed marks an install of a version no reviewer pinned: contentDigest
// is then this preview's, and apply must echo it.
export type MarketPlan = PluginPlan & { slug: string; version: string; contentDigest?: string; unreviewed?: boolean };

// The account's own package, any review state. digest is the previewed
// contentDigest; the kernel refuses an apply without it (market.unpreviewed).
export type MarketOwnRequest = MarketRequest;

// One package offered for review under the signed-in account's handle.
export interface MarketSubmission {
  kind: MarketKind;
  name: string;
  source: string;
  summary?: string;
  description?: string;
  repoUrl?: string;
  version?: string;
  tags?: string[];
  // private keeps it to the publisher and out of review until submitted.
  visibility?: "public" | "private";
}

// The registry's receipt: a new submission lands as pending until approved.
export interface MarketPublished {
  package: MarketPackage;
  created: boolean;
  version: string;
}

export type MarketReviewStatus = "pending" | "active" | "rejected" | "hidden" | "private";

// The signed-in person's vote on one package. signedIn false is an answer, not
// an error: the entry offers sign-in instead of the buttons.
export interface MarketVote {
  signedIn: boolean;
  value: -1 | 0 | 1;
  upCount?: number;
  downCount?: number;
  approvalRate?: number | null;
  canVote?: boolean;
  own?: boolean;
  emailVerified?: boolean;
}
