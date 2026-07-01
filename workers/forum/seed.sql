-- Initial categories. Apply after schema.sql:
--   wrangler d1 execute reasonix-forum --remote --file=seed.sql
INSERT OR IGNORE INTO categories (slug, name, description, position, min_trust_to_post) VALUES
  ('announcements', 'Announcements', 'Releases, roadmap, and community news from the team.', 1, 4),
  ('help',          'Help & Support', 'Stuck on setup, config, or cache behavior? Ask here.', 2, 0),
  ('skills',        'Skills & Plugins', 'Share, request, and review community skills and MCP servers.', 3, 0),
  ('show',          'Show & Tell', 'Built something with Reasonix? Show the community.', 4, 0),
  ('feedback',      'Feedback & Ideas', 'Feature requests and product feedback.', 5, 0);
