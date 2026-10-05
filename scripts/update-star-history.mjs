#!/usr/bin/env node

import { mkdir, writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';

const API_VERSION = '2026-03-10';
const DEFAULT_REPOSITORY = 'esengine/DeepSeek-Reasonix';

export function parseLinkHeader(value) {
  const links = {};
  for (const part of (value || '').split(',')) {
    const match = part.trim().match(/^<([^>]+)>;\s*rel="([^"]+)"$/);
    if (match) links[match[2]] = match[1];
  }
  return links;
}

async function requestPage(url, token, fetchImpl) {
  const retryable = new Set([429, 500, 502, 503, 504]);
  let lastError;

  for (let attempt = 0; attempt < 4; attempt += 1) {
    let response;
    try {
      response = await fetchImpl(url, {
        headers: {
          Accept: 'application/vnd.github.star+json',
          Authorization: `Bearer ${token}`,
          'User-Agent': 'DeepSeek-Reasonix-star-history-updater',
          'X-GitHub-Api-Version': API_VERSION,
        },
        signal: AbortSignal.timeout(30_000),
      });
    } catch (error) {
      lastError = error;
      if (attempt < 3) {
        await new Promise((resolve) => setTimeout(resolve, 2 ** attempt * 1_000));
        continue;
      }
      break;
    }

    if (response.ok) return response;

    const detail = (await response.text()).trim().slice(0, 300);
    lastError = new Error(`GitHub API returned ${response.status}: ${detail}`);
    if (!retryable.has(response.status) || attempt === 3) break;
    await new Promise((resolve) => setTimeout(resolve, 2 ** attempt * 1_000));
  }

  throw lastError;
}

async function readStargazerPage(response, page) {
  const entries = await response.json();
  if (!Array.isArray(entries)) {
    throw new Error(`GitHub stargazers page ${page} was not an array`);
  }

  return entries.map((entry, index) => {
    if (typeof entry?.starred_at !== 'string') {
      throw new Error(
        `GitHub stargazers page ${page} item ${index + 1} did not include starred_at`,
      );
    }
    return entry.starred_at;
  });
}

export async function fetchAllStargazers({
  repository = DEFAULT_REPOSITORY,
  token,
  fetchImpl = fetch,
}) {
  if (!token) throw new Error('STAR_HISTORY_GITHUB_TOKEN is required');

  const endpoint = new URL(`https://api.github.com/repos/${repository}/stargazers`);
  endpoint.searchParams.set('per_page', '100');
  endpoint.searchParams.set('page', '1');

  const firstResponse = await requestPage(endpoint, token, fetchImpl);
  const links = parseLinkHeader(firstResponse.headers.get('link'));
  const lastPage = links.last
    ? Number.parseInt(new URL(links.last).searchParams.get('page') || '1', 10)
    : 1;

  if (!Number.isInteger(lastPage) || lastPage < 1) {
    throw new Error(`GitHub returned an invalid last page: ${lastPage}`);
  }

  const timestamps = await readStargazerPage(firstResponse, 1);
  for (let page = 2; page <= lastPage; page += 1) {
    endpoint.searchParams.set('page', String(page));
    const response = await requestPage(endpoint, token, fetchImpl);
    timestamps.push(...(await readStargazerPage(response, page)));

    if (page % 50 === 0 || page === lastPage) {
      console.log(`Fetched stargazer page ${page}/${lastPage}`);
    }
  }

  return timestamps;
}

export function buildDailySeries(timestamps, asOf = new Date()) {
  if (timestamps.length === 0) throw new Error('The repository has no stargazers');

  const byDay = new Map();
  for (const timestamp of timestamps) {
    const instant = new Date(timestamp);
    if (Number.isNaN(instant.getTime())) {
      throw new Error(`Invalid starred_at timestamp: ${timestamp}`);
    }
    const day = instant.toISOString().slice(0, 10);
    byDay.set(day, (byDay.get(day) || 0) + 1);
  }

  let total = 0;
  const series = [...byDay.entries()]
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([date, count]) => {
      total += count;
      return { date, count: total };
    });

  const today = asOf.toISOString().slice(0, 10);
  if (today > series.at(-1).date) series.push({ date: today, count: total });
  return series;
}

function escapeXml(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;');
}

function niceMaximum(value) {
  const magnitude = 10 ** Math.floor(Math.log10(Math.max(1, value)));
  return Math.ceil(value / magnitude) * magnitude;
}

function formatDateTick(timestamp, spanDays) {
  const date = new Date(timestamp);
  return new Intl.DateTimeFormat('en-US', {
    month: 'short',
    ...(spanDays > 365 ? { year: 'numeric' } : { day: 'numeric' }),
    timeZone: 'UTC',
  }).format(date);
}

export function renderStarHistorySvg(
  series,
  { repository = DEFAULT_REPOSITORY, theme = 'light' } = {},
) {
  if (series.length === 0) throw new Error('Cannot render an empty star history series');

  const palette =
    theme === 'dark'
      ? {
          background: '#0d1117',
          border: '#30363d',
          grid: '#21262d',
          line: '#58a6ff',
          muted: '#8b949e',
          text: '#f0f6fc',
        }
      : {
          background: '#ffffff',
          border: '#d0d7de',
          grid: '#d8dee4',
          line: '#0969da',
          muted: '#57606a',
          text: '#24292f',
        };

  const width = 960;
  const height = 480;
  const margin = { top: 78, right: 34, bottom: 62, left: 76 };
  const plotWidth = width - margin.left - margin.right;
  const plotHeight = height - margin.top - margin.bottom;
  const firstTime = Date.parse(`${series[0].date}T00:00:00Z`);
  const lastTime = Date.parse(`${series.at(-1).date}T00:00:00Z`);
  const timeSpan = Math.max(86_400_000, lastTime - firstTime);
  const spanDays = timeSpan / 86_400_000;
  const total = series.at(-1).count;
  const yMaximum = niceMaximum(total);
  const xFor = (timestamp) => margin.left + ((timestamp - firstTime) / timeSpan) * plotWidth;
  const yFor = (count) => margin.top + plotHeight - (count / yMaximum) * plotHeight;

  const points = series.map(({ date, count }) => ({
    x: xFor(Date.parse(`${date}T00:00:00Z`)),
    y: yFor(count),
  }));
  const linePath = points
    .map(({ x, y }, index) => `${index === 0 ? 'M' : 'L'}${x.toFixed(2)},${y.toFixed(2)}`)
    .join(' ');
  const areaPath = `${linePath} L${points.at(-1).x.toFixed(2)},${(
    margin.top + plotHeight
  ).toFixed(2)} L${points[0].x.toFixed(2)},${(margin.top + plotHeight).toFixed(2)} Z`;

  const yTicks = Array.from({ length: 6 }, (_, index) => {
    const value = (yMaximum / 5) * index;
    const y = yFor(value);
    return `
      <line x1="${margin.left}" y1="${y.toFixed(2)}" x2="${width - margin.right}" y2="${y.toFixed(2)}" stroke="${palette.grid}" stroke-width="1" />
      <text x="${margin.left - 12}" y="${(y + 4).toFixed(2)}" text-anchor="end" fill="${palette.muted}" font-size="12">${Math.round(value).toLocaleString('en-US')}</text>`;
  }).join('');

  const xTicks = Array.from({ length: 6 }, (_, index) => {
    const timestamp = firstTime + (timeSpan * index) / 5;
    const x = xFor(timestamp);
    return `
      <line x1="${x.toFixed(2)}" y1="${margin.top}" x2="${x.toFixed(2)}" y2="${margin.top + plotHeight}" stroke="${palette.grid}" stroke-width="1" />
      <text x="${x.toFixed(2)}" y="${height - 28}" text-anchor="middle" fill="${palette.muted}" font-size="12">${escapeXml(formatDateTick(timestamp, spanDays))}</text>`;
  }).join('');

  const lastPoint = points.at(-1);
  const title = `${repository} Star History`;
  return `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" role="img" aria-labelledby="title description">
  <title id="title">${escapeXml(title)}</title>
  <desc id="description">${total.toLocaleString('en-US')} current GitHub stargazers through ${escapeXml(series.at(-1).date)}.</desc>
  <rect width="${width}" height="${height}" rx="12" fill="${palette.background}" stroke="${palette.border}" />
  <text x="${margin.left}" y="37" fill="${palette.text}" font-family="-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif" font-size="22" font-weight="600">Star History</text>
  <text x="${margin.left}" y="60" fill="${palette.muted}" font-family="-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif" font-size="13">${escapeXml(repository)}</text>
  <g font-family="-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif">
    ${yTicks}
    ${xTicks}
    <path d="${areaPath}" fill="${palette.line}" opacity="0.12" />
    <path d="${linePath}" fill="none" stroke="${palette.line}" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" />
    <circle cx="${lastPoint.x.toFixed(2)}" cy="${lastPoint.y.toFixed(2)}" r="5" fill="${palette.line}" stroke="${palette.background}" stroke-width="2" />
    <g transform="translate(${width - margin.right - 280}, 25)">
      <circle cx="0" cy="0" r="5" fill="${palette.line}" />
      <text x="12" y="5" fill="${palette.text}" font-size="14" font-weight="600">${escapeXml(repository)}</text>
      <text x="12" y="24" fill="${palette.muted}" font-size="12">${total.toLocaleString('en-US')} stars</text>
    </g>
    <text x="${width - margin.right}" y="${height - 14}" text-anchor="end" fill="${palette.muted}" font-size="11">Updated automatically from GitHub stargazer history · ${escapeXml(series.at(-1).date)} UTC</text>
  </g>
</svg>
`;
}

export async function main() {
  const repository = process.env.STAR_HISTORY_REPOSITORY || DEFAULT_REPOSITORY;
  const outputDirectory = process.env.STAR_HISTORY_OUTPUT_DIR || 'assets/star-history';
  const token = process.env.STAR_HISTORY_GITHUB_TOKEN;

  console.log(`Fetching GitHub stargazer history for ${repository}`);
  const timestamps = await fetchAllStargazers({ repository, token });
  const series = buildDailySeries(timestamps);
  console.log(`Rendering ${timestamps.length.toLocaleString('en-US')} current stargazers`);

  await mkdir(outputDirectory, { recursive: true });
  await Promise.all([
    writeFile(
      `${outputDirectory}/star-history-light.svg`,
      renderStarHistorySvg(series, { repository, theme: 'light' }),
      'utf8',
    ),
    writeFile(
      `${outputDirectory}/star-history-dark.svg`,
      renderStarHistorySvg(series, { repository, theme: 'dark' }),
      'utf8',
    ),
  ]);
}

const isMain = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isMain) {
  main().catch((error) => {
    console.error(error instanceof Error ? error.message : error);
    process.exitCode = 1;
  });
}
