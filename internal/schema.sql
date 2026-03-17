PRAGMA foreign_keys = ON;

-- Core users discovered from Telegram.
CREATE TABLE users (
    telegram_id TEXT PRIMARY KEY,
    telegram_username TEXT,
    display_name TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Per-user OAuth integration connections.
CREATE TABLE integrations (
    id TEXT PRIMARY KEY,
    telegram_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    account_email TEXT,
    access_token_enc TEXT NOT NULL,
    refresh_token_enc TEXT,
    scopes TEXT NOT NULL,
    expires_at TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (telegram_id) REFERENCES users(telegram_id)
);

-- Indexable sources configured by users.
CREATE TABLE sources (
    id TEXT PRIMARY KEY,
    telegram_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    external_id TEXT NOT NULL,
    name TEXT NOT NULL,
    source_type TEXT NOT NULL,
    last_synced_at TEXT,
    sync_cursor TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (telegram_id) REFERENCES users(telegram_id)
);

CREATE UNIQUE INDEX idx_sources_provider_external
ON sources(provider, external_id);

-- Chunked documents available for retrieval.
CREATE TABLE documents (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    external_path TEXT,
    title TEXT,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    metadata_json TEXT,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (source_id) REFERENCES sources(id)
);

CREATE UNIQUE INDEX idx_documents_source_path
ON documents(source_id, external_path);

CREATE TABLE chunks (
    id TEXT PRIMARY KEY,
    document_id TEXT NOT NULL,
    chunk_index INTEGER NOT NULL,
    content TEXT NOT NULL,
    embedding_json TEXT,
    token_count INTEGER,
    metadata_json TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (document_id) REFERENCES documents(id)
);

CREATE UNIQUE INDEX idx_chunks_document_order
ON chunks(document_id, chunk_index);

-- Sync job history for webhooks and scheduled refreshes.
CREATE TABLE sync_jobs (
    id TEXT PRIMARY KEY,
    source_id TEXT,
    trigger_type TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    error_message TEXT,
    stats_json TEXT,
    FOREIGN KEY (source_id) REFERENCES sources(id)
);

-- Chat history metadata (messages can be stored externally later if needed).
CREATE TABLE chat_sessions (
    id TEXT PRIMARY KEY,
    telegram_chat_id TEXT NOT NULL,
    telegram_user_id TEXT NOT NULL,
    last_message_at TEXT NOT NULL,
    context_window_json TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);


CREATE TABLE audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    telegram_id TEXT,
    event_type TEXT NOT NULL,
    entity_type TEXT,
    entity_id TEXT,
    details_json TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (telegram_id) REFERENCES users(telegram_id)
);
