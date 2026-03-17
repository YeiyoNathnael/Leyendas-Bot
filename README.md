# Takhtit Assistant Bot

Takhtit Assistant is a Telegram group assistant for development teams. It is being rebuilt from scratch in this repository to support project-aware answers backed by retrieval-augmented generation (RAG).

This first implementation slice removes all chess/Lichess logic and introduces the new assistant runtime structure.

Phase 2 is now in progress with LangChainGo as the core RAG framework.

## Current Status

- Chess and tournament features have been removed.
- Telegram runtime now exposes Takhtit-focused commands.
- Service layering is in place:
  - Telegram handlers
  - Chat service
  - LangChainGo-based retrieval and chunking service
  - Turso-backed persistent vector chunk storage with Gemini embeddings
  - Connector-driven sync orchestration
- GitHub connector now ingests repository files, open issues, and open pull requests via GitHub API.
- Google Docs and Calendar connectors now ingest real data via Google APIs.
- Database schema and SQL query contracts were replaced with Takhtit assistant entities.

## Commands (Current)

| Command | Description |
|---|---|
| `/start` | Intro message for Takhtit assistant |
| `/help` | List supported commands |
| `/ask <question>` | Ask project questions using RAG context |
| `/connect_github` | Start GitHub read-only connection flow (stub URL) |
| `/connect_google` | Start Google Docs/Calendar read-only flow (stub URL) |
| `/sync` | Trigger immediate ingestion and re-indexing |
| `/sync_status` | Show latest sync run result for your user |

## Architecture (Initial)

```
.
├── main.go
├── Procfile
└── internal/
    ├── app/               # Core service composition
    ├── gemini/            # Gemini client wrapper
    ├── integrations/      # Connector framework and source adapters
    ├── rag/               # LangChainGo retrieval and chunking pipeline
    ├── telegram/          # Telegram commands and message handlers
    ├── schema.sql         # Assistant schema
    └── query.sql          # Assistant SQL contracts
```

## Environment

Create `.env`:

```env
TOKEN=your-telegram-bot-token
GEMINI_API_KEY=your-google-gemini-api-key
DBURL=libsql://your-turso-db-url?authToken=...
TOKEN_ENCRYPTION_KEY=long-random-secret-for-token-encryption
EMBEDDING_MODEL=text-embedding-004
GITHUB_TOKEN=github-personal-access-token-with-read-access
GITHUB_REPOS=owner1/repo1,owner2/repo2
GITHUB_MAX_FILES=200
GITHUB_WEBHOOK_SECRET=webhook-shared-secret
GITHUB_CLIENT_ID=github-oauth-app-client-id
GITHUB_CLIENT_SECRET=github-oauth-app-client-secret
GITHUB_REDIRECT_URI=https://your-domain.com/oauth/github/callback
GOOGLE_CLIENT_ID=google-oauth-client-id
GOOGLE_CLIENT_SECRET=google-oauth-client-secret
GOOGLE_REDIRECT_URI=https://your-domain.com/oauth/google/callback
GOOGLE_TOKEN=google-access-token-fallback
GOOGLE_DOCS_MAX=20
GOOGLE_CALENDAR_MAX_EVENTS=25
GOOGLE_CALENDAR_ID=primary
OAUTH_STATE_SECRET=long-random-secret
PORT=8080
SYNC_INTERVAL_MINUTES=30
DEFAULT_SYNC_USER_ID=123456789
```

Notes:
- `GITHUB_TOKEN` and `GITHUB_REPOS` are required for GitHub ingestion during `/sync`.
- Per-user OAuth is supported via `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `GITHUB_REDIRECT_URI`, and `OAUTH_STATE_SECRET`.
- Per-user Google OAuth is supported via `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and `GOOGLE_REDIRECT_URI`.
- If a user has OAuth token stored, connector uses that token first; otherwise it falls back to `GITHUB_TOKEN`.
- Google connectors use per-user stored token first; otherwise they fall back to `GOOGLE_TOKEN`.
- OAuth tokens are encrypted at rest when `TOKEN_ENCRYPTION_KEY` is set.
- Google access tokens are auto-refreshed using stored refresh tokens when expired.
- `GITHUB_MAX_FILES` is optional and limits indexed files per repo per sync.
- `GITHUB_WEBHOOK_SECRET` is required for validating GitHub webhook signatures.
- `PORT` is required for web mode and webhook endpoint hosting.
- `DBURL` is required; RAG chunks and embeddings are persisted in Turso.
- `SYNC_INTERVAL_MINUTES` + `DEFAULT_SYNC_USER_ID` enable periodic worker sync.

## Run

```bash
go mod download
go build -o bin/takhtit-bot
./bin/takhtit-bot
```

## Deploy (Heroku)

Current process types:

```procfile
web: RUN_MODE=web bin/takhtit-bot
worker: RUN_MODE=worker bin/takhtit-bot
```

HTTP endpoints:
- `GET /healthz`
- `GET /metrics`
- `POST /webhooks/github?user_id=<telegram_user_id>`
- `GET /oauth/github/callback`
- `GET /oauth/google/callback`

RAG storage notes:
- The index is now persisted in Turso (SQLite/libSQL), not in-memory.
- Embeddings are generated via Gemini embedding model and stored per chunk in Turso.
- `/ask` responses append source citations based on retrieved context.
- Retrieval uses hybrid ranking (vector similarity + lexical overlap).

## Next Implementation Targets

1. Add integration tests that exercise live OAuth callback and refresh flows with mocked providers.
2. Add richer per-source sync metrics and error-cardinality tracking.
3. Add admin command to inspect recent sync failures by user.