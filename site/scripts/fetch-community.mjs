// Prebuild: fetch live community stats + bake contributor avatars into public/av/.
// Never fails the build — falls back to the committed snapshot on any error.
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import sharp from 'sharp';

const repo = 'esengine/DeepSeek-Reasonix';
const api = `https://api.github.com/repos/${repo}`;
const headers = { 'User-Agent': 'reasonix-site', Accept: 'application/vnd.github+json' };
if (process.env.GITHUB_TOKEN) headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;

const fallback = JSON.parse(await readFile('src/data/contributors.json', 'utf8'));
let { stars, mergedPrs, contributors, list } = fallback;

try {
  const r = await fetch(`${api}/contributors?per_page=100`, { headers });
  if (r.ok) {
    const humans = (await r.json())
      .filter((c) => c.type === 'User')
      .map((c) => ({ login: c.login, avatar: String(c.avatar_url).split('?')[0], url: c.html_url, c: c.contributions }));
    if (humans.length) { list = humans; contributors = humans.length; }
  }
} catch {}
try {
  const r = await fetch(api, { headers });
  if (r.ok) { const d = await r.json(); if (typeof d.stargazers_count === 'number') stars = d.stargazers_count; }
} catch {}
try {
  const r = await fetch(`https://api.github.com/search/issues?q=repo:${repo}+is:pr+is:merged&per_page=1`, { headers });
  if (r.ok) { const d = await r.json(); if (typeof d.total_count === 'number') mergedPrs = d.total_count; }
} catch {}

// 64px is the largest rendered avatar; bake at 2x. GitHub honours ?s= for its
// own CDN but serves some custom avatars unscaled and in whatever format was
// uploaded, so re-encode rather than trusting the response.
const AVATAR_PX = 128;

await rm('public/av', { recursive: true, force: true });
await mkdir('public/av', { recursive: true });
let baked = 0;
let bakedBytes = 0;
const out = await Promise.all(list.map(async (c) => {
  const remote = `${c.avatar}?s=${AVATAR_PX}`;
  try {
    const r = await fetch(remote);
    if (!r.ok) throw new Error(String(r.status));
    const webp = await sharp(Buffer.from(await r.arrayBuffer()))
      .resize(AVATAR_PX, AVATAR_PX, { fit: 'cover' })
      .webp({ quality: 80 })
      .toBuffer();
    await writeFile(`public/av/${c.login}.webp`, webp);
    baked++;
    bakedBytes += webp.byteLength;
    return { ...c, avatar: `/av/${c.login}.webp` };
  } catch {
    return { ...c, avatar: remote };
  }
}));

await writeFile('src/data/community.json', JSON.stringify({ stars, mergedPrs, contributors, list: out }, null, 2) + '\n');
console.log(
  `community: ${contributors} contributors · ${stars} stars · ${mergedPrs} merged PRs · ` +
    `${baked}/${out.length} avatars baked (${(bakedBytes / 1024).toFixed(0)} KB)`,
);
