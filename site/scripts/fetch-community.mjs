// Prebuild: fetch live community stats + bake contributor avatars into public/av/.
// Never fails the build — falls back to the committed snapshot on any error.
import { mkdir, readFile, writeFile } from 'node:fs/promises';

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

await mkdir('public/av', { recursive: true });
let baked = 0;
const out = await Promise.all(list.map(async (c) => {
  const remote = `${c.avatar}?s=120`;
  try {
    const r = await fetch(remote);
    if (!r.ok) throw new Error(String(r.status));
    await writeFile(`public/av/${c.login}.png`, Buffer.from(await r.arrayBuffer()));
    baked++;
    return { ...c, avatar: `/av/${c.login}.png` };
  } catch {
    return { ...c, avatar: remote };
  }
}));

await writeFile('src/data/community.json', JSON.stringify({ stars, mergedPrs, contributors, list: out }, null, 2) + '\n');
console.log(`community: ${contributors} contributors · ${stars} stars · ${mergedPrs} merged PRs · ${baked}/${out.length} avatars baked`);
