-- name: UpsertUser :exec
INSERT INTO users (
    telegram_id,
    telegram_username,
    display_name,
    created_at,
    updated_at
)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(telegram_id) DO UPDATE SET
    telegram_username = excluded.telegram_username,
    display_name = excluded.display_name,
    updated_at = excluded.updated_at;

-- name: GetUser :one
SELECT * FROM users
WHERE telegram_id = ?;

-- name: ListUserIntegrations :many
SELECT * FROM integrations
WHERE telegram_id = ?
ORDER BY provider ASC;

-- name: UpsertIntegration :exec
INSERT INTO integrations (
    id,
    telegram_id,
    provider,
    account_email,
    access_token_enc,
    refresh_token_enc,
    scopes,
    expires_at,
    status,
    created_at,
    updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    account_email = excluded.account_email,
    access_token_enc = excluded.access_token_enc,
    refresh_token_enc = excluded.refresh_token_enc,
    scopes = excluded.scopes,
    expires_at = excluded.expires_at,
    status = excluded.status,
    updated_at = excluded.updated_at;

-- name: UpsertSource :exec
INSERT INTO sources (
    id,
    telegram_id,
    provider,
    external_id,
    name,
    source_type,
    last_synced_at,
    sync_cursor,
    status,
    created_at,
    updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider, external_id) DO UPDATE SET
    name = excluded.name,
    source_type = excluded.source_type,
    last_synced_at = excluded.last_synced_at,
    sync_cursor = excluded.sync_cursor,
    status = excluded.status,
    updated_at = excluded.updated_at;

-- name: ListSourcesByUser :many
SELECT * FROM sources
WHERE telegram_id = ?
ORDER BY updated_at DESC;

-- name: UpsertDocument :exec
INSERT INTO documents (
    id,
    source_id,
    external_path,
    title,
    content,
    content_hash,
    metadata_json,
    updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source_id, external_path) DO UPDATE SET
    title = excluded.title,
    content = excluded.content,
    content_hash = excluded.content_hash,
    metadata_json = excluded.metadata_json,
    updated_at = excluded.updated_at;

-- name: DeleteChunksByDocument :exec
DELETE FROM chunks
WHERE document_id = ?;

-- name: InsertChunk :exec
INSERT INTO chunks (
    id,
    document_id,
    chunk_index,
    content,
    embedding_json,
    token_count,
    metadata_json,
    created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertSyncJob :exec
INSERT INTO sync_jobs (
    id,
    source_id,
    trigger_type,
    status,
    started_at,
    finished_at,
    error_message,
    stats_json
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateSyncJobStatus :exec
UPDATE sync_jobs
SET status = ?,
    finished_at = ?,
    error_message = ?,
    stats_json = ?
WHERE id = ?;

-- name: UpsertChatSession :exec
INSERT INTO chat_sessions (
    id,
    telegram_chat_id,
    telegram_user_id,
    last_message_at,
    context_window_json,
    created_at,
    updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    last_message_at = excluded.last_message_at,
    context_window_json = excluded.context_window_json,
    updated_at = excluded.updated_at;

-- name: InsertAuditEvent :exec
INSERT INTO audit_events (
    telegram_id,
    event_type,
    entity_type,
    entity_id,
    details_json,
    created_at
)
VALUES (?, ?, ?, ?, ?, ?);
