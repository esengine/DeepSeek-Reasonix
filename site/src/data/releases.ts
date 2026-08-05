import source from '../../../release-notes/releases.json';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

export type LocalizedText = { en: string; zh: string };

export type ReleaseItem = {
  title: LocalizedText;
  body: LocalizedText;
  refs?: number[];
};

export type ReleaseHighlight = ReleaseItem & {
  kind: 'new' | 'improved' | 'fixed' | 'security';
};

export type ReleaseGuide = ReleaseItem & { href: string };
export type ReleaseUpgrade = ReleaseItem & { level: 'info' | 'warning' };

export type ReleaseRecord = {
  version: string;
  releaseId?: string;
  baseVersion?: string;
  date: string;
  channel: 'stable' | 'prerelease';
  status?: 'reviewed' | 'published';
  candidateSha?: string;
  previousRelease?: string;
  builds?: { cli: string; desktop: string; npm: string };
  title: LocalizedText;
  summary: LocalizedText;
  surfaces: string[];
  guides: ReleaseGuide[];
  highlights: ReleaseHighlight[];
  changes: { new: ReleaseItem[]; improved: ReleaseItem[]; fixed: ReleaseItem[] };
  upgrade: ReleaseUpgrade[];
  risks: ReleaseItem[];
  contributors: string[];
  links: { github: string; compare: string; download: string };
};

export const releaseCatalog = source as { schemaVersion: number; releases: ReleaseRecord[] };
export const allReleases = releaseCatalog.releases;

let publishedIds = new Set<string>();
try {
  const generated = JSON.parse(
    readFileSync(resolve(process.cwd(), '.generated/publications.json'), 'utf8'),
  ) as { publications?: string[] };
  publishedIds = new Set(generated.publications ?? []);
} catch {
  // Before prebuild, preserve legacy entries and fail closed for reviewed
  // releases that require a successful all-surface publication marker.
}

export const publishedReleases = allReleases.filter(
  (release) => !release.status || release.status === 'published' || publishedIds.has(release.version),
);
// Public navigation contains official releases only. Historical prereleases
// remain addressable by their exact version URL for compatibility.
export const releases = publishedReleases.filter((release) => release.channel === 'stable');
export const stableReleases = releases;
export const previewReleases = publishedReleases.filter((release) => release.channel === 'prerelease');
export const latestStableRelease = stableReleases[0];
export const latestPreviewRelease = previewReleases[0];
export const latestRelease = latestStableRelease;

export function releasePath(version: string): string {
  return `/changelog/v${version}/`;
}
