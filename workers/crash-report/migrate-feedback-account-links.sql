CREATE TABLE IF NOT EXISTS feedback_account_links (
  install_hash TEXT PRIMARY KEY,
  account_hash TEXT NOT NULL,
  linked_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS feedback_account_links_account ON feedback_account_links (account_hash);
