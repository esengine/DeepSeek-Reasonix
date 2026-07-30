const R2_BASE = "https://dl.reasonix.io";
const GITHUB_RELEASES_API = "https://api.github.com/repos/esengine/DeepSeek-Reasonix/releases?per_page=100";
const GITHUB_LATEST_RELEASE_API = "https://api.github.com/repos/esengine/DeepSeek-Reasonix/releases/latest";
const RELEASE_METHODS = "GET, HEAD, OPTIONS";
const DESKTOP_DOWNLOAD_PAGE = "https://reasonix.io/?download=desktop#start";
const DESKTOP_UPDATER_ASSETS = [
  ["platforms", "darwin-arm64", "Reasonix-darwin-arm64.zip"],
  ["platforms", "darwin-amd64", "Reasonix-darwin-amd64.zip"],
  ["platforms", "windows-amd64", "Reasonix-windows-amd64-installer.exe"],
  ["platforms", "windows-arm64", "Reasonix-windows-arm64-installer.exe"],
  ["platforms", "linux-amd64", "Reasonix-linux-amd64.tar.gz"],
  ["native_packages", "linux-amd64", "Reasonix-linux-amd64.deb"],
] as const;
const DESKTOP_DOWNLOAD_ASSETS = [
  ["downloads", "Reasonix-darwin-universal.dmg", "Reasonix-darwin-universal.dmg"],
  ["downloads", "Reasonix-windows-amd64.zip", "Reasonix-windows-amd64.zip"],
] as const;
const SHA256 = /^[0-9a-f]{64}$/;
const MAX_RELEASE_ASSET_SIZE = 1 << 30;

type PublicReleaseChannel = "stable" | "preview";
type ReleaseChannel = PublicReleaseChannel | "canary";

type GitHubAsset = {
  name?: string;
  browser_download_url?: string;
  size?: unknown;
};

type GitHubRelease = {
  tag_name?: string;
  draft?: boolean;
  prerelease?: boolean;
  html_url?: string;
  assets?: GitHubAsset[];
};

const CLI_ASSETS = [
  "reasonix-darwin-amd64.tar.gz",
  "reasonix-darwin-arm64.tar.gz",
  "reasonix-linux-amd64.tar.gz",
  "reasonix-linux-arm64.tar.gz",
  "reasonix-windows-amd64.zip",
  "reasonix-windows-arm64.zip",
  "SHA256SUMS",
] as const;
const CLI_ASSET_NAMES = new Set<string>(CLI_ASSETS);
const STABLE_CLI_TAG = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const PREVIEW_CLI_TAG = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)-preview\.(0|[1-9]\d*)$/;

function manifestPointer(channel: ReleaseChannel): string {
  if (channel === "stable") return `${R2_BASE}/latest/latest.json`;
  return channel === "preview" ? `${R2_BASE}/preview/latest.json` : `${R2_BASE}/canary/latest.json`;
}

function gatewayHeaders(source: string): Record<string, string> {
  return {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "public, max-age=300, stale-if-error=86400",
    "access-control-allow-origin": "*",
    "access-control-allow-methods": RELEASE_METHODS,
    "x-reasonix-release-source": source,
  };
}

export async function handleReleaseGatewayRequest(
  method: string,
  load: () => Promise<Response>,
): Promise<Response> {
  const normalized = method.toUpperCase();
  if (normalized === "OPTIONS") {
    return new Response(null, {
      status: 204,
      headers: {
        ...gatewayHeaders("preflight"),
        "cache-control": "public, max-age=86400",
        "access-control-max-age": "86400",
        allow: RELEASE_METHODS,
      },
    });
  }
  if (normalized !== "GET" && normalized !== "HEAD") {
    return new Response(JSON.stringify({ error: "method not allowed" }) + "\n", {
      status: 405,
      headers: { ...gatewayHeaders("method-not-allowed"), allow: RELEASE_METHODS },
    });
  }

  const response = await load();
  if (normalized === "GET") return response;
  return new Response(null, {
    status: response.status,
    statusText: response.statusText,
    headers: response.headers,
  });
}

function safeHTTPSURL(value: unknown): string {
  if (typeof value !== "string") return "";
  try {
    const url = new URL(value);
    return url.protocol === "https:" &&
      Boolean(url.hostname) &&
      !url.username &&
      !url.password &&
      url.href === value
      ? url.href
      : "";
  } catch {
    return "";
  }
}

function expectedCLIAssetURL(value: unknown, tag: string, name: string): string {
  const safe = safeHTTPSURL(value);
  if (!safe) return "";
  const url = new URL(safe);
  const path = `/esengine/DeepSeek-Reasonix/releases/download/${tag}/${name}`;
  return url.hostname.toLowerCase() === "github.com" &&
    !url.port &&
    url.pathname === path &&
    !url.search &&
    !url.hash
    ? url.href
    : "";
}

function cliTagOrder(tag: unknown, channel: PublicReleaseChannel): string[] | null {
  const value = String(tag || "");
  const match = value.match(channel === "preview" ? PREVIEW_CLI_TAG : STABLE_CLI_TAG);
  return match ? match.slice(1) : null;
}

function compareDecimal(left: string, right: string): number {
  if (left.length !== right.length) return left.length - right.length;
  return left === right ? 0 : left > right ? 1 : -1;
}

function compareOrder(left: string[], right: string[]): number {
  for (let index = 0; index < Math.max(left.length, right.length); index += 1) {
    const difference = compareDecimal(left[index] || "0", right[index] || "0");
    if (difference !== 0) return difference;
  }
  return 0;
}

function normalizeCLIRelease(
  release: GitHubRelease,
  channel: PublicReleaseChannel,
): { release: GitHubRelease; order: string[] } | null {
  const order = cliTagOrder(release.tag_name, channel);
  if (!order || release.draft || Boolean(release.prerelease) !== (channel === "preview")) return null;

  const assetsByName = new Map<string, GitHubAsset>();
  const seen = new Set<string>();
  for (const asset of Array.isArray(release.assets) ? release.assets : []) {
    const name = String(asset?.name || "");
    if (!CLI_ASSET_NAMES.has(name)) continue;
    if (seen.has(name)) return null;
    seen.add(name);
    const download = expectedCLIAssetURL(asset?.browser_download_url, String(release.tag_name || ""), name);
    if (
      name &&
      download &&
      Number.isSafeInteger(asset?.size) &&
      (asset.size as number) > 0 &&
      (asset.size as number) <= MAX_RELEASE_ASSET_SIZE
    ) {
      assetsByName.set(name, { name, browser_download_url: download, size: asset.size });
    }
  }
  if (CLI_ASSETS.some((name) => !assetsByName.has(name))) return null;

  const tag = String(release.tag_name);
  const releaseURL = `https://github.com/esengine/DeepSeek-Reasonix/releases/tag/${tag}`;
  return {
    order,
    release: {
      tag_name: tag,
      prerelease: channel === "preview",
      html_url: releaseURL,
      assets: CLI_ASSETS.map((name) => assetsByName.get(name) as GitHubAsset),
    },
  };
}

function selectCLIRelease(releases: GitHubRelease[], channel: PublicReleaseChannel): GitHubRelease | null {
  let selected: { release: GitHubRelease; order: string[] } | null = null;
  for (const release of releases) {
    const candidate = normalizeCLIRelease(release, channel);
    if (candidate && (!selected || compareOrder(candidate.order, selected.order) > 0)) selected = candidate;
  }
  return selected?.release ?? null;
}

async function fetchCLIRelease(url: string, channel: PublicReleaseChannel, source: string): Promise<Response | null> {
  try {
    const response = await fetch(url, {
      headers: { accept: "application/json", "user-agent": "reasonix-release-gateway" },
    });
    if (!response.ok) return null;
    const release = normalizeCLIRelease((await response.json()) as GitHubRelease, channel)?.release;
    return release ? new Response(JSON.stringify(release) + "\n", { status: 200, headers: gatewayHeaders(source) }) : null;
  } catch {
    return null;
  }
}

async function fetchLatestCLIReleaseFromGitHub(channel: PublicReleaseChannel): Promise<Response | null> {
  try {
    const response = await fetch(GITHUB_RELEASES_API, {
      headers: { accept: "application/vnd.github+json", "user-agent": "reasonix-release-gateway" },
    });
    if (!response.ok) return null;
    const release = selectCLIRelease((await response.json()) as GitHubRelease[], channel);
    return release
      ? new Response(JSON.stringify(release) + "\n", { status: 200, headers: gatewayHeaders("github-cli-releases") })
      : null;
  } catch {
    return null;
  }
}

function objectValue(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function desktopAssetBases(
  version: string,
  channel: PublicReleaseChannel,
  allowLegacyPreview: boolean,
): string[] {
  const r2 = `${R2_BASE}/desktop-${version}/`;
  if (channel === "preview") {
    return allowLegacyPreview ? [r2, `${R2_BASE}/desktop-preview/`] : [r2];
  }
  return [r2, `https://github.com/esengine/DeepSeek-Reasonix/releases/download/desktop-${version}/`];
}

function normalizeDesktopManifest(
  value: unknown,
  channel: PublicReleaseChannel,
  expectedVersion?: string,
): Record<string, unknown> | null {
  const manifest = objectValue(value);
  if (!manifest) return null;

  const version = manifest.version;
  const pattern = channel === "preview" ? PREVIEW_CLI_TAG : STABLE_CLI_TAG;
  if (
    typeof version !== "string" ||
    !pattern.test(version) ||
    (expectedVersion !== undefined && version !== expectedVersion) ||
    manifest.download_page !== DESKTOP_DOWNLOAD_PAGE
  ) {
    return null;
  }

  const legacyManifest = manifest.downloads === undefined || manifest.downloads === null;
  const allowedBases = desktopAssetBases(version, channel, legacyManifest);
  let selectedBase = "";
  const requiredAssets = legacyManifest
    ? DESKTOP_UPDATER_ASSETS
    : [...DESKTOP_UPDATER_ASSETS, ...DESKTOP_DOWNLOAD_ASSETS];
  for (const [groupName, key, fileName] of requiredAssets) {
    const group = objectValue(manifest[groupName]);
    const asset = objectValue(group?.[key]);
    if (
      !asset ||
      !Number.isSafeInteger(asset.size) ||
      (asset.size as number) <= 0 ||
      (asset.size as number) > MAX_RELEASE_ASSET_SIZE ||
      typeof asset.sha256 !== "string" ||
      !SHA256.test(asset.sha256)
    ) {
      return null;
    }

    const rawURL = asset.url;
    const safeURL = safeHTTPSURL(rawURL);
    const base = typeof rawURL === "string"
      ? allowedBases.find((candidate) => rawURL === candidate + fileName)
      : undefined;
    if (
      !safeURL ||
      !base ||
      asset.sig !== `${rawURL}.minisig` ||
      (selectedBase && selectedBase !== base)
    ) {
      return null;
    }
    selectedBase = base;
  }

  return selectedBase ? manifest : null;
}

function parseDesktopManifestJSON(
  text: string,
  channel: PublicReleaseChannel,
  expectedVersion?: string,
): Record<string, unknown> | null {
  try {
    return normalizeDesktopManifest(JSON.parse(text), channel, expectedVersion);
  } catch {
    return null;
  }
}

async function fetchManifestText(
  url: string,
  source: string,
  channel: PublicReleaseChannel,
  expectedVersion?: string,
): Promise<Response | null> {
  try {
    const safeURL = safeHTTPSURL(url);
    if (!safeURL) return null;
    const res = await fetch(safeURL, {
      headers: {
        accept: "application/json",
        "user-agent": "reasonix-release-gateway",
      },
    });
    if (!res.ok) return null;
    const text = await res.text();
    if (!parseDesktopManifestJSON(text, channel, expectedVersion)) return null;
    return new Response(text, { status: 200, headers: gatewayHeaders(source) });
  } catch {
    return null;
  }
}

async function fetchLatestDesktopManifestFromGitHub(): Promise<Response | null> {
  try {
    const latest = await fetch(GITHUB_LATEST_RELEASE_API, {
      headers: {
        accept: "application/vnd.github+json",
        "user-agent": "reasonix-release-gateway",
      },
    });
    if (!latest.ok) return null;

    const release = (await latest.json()) as GitHubRelease;
    const tag = typeof release.tag_name === "string" ? release.tag_name : "";
    const match = tag.match(/^desktop-(v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*))$/);
    if (!match || release.draft !== false || release.prerelease !== false) return null;

    const expectedManifestURL =
      `https://github.com/esengine/DeepSeek-Reasonix/releases/download/${tag}/latest.json`;
    const manifest = Array.isArray(release.assets)
      ? release.assets.find((asset) =>
          asset?.name === "latest.json" &&
          asset.browser_download_url === expectedManifestURL &&
          safeHTTPSURL(asset.browser_download_url) === expectedManifestURL &&
          Number.isSafeInteger(asset.size) &&
          (asset.size as number) > 0 &&
          (asset.size as number) <= MAX_RELEASE_ASSET_SIZE)
      : undefined;
    if (!manifest?.browser_download_url) return null;
    return fetchManifestText(
      manifest.browser_download_url,
      "github-desktop-release",
      "stable",
      match[1],
    );
  } catch {
    return null;
  }
}

export async function handleDesktopReleaseManifest(channel: ReleaseChannel): Promise<Response> {
  const publicChannel = channel === "stable" ? "stable" : "preview";
  const r2 = await fetchManifestText(manifestPointer(channel), `r2-${channel}`, publicChannel);
  if (r2) return r2;

  if (channel === "preview") {
    const canary = await fetchManifestText(manifestPointer("canary"), "r2-canary-compat", "preview");
    if (canary) return canary;
  }

  if (channel === "stable") {
    const github = await fetchLatestDesktopManifestFromGitHub();
    if (github) return github;
  }

  return new Response(JSON.stringify({ error: "desktop release manifest unavailable", channel }) + "\n", {
    status: 502,
    headers: gatewayHeaders("unavailable"),
  });
}

export async function handleCLIRelease(channel: PublicReleaseChannel): Promise<Response> {
  const cacheStorage = (globalThis as unknown as {
    caches?: CacheStorage & { default?: Cache };
  }).caches;
  const cache = cacheStorage?.default;
  const cacheKey = new Request(`https://crash.reasonix.io/v1/cli/releases/${channel}/latest.json`);
  const cached = await cache?.match(cacheKey);
  if (cached) return cached;

  const pointer = await fetchCLIRelease(`${R2_BASE}/cli/${channel}/latest.json`, channel, `r2-cli-${channel}`);
  const response = pointer ?? (await fetchLatestCLIReleaseFromGitHub(channel));
  if (response) {
    await cache?.put(cacheKey, response.clone());
    return response;
  }

  return new Response(JSON.stringify({ error: "CLI release unavailable", channel }) + "\n", {
    status: 502,
    headers: gatewayHeaders("unavailable"),
  });
}

export function desktopReleaseChannel(path: string): ReleaseChannel | null {
  const match = path.match(/^\/v1\/desktop\/releases\/(stable|preview|canary)\/latest\.json$/);
  return (match?.[1] as ReleaseChannel | undefined) ?? null;
}

export function cliReleaseChannel(path: string): PublicReleaseChannel | null {
  const match = path.match(/^\/v1\/cli\/releases\/(stable|preview)\/latest\.json$/);
  return (match?.[1] as PublicReleaseChannel | undefined) ?? null;
}
