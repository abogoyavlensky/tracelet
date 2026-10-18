-- Dashboards: one JSON document per project and uid.
CREATE TABLE dashboards (
  project_id TEXT NOT NULL REFERENCES projects(id),
  uid TEXT NOT NULL,                 -- user-chosen, [a-z0-9-], stable across applies
  definition TEXT NOT NULL,          -- normalized JSON: title, schema_version, last, refresh, panels
  revision INTEGER NOT NULL,         -- 1 on create, +1 on each changing write
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (project_id, uid)
);
