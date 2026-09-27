-- Short-lived authorization for encrypted remote attachments.
-- Raw upload and download tickets are never stored.

CREATE TABLE IF NOT EXISTS remote_attachment_grants (
  object_id             TEXT    PRIMARY KEY,
  user_id               INTEGER NOT NULL,
  target_device_id      TEXT    NOT NULL,
  upload_ticket_hash    TEXT    NOT NULL UNIQUE,
  download_ticket_hash  TEXT    NOT NULL UNIQUE,
  max_bytes             INTEGER NOT NULL,
  created_at            TEXT    NOT NULL,
  expires_at            TEXT    NOT NULL,
  uploaded_at           TEXT,
  ciphertext_bytes      INTEGER,
  ciphertext_sha256     TEXT
);
CREATE INDEX IF NOT EXISTS remote_attachment_grants_expires
  ON remote_attachment_grants (expires_at);
CREATE INDEX IF NOT EXISTS remote_attachment_grants_user
  ON remote_attachment_grants (user_id, target_device_id);
