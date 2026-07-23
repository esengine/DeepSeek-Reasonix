import source from '../../../release-notes/releases.json';

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
  date: string;
  channel: 'stable' | 'prerelease';
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
export const releases = releaseCatalog.releases;
export const latestRelease = releases[0];

export function releasePath(version: string): string {
  return `/changelog/v${version}/`;
}
