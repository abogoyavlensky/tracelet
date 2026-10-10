CREATE TABLE projects (
  id TEXT PRIMARY KEY,                       -- 16 random hex chars, immutable, stored in telemetry rows
  slug TEXT NOT NULL UNIQUE,                 -- [a-z0-9-]{1,40}
  name TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE tokens (
  id TEXT PRIMARY KEY,                       -- 16 random hex chars
  project_id TEXT REFERENCES projects(id),   -- NULL for admin and all-project read tokens
  scope TEXT NOT NULL CHECK (scope IN ('ingest', 'read', 'admin')),
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,                      -- first 6 chars after tl_, for listing
  hash BLOB NOT NULL UNIQUE,                 -- sha256 of the full token
  created_at TEXT NOT NULL,
  last_used_at TEXT,
  revoked_at TEXT
);

-- The Parquet file manifest: the manifest, not a directory glob, decides
-- which cold files a query reads.
CREATE TABLE files (
  id INTEGER PRIMARY KEY,
  signal TEXT NOT NULL,
  hour TEXT NOT NULL,
  path TEXT NOT NULL UNIQUE,
  rows INTEGER NOT NULL,
  min_ts TEXT NOT NULL,
  max_ts TEXT NOT NULL,
  max_ingest_ts TEXT NOT NULL,
  bytes INTEGER NOT NULL,
  schema_version INTEGER NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX files_signal_hour ON files (signal, hour);
