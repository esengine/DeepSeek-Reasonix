import { mkdir, readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { validateReleaseEvent } from "../../scripts/release-event.mjs";

const siteRoot = resolve(import.meta.dirname, "..");
const repoRoot = resolve(siteRoot, "..");
const catalog = JSON.parse(await readFile(resolve(repoRoot, "release-notes/releases.json"), "utf8"));
const output = resolve(siteRoot, ".generated/publications.json");
const headers = {
  Accept: "application/vnd.github+json",
  "User-Agent": "reasonix-site-publications",
};
if (process.env.GITHUB_TOKEN) headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;

const publications = [];
for (const release of catalog.releases) {
  // Legacy entries predate publication markers and remain public. Every new
  // reviewed entry fails closed until its exact CLI release carries the marker
  // uploaded by the all-surface postflight.
  if (!release.status || release.status === "published") {
    publications.push(release.version);
    continue;
  }
  const tag = `v${release.version}`;
  try {
    const response = await fetch(
      `https://api.github.com/repos/esengine/DeepSeek-Reasonix/releases/tags/${encodeURIComponent(tag)}`,
      { headers, signal: AbortSignal.timeout(20_000) },
    );
    if (!response.ok) continue;
    const githubRelease = await response.json();
    const marker = githubRelease.assets?.find((asset) => asset.name === "release-event.json" && asset.size > 0);
    if (!marker?.browser_download_url) continue;
    const markerResponse = await fetch(marker.browser_download_url, {
      headers,
      signal: AbortSignal.timeout(20_000),
    });
    if (!markerResponse.ok) continue;
    const event = await markerResponse.json();
    validateReleaseEvent(event, release);
    publications.push(release.version);
  } catch {
    // A reviewed release must never become public because an upstream lookup
    // failed. The next Pages build retries after the release postflight.
  }
}

await mkdir(resolve(siteRoot, ".generated"), { recursive: true });
await writeFile(output, `${JSON.stringify({ schemaVersion: 1, publications }, null, 2)}\n`);
console.log(`Resolved ${publications.length} public release note(s).`);
