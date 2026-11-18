-- Rolling-Memory thin bootstrap schema (fresh databases).
--
-- New databases carry ONLY the plaintext-vault product surface: the thin vault
-- tables are created by thinclient, the relational core tables (`rm_docs` /
-- `rm_terms` / `rm_citations`) by the embedded core store, and this bootstrap
-- adds the shared bookkeeping tables below. The legacy assertion graph
-- (`trace` / `node` / `assertion` / `mention` / `meta_edge` / `event` / FTS
-- shadows) is NOT created for fresh databases: the vault + dual-cluster kernel
-- is the single retrieval engine, so new databases start free of the legacy
-- graph, its indexes and its maintenance cost.
--
-- Existing databases keep whatever schema they already have; the canonical
-- v1 schema (schema.sql) remains the source for historical databases and for
-- the graph regression fixtures.

CREATE TABLE IF NOT EXISTS db_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS injection_ledger (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  session      TEXT NOT NULL,
  prompt_hash  TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  UNIQUE (session, prompt_hash, content_hash)
);
CREATE INDEX IF NOT EXISTS injection_time ON injection_ledger(created_at);

CREATE TABLE IF NOT EXISTS transcript_cursor (
  path       TEXT NOT NULL,
  session    TEXT NOT NULL,
  offset     INTEGER NOT NULL DEFAULT 0,
  size       INTEGER NOT NULL DEFAULT 0,
  mtime      REAL NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (path, session)
);
