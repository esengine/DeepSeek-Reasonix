import assert from 'node:assert/strict';
import test from 'node:test';

import {
  buildDailySeries,
  fetchAllStargazers,
  parseLinkHeader,
  renderStarHistorySvg,
} from './update-star-history.mjs';

test('parseLinkHeader extracts pagination relationships', () => {
  assert.deepEqual(
    parseLinkHeader(
      '<https://api.github.com/example?page=2>; rel="next", <https://api.github.com/example?page=4>; rel="last"',
    ),
    {
      next: 'https://api.github.com/example?page=2',
      last: 'https://api.github.com/example?page=4',
    },
  );
});

test('fetchAllStargazers follows every page and requests starred_at timestamps', async () => {
  const requests = [];
  const fakeFetch = async (url, options) => {
    requests.push({ url: String(url), headers: options.headers });
    const page = new URL(url).searchParams.get('page');
    const body =
      page === '1'
        ? [{ starred_at: '2026-07-19T01:00:00Z' }]
        : [{ starred_at: '2026-07-20T01:00:00Z' }];
    return new Response(JSON.stringify(body), {
      headers:
        page === '1'
          ? {
              link: '<https://api.github.com/repos/example/repo/stargazers?per_page=100&page=2>; rel="next", <https://api.github.com/repos/example/repo/stargazers?per_page=100&page=2>; rel="last"',
            }
          : {},
      status: 200,
    });
  };

  assert.deepEqual(
    await fetchAllStargazers({
      repository: 'example/repo',
      token: 'fake-token',
      fetchImpl: fakeFetch,
    }),
    ['2026-07-19T01:00:00Z', '2026-07-20T01:00:00Z'],
  );
  assert.equal(requests.length, 2);
  assert.equal(requests[0].headers.Accept, 'application/vnd.github.star+json');
  assert.equal(requests[0].headers.Authorization, 'Bearer fake-token');
});

test('fetchAllStargazers does not retry permission failures', async () => {
  let requests = 0;
  const fakeFetch = async () => {
    requests += 1;
    return new Response('{"message":"Forbidden"}', { status: 403 });
  };

  await assert.rejects(
    fetchAllStargazers({
      repository: 'example/repo',
      token: 'fake-token',
      fetchImpl: fakeFetch,
    }),
    /GitHub API returned 403/,
  );
  assert.equal(requests, 1);
});

test('buildDailySeries aggregates days and extends the line to the current date', () => {
  assert.deepEqual(
    buildDailySeries(
      [
        '2026-07-19T01:00:00Z',
        '2026-07-19T18:00:00Z',
        '2026-07-20T01:00:00Z',
      ],
      new Date('2026-07-21T12:00:00Z'),
    ),
    [
      { date: '2026-07-19', count: 2 },
      { date: '2026-07-20', count: 3 },
      { date: '2026-07-21', count: 3 },
    ],
  );
});

test('renderStarHistorySvg produces stable accessible light and dark charts', () => {
  const series = [
    { date: '2026-07-19', count: 1 },
    { date: '2026-07-21', count: 3 },
  ];
  const light = renderStarHistorySvg(series, { repository: 'example/repo', theme: 'light' });
  const dark = renderStarHistorySvg(series, { repository: 'example/repo', theme: 'dark' });

  assert.match(light, /<title id="title">example\/repo Star History<\/title>/);
  assert.match(light, /3 stars/);
  assert.match(light, /#ffffff/);
  assert.match(dark, /#0d1117/);
  assert.doesNotMatch(light, /NaN|undefined/);
});
