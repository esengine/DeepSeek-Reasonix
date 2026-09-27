CREATE TABLE IF NOT EXISTS telemetry_receipts (
  event_id TEXT PRIMARY KEY,
  date TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS telemetry_receipts_date ON telemetry_receipts (date);
