-- Local demo data for the registry. Apply to the LOCAL D1 only:
--   wrangler d1 execute reasonix-registry --local --file=seed.sql
DELETE FROM events;
DELETE FROM stars;
DELETE FROM package_versions;
DELETE FROM packages;

INSERT INTO packages
  (kind, scope_handle, name, slug, summary, source, install_kind, repo_url, tags, latest_version, install_count, star_count, verified, publisher_id, created_at, updated_at)
VALUES
  ('skill','esengine','pr-review','esengine/pr-review','Adversarial multi-pass PR review with inline findings','https://github.com/esengine/reasonix-skills/pr-review','skill','https://github.com/esengine/reasonix-skills','review,git,quality','1.2.0',428,96,1,1,datetime('now','-40 days'),datetime('now','-3 days')),
  ('skill','esengine','go-test-gen','esengine/go-test-gen','Generate table-driven Go tests for the current package','https://github.com/esengine/reasonix-skills/go-test-gen','skill','https://github.com/esengine/reasonix-skills','go,testing','0.4.1',213,44,1,1,datetime('now','-22 days'),datetime('now','-6 days')),
  ('mcp','sivancola','feishu-notify','sivancola/feishu-notify','Send Feishu/Lark interactive cards from your agent','https://github.com/SivanCola/feishu-mcp/.mcp.json','mcp','https://github.com/SivanCola/feishu-mcp','feishu,notify,im','0.3.0',156,31,0,2,datetime('now','-14 days'),datetime('now','-2 days')),
  ('skill','huqiantao','commit-msg','huqiantao/commit-msg','Write conventional commit messages from the staged diff','https://raw.githubusercontent.com/HUQIANTAO/rx/main/commit-msg/SKILL.md','skill','https://github.com/HUQIANTAO/rx','git,commits','1.0.0',502,73,0,3,datetime('now','-9 days'),datetime('now','-1 day')),
  ('mcp','community','postgres','community/postgres','Query and inspect a Postgres database over MCP','npx -y @modelcontextprotocol/server-postgres','mcp','https://github.com/modelcontextprotocol/servers','database,sql,postgres','0.6.2',89,18,0,4,datetime('now','-5 days'),datetime('now','-5 days')),
  ('skill','gtc2080','changelog','gtc2080/changelog','Draft a CHANGELOG entry from merged PRs since the last tag','https://github.com/GTC2080/rx-skills/changelog','skill','https://github.com/GTC2080/rx-skills','release,docs','0.2.0',37,9,0,5,datetime('now','-2 days'),datetime('now','-2 days'));

INSERT INTO package_versions (package_id, version, source, content_hash, risk_level, created_at)
  SELECT id, latest_version, source, '', 'low', updated_at FROM packages;

INSERT INTO events (type, package_id, actor_handle, summary, created_at)
  SELECT 'publish', id, scope_handle, 'published ' || slug || '@' || latest_version, created_at FROM packages;

INSERT INTO events (type, package_id, actor_handle, summary, created_at) VALUES
  ((SELECT id FROM packages WHERE slug='huqiantao/commit-msg'),'milestone','huqiantao','huqiantao/commit-msg reached 500 installs',datetime('now','-6 hours')),
  ((SELECT id FROM packages WHERE slug='esengine/pr-review'),'star','sivancola','starred esengine/pr-review',datetime('now','-3 hours')),
  ((SELECT id FROM packages WHERE slug='gtc2080/changelog'),'publish','gtc2080','published gtc2080/changelog@0.2.0',datetime('now','-90 minutes'));

-- install pings over the last week to give the trending rank something to sort on
INSERT INTO events (type, package_id, actor_handle, summary, created_at)
  SELECT 'install', p.id, '', 'installed ' || p.slug, datetime('now','-' || (abs(random()) % 6) || ' days')
  FROM packages p, (SELECT 1 UNION SELECT 2 UNION SELECT 3 UNION SELECT 4 UNION SELECT 5);

-- a pending submission (moderation): must stay out of the public list/detail until approved
INSERT INTO packages
  (kind, scope_handle, name, slug, summary, source, install_kind, tags, latest_version, status, publisher_id, created_at, updated_at)
VALUES
  ('skill','newbie','untested-skill','newbie/untested-skill','A freshly submitted skill awaiting review','https://example.com/untested/SKILL.md','skill','experimental','0.1.0','pending',9,datetime('now','-20 minutes'),datetime('now','-20 minutes'));
