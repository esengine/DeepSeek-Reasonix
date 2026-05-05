export type RegistrySource = "official" | "smithery" | "local";

export interface RegistryInstall {
  runtime: "npm" | "pypi" | "remote";
  packageId?: string;
  version?: string;
  transport: "stdio" | "sse" | "streamable-http";
  /** For remote transports. */
  url?: string;
  /** Env var names the user must set. */
  requiredEnv?: string[];
  /** Trailing args to pass after the package id — e.g. ["run", "<qualifiedName>"] for `npx -y @smithery/cli run X`. */
  extraArgs?: string[];
}

export interface RegistryEntry {
  /** Stable identifier — may be qualified ("io.example/mcp") or scoped ("@vendor/pkg"). */
  name: string;
  title: string;
  description: string;
  source: RegistrySource;
  /** Populated for official + local. Smithery list omits install info. */
  install?: RegistryInstall;
  /** Smithery's useCount, used as a sort key when present. */
  popularity?: number;
  /** Project / homepage URL. */
  homepage?: string;
  /** Icon URL — official: first packages[0].icons[0].src; smithery: iconUrl on listing. */
  iconUrl?: string;
}

export interface CachePagination {
  /** How many pages have been loaded so far. Smithery / local treat the whole listing as page 1. */
  pagesLoaded: number;
  /** Cursor needed to fetch the next page, or null if the source has been exhausted. */
  nextCursor: string | null;
}

export interface CacheFile {
  /** Bumped when the on-disk shape changes — older files are treated as invalid. */
  schemaVersion: 2;
  fetchedAt: number;
  source: RegistrySource;
  entries: RegistryEntry[];
  pagination: CachePagination;
}
