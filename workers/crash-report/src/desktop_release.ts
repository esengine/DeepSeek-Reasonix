const R2_BASE = "https://dl.reasonix.io";
const GITHUB_RELEASES_API = "https://api.github.com/repos/esengine/DeepSeek-Reasonix/releases?per_page=30";

type ReleaseChannel = "stable" | "canary";

type GitHubAsset = {
  name?: string;
  browser_download_url?: string;
};

type GitHubRelease = {
  tag_name?: string;
  draft?: boolean;
  prerelease?: boolean;
  assets?: GitHubAsset[];
};

function manifestPointer(channel: ReleaseChannel): string {
  return channel === "canary" ? `${R2_BASE}/canary/latest.json` : `${R2_BASE}/latest/latest.json`;
}

function gatewayHeaders(source: string): HeadersInit {
  return {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "public, max-age=60",
    "x-reasonix-release-source": source,
  };
}

function isManifestJSON(text: string): boolean {
  try {
    const data = JSON.parse(text) as { version?: unknown; platforms?: unknown };
    return typeof data.version === "string" && Boolean(data.platforms) && typeof data.platforms === "object";
  } catch {
    return false;
  }
}

async function fetchManifestText(url: string, source: string): Promise<Response | null> {
  try {
    const res = await fetch(url, {
      headers: {
        accept: "application/json",
        "user-agent": "reasonix-release-gateway",
      },
    });
    if (!res.ok) return null;
    const text = await res.text();
    if (!isManifestJSON(text)) return null;
    return new Response(text, { status: 200, headers: gatewayHeaders(source) });
  } catch {
    return null;
  }
}

async function fetchLatestDesktopManifestFromGitHub(): Promise<Response | null> {
  try {
    const list = await fetch(GITHUB_RELEASES_API, {
      headers: {
        accept: "application/vnd.github+json",
        "user-agent": "reasonix-release-gateway",
      },
    });
    if (!list.ok) return null;

    const releases = (await list.json()) as GitHubRelease[];
    const latestDesktop = releases.find(
      (r) =>
        !r.draft &&
        !r.prerelease &&
        typeof r.tag_name === "string" &&
        /^desktop-v\d+\.\d+\.\d+(?:[+-][0-9A-Za-z.-]+)?$/.test(r.tag_name),
    );
    const manifest = latestDesktop?.assets?.find((a) => a.name === "latest.json" && a.browser_download_url);
    if (!manifest?.browser_download_url) return null;
    return fetchManifestText(manifest.browser_download_url, "github-desktop-release");
  } catch {
    return null;
  }
}

export async function handleDesktopReleaseManifest(channel: ReleaseChannel): Promise<Response> {
  const r2 = await fetchManifestText(manifestPointer(channel), `r2-${channel}`);
  if (r2) return r2;

  if (channel === "stable") {
    const github = await fetchLatestDesktopManifestFromGitHub();
    if (github) return github;
  }

  return new Response(JSON.stringify({ error: "desktop release manifest unavailable", channel }) + "\n", {
    status: 502,
    headers: gatewayHeaders("unavailable"),
  });
}

export function desktopReleaseChannel(path: string): ReleaseChannel | null {
  const match = path.match(/^\/v1\/desktop\/releases\/(stable|canary)\/latest\.json$/);
  return (match?.[1] as ReleaseChannel | undefined) ?? null;
}
