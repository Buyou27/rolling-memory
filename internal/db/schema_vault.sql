-- Rolling-Memory bootstrap schema (fresh databases).
--
-- New databases carry ONLY the plaintext-vault product surface: the local vault
-- tables are created by localvault, the relational core tables (`rm_docs` /
-- `rm_terms` / `rm_citations`) by the embedded core store, and this bootstrap
-- adds the shared bookkeeping tables below.

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
