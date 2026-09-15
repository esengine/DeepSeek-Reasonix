const STABLE_TAG = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const DESKTOP_DOWNLOAD_PAGE = "https://reasonix.io/?download=desktop#start";
const DESKTOP_REQUIRED_ASSETS = [
  ["platforms", "darwin-arm64", "Reasonix-darwin-arm64.zip"],
  ["platforms", "darwin-amd64", "Reasonix-darwin-amd64.zip"],
  ["platforms", "windows-amd64", "Reasonix-windows-amd64-installer.exe"],
  ["platforms", "windows-arm64", "Reasonix-windows-arm64-installer.exe"],
  ["platforms", "linux-amd64", "Reasonix-linux-amd64.tar.gz"],
  ["native_packages", "linux-amd64", "Reasonix-linux-amd64.deb"],
  ["downloads", "Reasonix-darwin-universal.dmg", "Reasonix-darwin-universal.dmg"],
  ["downloads", "Reasonix-windows-amd64.zip", "Reasonix-windows-amd64.zip"],
];
const DESKTOP_ARCH_DMG_ASSETS = [
  ["downloads", "Reasonix-darwin-arm64.dmg", "Reasonix-darwin-arm64.dmg"],
  ["downloads", "Reasonix-darwin-amd64.dmg", "Reasonix-darwin-amd64.dmg"],
];
const DESKTOP_ASSETS = [...DESKTOP_REQUIRED_ASSETS, ...DESKTOP_ARCH_DMG_ASSETS];
const DESKTOP_ASSET_NAMES = new Set(DESKTOP_ASSETS.map(([, , name]) => name));
const OFFICIAL_DESKTOP_RELEASE_TAG = /^(?:desktop-)?(v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*))$/;
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

export const publicReleaseChannels = new Set(["stable"]);

export function normalizePublicReleaseChannel(_value) {
  return "stable";
}

export function cliUpgradeCommand(_value) {
  return "reasonix upgrade";
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
    if (Boolean(release?.prerelease)) continue;
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
  const exactChangelogURL = `https://reasonix.io/changelog/${parsed.tag}/`;
  const changelogURL = release.release_notes_url === exactChangelogURL
    ? exactChangelogURL
    : "https://reasonix.io/changelog/";
  return {
    channel,
    version: parsed.tag,
    displayVersion: parsed.tag.slice(1),
    assets,
    releaseURL,
    changelogURL,
  };
}

function desktopAssetBases(parsed) {
  const tag = `desktop-${parsed.tag}`;
  return [
    `https://dl.reasonix.io/${tag}/`,
    `https://github.com/esengine/DeepSeek-Reasonix/releases/download/${tag}/`,
    `https://github.com/esengine/DeepSeek-Reasonix/releases/download/${parsed.tag}/`,
  ];
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
  const archDMGCount = DESKTOP_ARCH_DMG_ASSETS.filter(([group, key]) => manifest?.[group]?.[key]).length;
  if (archDMGCount !== 0 && archDMGCount !== DESKTOP_ARCH_DMG_ASSETS.length) return null;
  const manifestAssets = archDMGCount === DESKTOP_ARCH_DMG_ASSETS.length
    ? DESKTOP_ASSETS
    : DESKTOP_REQUIRED_ASSETS;
  for (const [group, key, name] of manifestAssets) {
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
  return selectedBase ? { parsed, manifestAssets } : null;
}

export function desktopReleaseModel(manifest, requestedChannel) {
  const normalized = normalizeDesktopManifest(manifest, requestedChannel);
  if (!normalized) return null;
  const { parsed, manifestAssets } = normalized;
  const assets = Object.fromEntries(manifestAssets.map(([group, key, name]) => [
    name,
    manifest[group][key].url,
  ]));
  return {
    channel: parsed.channel,
    version: parsed.tag,
    displayVersion: parsed.tag.slice(1),
    assets,
    changelogURL: manifest.release_notes_url === `https://reasonix.io/changelog/${parsed.tag}/`
      ? manifest.release_notes_url
      : "https://reasonix.io/changelog/",
  };
}

// Accept both historical desktop-v* releases and the combined v* release.
export function desktopGitHubReleaseModel(release) {
  const match = typeof release?.tag_name === "string"
    ? release.tag_name.match(OFFICIAL_DESKTOP_RELEASE_TAG)
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
  if (DESKTOP_REQUIRED_ASSETS.some(([, , name]) => !found[name])) return null;
  const archDMGCount = DESKTOP_ARCH_DMG_ASSETS.filter(([, , name]) => found[name]).length;
  if (archDMGCount !== 0 && archDMGCount !== DESKTOP_ARCH_DMG_ASSETS.length) return null;
  const releaseAssets = archDMGCount === DESKTOP_ARCH_DMG_ASSETS.length
    ? DESKTOP_ASSETS
    : DESKTOP_REQUIRED_ASSETS;

  return {
    channel: "stable",
    version: match[1],
    displayVersion: match[1].slice(1),
    assets: Object.fromEntries(releaseAssets.map(([, , name]) => [name, found[name]])),
    changelogURL: "https://reasonix.io/changelog/",
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

// Desktop releases published for manual download while the updater stays on its
// prior version; scripts/manual-desktop-exception.sh owns the same approval list
// for publication. Every entry is probed and the newest one that actually
// resolves wins, so adding the next tag before it is published cannot downgrade
// the page, and a later signed stable release still supersedes all of them.
const MANUAL_DESKTOP_TAGS = ["desktop-v1.38.8", "desktop-v1.38.9"];

export async function fetchDesktopDownloadModel(fetchImpl = fetch) {
  const load = async (manifestURLs, releaseURL) => {
    try {
      return desktopReleaseModel(await fetchFirstJSON(
        manifestURLs, fetchImpl, (manifest) => Boolean(desktopReleaseModel(manifest)),
      ));
    } catch {
      return desktopGitHubReleaseModel(await fetchFirstJSON(
        [releaseURL], fetchImpl, (release) => Boolean(desktopGitHubReleaseModel(release)),
      ));
    }
  };
  const results = await Promise.allSettled([
    load([
      "https://dl.reasonix.io/latest/latest.json",
      "https://crash.reasonix.io/v1/desktop/releases/stable/latest.json",
    ], "https://api.github.com/repos/esengine/DeepSeek-Reasonix/releases/latest"),
    ...MANUAL_DESKTOP_TAGS.map((tag) => load(
      [`https://dl.reasonix.io/${tag}/latest.json`],
      `https://api.github.com/repos/esengine/DeepSeek-Reasonix/releases/tags/${tag}`,
    )),
  ]);
  let selected = null;
  for (const result of results) {
    if (result.status !== "fulfilled" || !result.value) continue;
    const model = result.value;
    if (!selected || compareOrder(parsePublicTag(model.version).order, parsePublicTag(selected.version).order) > 0) {
      selected = model;
    }
  }
  return selected;
}
