const STABLE_TAG = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const PREVIEW_TAG = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)-preview\.(0|[1-9]\d*)$/;
const DESKTOP_DOWNLOAD_PAGE = "https://reasonix.io/?download=desktop#start";
const DESKTOP_ASSETS = [
  ["platforms", "darwin-arm64", "Reasonix-darwin-arm64.zip"],
  ["platforms", "darwin-amd64", "Reasonix-darwin-amd64.zip"],
  ["platforms", "windows-amd64", "Reasonix-windows-amd64-installer.exe"],
  ["platforms", "windows-arm64", "Reasonix-windows-arm64-installer.exe"],
  ["platforms", "linux-amd64", "Reasonix-linux-amd64.tar.gz"],
  ["native_packages", "linux-amd64", "Reasonix-linux-amd64.deb"],
  ["downloads", "Reasonix-darwin-universal.dmg", "Reasonix-darwin-universal.dmg"],
  ["downloads", "Reasonix-windows-amd64.zip", "Reasonix-windows-amd64.zip"],
];
const DESKTOP_ASSET_NAMES = new Set(DESKTOP_ASSETS.map(([, , name]) => name));
const STABLE_DESKTOP_RELEASE_TAG = /^desktop-(v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*))$/;
const SHA256 = /^[0-9a-f]{64}$/;
const MAX_RELEASE_ASSET_SIZE = 1 << 30;

// Keep in lockstep with workers/crash-report CLI asset gate.
export const CLI_RELEASE_ASSETS = [
  "reasonix-darwin-amd64.tar.gz",
  "reasonix-darwin-arm64.tar.gz",
  "reasonix-linux-amd64.tar.gz",
  "reasonix-linux-arm64.tar.gz",
  "reasonix-windows-amd64.zip",
  "reasonix-windows-arm64.zip",
  "SHA256SUMS",
];
const CLI_RELEASE_ASSET_NAMES = new Set(CLI_RELEASE_ASSETS);

export const publicReleaseChannels = new Set(["stable", "preview"]);

export function normalizePublicReleaseChannel(value) {
  return value === "preview" ? "preview" : "stable";
}

export function cliUpgradeCommand(value) {
  return `reasonix upgrade ${normalizePublicReleaseChannel(value)}`;
}

export function releaseVersionLabel(model) {
  return String(model?.version || "").trim() || "latest";
}

function parsePublicTag(tag) {
  const value = typeof tag === "string" ? tag : "";
  let match = value.match(STABLE_TAG);
  if (match) {
    return { tag: value, channel: "stable", order: match.slice(1) };
  }
  match = value.match(PREVIEW_TAG);
  if (match) {
    return { tag: value, channel: "preview", order: match.slice(1) };
  }
  return null;
}

function compareDecimal(left, right) {
  if (left.length !== right.length) return left.length - right.length;
  return left === right ? 0 : left > right ? 1 : -1;
}

function compareOrder(left, right) {
  for (let i = 0; i < Math.max(left.length, right.length); i += 1) {
    const delta = compareDecimal(left[i] || "0", right[i] || "0");
    if (delta !== 0) return delta;
  }
  return 0;
}

function safeHTTPSURL(value) {
  if (typeof value !== "string") return null;
  try {
    const url = new URL(value);
    return url.protocol === "https:" &&
      Boolean(url.hostname) &&
      !url.username &&
      !url.password
      ? url
      : null;
  } catch {
    return null;
  }
}

function expectedCLIAssetURL(value, tag, name) {
  if (typeof value !== "string") return null;
  const url = safeHTTPSURL(value);
  const path = `/esengine/DeepSeek-Reasonix/releases/download/${tag}/${name}`;
  return url &&
    url.href === value &&
    url.hostname.toLowerCase() === "github.com" &&
    !url.port &&
    url.pathname === path &&
    !url.search &&
    !url.hash
    ? url
    : null;
}

// Returns the 7 required CLI asset URLs, or null when any required asset is
// missing. Never synthesizes download URLs — incomplete releases must be
// rejected so the site never advertises 404 links.
export function releaseAssetMap(release) {
  const found = {};
  const seen = new Set();
  for (const asset of Array.isArray(release?.assets) ? release.assets : []) {
    const name = String(asset?.name || "");
    if (!CLI_RELEASE_ASSET_NAMES.has(name)) continue;
    if (seen.has(name)) return null;
    seen.add(name);
    const url = expectedCLIAssetURL(asset?.browser_download_url, String(release?.tag_name || ""), name);
    if (
      name &&
      url &&
      Number.isSafeInteger(asset?.size) &&
      asset.size > 0 &&
      asset.size <= MAX_RELEASE_ASSET_SIZE
    ) {
      found[name] = url.href;
    }
  }
  const assets = {};
  for (const name of CLI_RELEASE_ASSETS) {
    if (!found[name]) return null;
    assets[name] = found[name];
  }
  return assets;
}

export function selectCLIRelease(releases, requestedChannel) {
  const channel = normalizePublicReleaseChannel(requestedChannel);
  let selected = null;
  let selectedTag = null;
  for (const release of Array.isArray(releases) ? releases : []) {
    const parsed = parsePublicTag(release?.tag_name);
    if (!parsed || parsed.channel !== channel) continue;
    if (Boolean(release?.prerelease) !== (channel === "preview")) continue;
    if (!releaseAssetMap(release)) continue;
    if (!selectedTag || compareOrder(parsed.order, selectedTag.order) > 0) {
      selected = release;
      selectedTag = parsed;
    }
  }
  return selected;
}

export function cliReleaseModel(releases, requestedChannel) {
  const channel = normalizePublicReleaseChannel(requestedChannel);
  const release = selectCLIRelease(releases, channel);
  if (!release) return null;
  const parsed = parsePublicTag(release.tag_name);
  if (!parsed) return null;
  const assets = releaseAssetMap(release);
  if (!assets) return null;
  const releaseURL = `https://github.com/esengine/DeepSeek-Reasonix/releases/tag/${parsed.tag}`;
  return {
    channel,
    version: parsed.tag,
    displayVersion: parsed.tag.slice(1),
    assets,
    releaseURL,
  };
}

function desktopAssetBases(parsed) {
  const tag = `desktop-${parsed.tag}`;
  const bases = [`https://dl.reasonix.io/${tag}/`];
  if (parsed.channel === "stable") {
    bases.push(`https://github.com/esengine/DeepSeek-Reasonix/releases/download/${tag}/`);
  }
  return bases;
}

function normalizeDesktopManifest(manifest, requestedChannel) {
  const channel = normalizePublicReleaseChannel(requestedChannel);
  const parsed = parsePublicTag(manifest?.version);
  if (
    !parsed ||
    parsed.channel !== channel ||
    manifest?.download_page !== DESKTOP_DOWNLOAD_PAGE
  ) {
    return null;
  }

  const allowedBases = desktopAssetBases(parsed);
  let selectedBase = "";
  for (const [group, key, name] of DESKTOP_ASSETS) {
    const asset = manifest?.[group]?.[key];
    if (
      !asset ||
      typeof asset !== "object" ||
      Array.isArray(asset) ||
      !Number.isSafeInteger(asset.size) ||
      asset.size <= 0 ||
      asset.size > MAX_RELEASE_ASSET_SIZE ||
      typeof asset.sha256 !== "string" ||
      !SHA256.test(asset.sha256)
    ) {
      return null;
    }

    const rawURL = typeof asset.url === "string" ? asset.url : "";
    const url = safeHTTPSURL(rawURL);
    const base = allowedBases.find((candidate) => rawURL === candidate + name);
    if (
      !url ||
      !base ||
      url.href !== rawURL ||
      asset.sig !== `${rawURL}.minisig` ||
      (selectedBase && selectedBase !== base)
    ) {
      return null;
    }
    selectedBase = base;
  }
  return selectedBase ? { parsed } : null;
}

export function desktopReleaseModel(manifest, requestedChannel) {
  const normalized = normalizeDesktopManifest(manifest, requestedChannel);
  if (!normalized) return null;
  const { parsed } = normalized;
  const assets = Object.fromEntries(DESKTOP_ASSETS.map(([group, key, name]) => [
    name,
    manifest[group][key].url,
  ]));
  return {
    channel: parsed.channel,
    version: parsed.tag,
    displayVersion: parsed.tag.slice(1),
    assets,
  };
}

// GitHub Latest is owned by Desktop, so it can safely bridge the deployment
// window before an older six-asset manifest is replaced by the new eight-asset
// format. Preview has no GitHub release and never uses this fallback.
export function desktopGitHubReleaseModel(release) {
  const match = typeof release?.tag_name === "string"
    ? release.tag_name.match(STABLE_DESKTOP_RELEASE_TAG)
    : null;
  if (!match || release?.draft !== false || release?.prerelease !== false) return null;

  const tag = release.tag_name;
  const found = {};
  const seen = new Set();
  for (const asset of Array.isArray(release.assets) ? release.assets : []) {
    const name = String(asset?.name || "");
    if (!DESKTOP_ASSET_NAMES.has(name)) continue;
    if (seen.has(name)) return null;
    seen.add(name);
    const rawURL = typeof asset?.browser_download_url === "string" ? asset.browser_download_url : "";
    const url = safeHTTPSURL(rawURL);
    const expected = `https://github.com/esengine/DeepSeek-Reasonix/releases/download/${tag}/${name}`;
    if (
      !url ||
      url.href !== rawURL ||
      rawURL !== expected ||
      !Number.isSafeInteger(asset?.size) ||
      asset.size <= 0 ||
      asset.size > MAX_RELEASE_ASSET_SIZE
    ) {
      return null;
    }
    found[name] = rawURL;
  }
  if (DESKTOP_ASSETS.some(([, , name]) => !found[name])) return null;

  return {
    channel: "stable",
    version: match[1],
    displayVersion: match[1].slice(1),
    assets: Object.fromEntries(DESKTOP_ASSETS.map(([, , name]) => [name, found[name]])),
  };
}

export async function fetchFirstJSON(urls, fetchImpl = fetch, accept = () => true) {
  const failures = [];
  for (const url of urls) {
    try {
      const response = await fetchImpl(url, { cache: "no-cache" });
      if (!response.ok) throw new Error(`${response.status} ${response.statusText}`.trim());
      const payload = await response.json();
      if (!accept(payload)) throw new Error("invalid release data");
      return payload;
    } catch (error) {
      failures.push(`${url}: ${String(error?.message || error)}`);
    }
  }
  throw new Error(`release data unavailable (${failures.join("; ")})`);
}
