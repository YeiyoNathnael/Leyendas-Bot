# Takhtit Bot Implementation Status

Date: 2026-03-17

## Current Phase

We are in Phase 4 hardening and quality (retrieval quality, security lifecycle, and persistence completeness).

## Completed

1. Migration from chess bot baseline to Takhtit assistant runtime
- Removed chess/Lichess command paths from runtime.
- Added Takhtit command surface: `/start`, `/help`, `/ask`, `/connect_github`, `/connect_google`, `/sync`, `/sync_status`.

2. LangChainGo-first RAG implementation
- LangChainGo text splitting/chunking is integrated.
- RAG index is now persisted in Turso (not in-memory).
- Embeddings are generated with Gemini embedding model and stored per chunk in Turso.
- Query retrieval uses embedding similarity against persisted vectors.

3. Connector framework + ingestion pipeline
- Connector abstraction introduced.
- GitHub connector implemented with real API ingestion for:
  - code and config files
  - open issues
  - open pull requests
- Google Docs and Google Calendar connectors ingest real API data.

4. Sync orchestration
- Manual sync via `/sync`.
- Sync run tracking + `/sync_status`.
- Periodic sync scheduler supported with env configuration.

5. Web runtime + webhook support
- Health endpoint: `GET /healthz`.
- GitHub webhook endpoint: `POST /webhooks/github?user_id=<telegram_user_id>`.
- Heroku process split: web + worker via `RUN_MODE`.

6. Per-user OAuth (GitHub + Google)
- OAuth connect URL generation with signed state.
- OAuth callback endpoint: `GET /oauth/github/callback`.
- OAuth callback endpoint: `GET /oauth/google/callback`.
- Code exchange for access token.
- Token and account persistence in Turso integrations table.
- GitHub ingestion uses per-user stored token first, with env token fallback.
- Google Docs and Calendar ingestion use per-user stored token first, with env token fallback.

7. Real Google ingestion
- Google Docs connector now lists documents from Drive and extracts document body text from Docs API.
- Google Calendar connector now ingests upcoming events from Calendar API, including rich descriptions with project context links.

8. Retrieval quality and persistence hardening
- Hybrid retrieval is implemented (vector similarity + lexical overlap scoring).
- Sync run states are persisted in DB (`sync_jobs`) in addition to in-memory status for fast reads.
- Chat activity is persisted in DB (`chat_sessions`).
- `/ask` responses append explicit source citation lines based on retrieved context.

9. OAuth token security lifecycle hardening
- Integration tokens are encrypted at rest when `TOKEN_ENCRYPTION_KEY` is configured.
- Google access tokens are refreshed automatically using stored refresh tokens on expiry.
- Integration status is marked revoked when refresh fails.

10. Observability and tests
- Metrics endpoint added: `GET /metrics` (sync totals, success/failure counts, last status/message/timestamps).
- Added unit tests for retrieval utility scoring, token encryption/decryption behavior, and citation formatting helpers.

## In Progress / Remaining

1. Add integration tests for OAuth callback and refresh behavior with mocked providers.
2. Add per-user and per-provider sync metrics dimensions.
3. Add admin command for recent sync failures and diagnostics.

## Environment Variables Used Now

- `TOKEN`
- `GEMINI_API_KEY`
- `DBURL`
- `EMBEDDING_MODEL`
- `GITHUB_REPOS`
- `GITHUB_MAX_FILES` (optional)
- `GITHUB_TOKEN` (fallback only)
- `GITHUB_CLIENT_ID`
- `GITHUB_CLIENT_SECRET`
- `GITHUB_REDIRECT_URI`
- `OAUTH_STATE_SECRET`
- `GITHUB_WEBHOOK_SECRET`
- `GOOGLE_CLIENT_ID`
- `GOOGLE_CLIENT_SECRET`
- `GOOGLE_REDIRECT_URI`
- `GOOGLE_TOKEN` (fallback only)
- `GOOGLE_DOCS_MAX` (optional)
- `GOOGLE_CALENDAR_MAX_EVENTS` (optional)
- `GOOGLE_CALENDAR_ID` (optional)
- TOKEN_ENCRYPTION_KEY
- `PORT`
- `RUN_MODE`
- `SYNC_INTERVAL_MINUTES` (optional)
- `DEFAULT_SYNC_USER_ID` (required when scheduler is enabled)

## Suggested Next Execution Block

Implement hybrid retrieval (vector + lexical + reranker), then harden token security (encryption + refresh/revoke lifecycle).
