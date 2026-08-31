# 🤖 Leyendas Bot — Telegram Bot + Notion Workflow

Production-grade Telegram bot written in Go powering the Leyendas Capstone workspace. Seamlessly synchronizes Notion databases, Google Calendar/Meet schedules, daily standups, deadline reminders, and natural-language AI actions powered by **Gemma 4 31B** with 1:1 slash-command-to-AI-tool parity.

---

## 🌟 Key Features

- **🌅 Standup Automation**: Scheduled daily prompts (9:00 AM) with in-thread replies, DM reminder nudges, and 3:00 PM summary digests.
- **🔄 Notion ↔ Telegram Task Sync**: Polling engine backed by an idempotent Turso libSQL mirror (`task_snapshots`) preventing repeated notifications. Celebrates completed tasks with celebratory pings and notifies assignees of new tasks.
- **⏰ Smart Deadline Reminders**: Automatic 48h, 24h, and due-date DM reminders with 24h+ overdue escalation pings to the group chat.
- **🗓️ Google Calendar & Meet Scheduler**: One-click meeting creation with auto-generated Google Meet links (`conferenceDataVersion=1`) and synchronized Notion Meeting Notes rows.
- **🧠 Gemma 4 31B AI Natural Language Layer**: Mentions (`@bot I'll take the login page redesign, due Friday`) trigger structured tool calling against Notion and Google Calendar with sender identity injection.
- **⚡ Complete Command & AI Parity**: Every Telegram command (`/tasks`, `/mytasks`, `/task`, `/done`, `/schedule`, `/standup`, `/agenda`, `/status`, `/register`, `/whoami`) maps directly to an AI tool call counterpart.

---

## 🏗️ Architecture

```
Leyendas-Bot/
├── cmd/
│   ├── bot/
│   │   └── main.go           # Main Daemon entrypoint (gotgbot, scheduler, services)
│   └── auth/
│       └── main.go           # Interactive CLI Google Calendar OAuth token generator
├── internal/
│   ├── ai/
│   │   ├── agent.go          # Google GenAI Go SDK client (Gemma 4 31B)
│   │   ├── executor.go       # Unified tool execution & business logic
│   │   └── tools.go          # OpenAPI tool definitions (11 tools)
│   ├── bot/
│   │   ├── bot.go            # gotgbot/v2 setup, dispatcher, message filters
│   │   ├── commands.go       # Handlers for /start, /register, /tasks, /schedule, etc.
│   │   ├── mentions.go       # @bot natural language message routing
│   │   └── standup.go        # Thread reply listener for standup responses
│   ├── calendar/
│   │   ├── client.go         # OAuth2 client & persisting token source
│   │   ├── events.go         # CreateEventWithMeet (Hangouts Meet link generator)
│   │   └── freebusy.go       # Availability checking & slot finding
│   ├── config/
│   │   └── config.go         # Environment & .env loader
│   ├── db/
│   │   ├── members.go        # Team members repository
│   │   ├── reminders.go      # Idempotent reminder state & task snapshot tracking
│   │   ├── standups.go       # Standup cycles & thread responses
│   │   └── store.go          # Turso libSQL connection pool & schema migrations
│   ├── notion/
│   │   ├── client.go         # Rate-limited REST client (2022-06-28, 3 req/s)
│   │   ├── meetings.go       # Meeting Notes DB repository
│   │   ├── models.go         # Strongly-typed Notion DTOs
│   │   ├── tasks.go          # Tasks DB repository (CRUD, filters, relations)
│   │   └── team.go           # The Team DB repository
│   └── scheduler/
│       ├── reminders.go      # Deadline evaluator (48h, 24h, due today, overdue >24h)
│       ├── scheduler.go      # robfig/cron coordinator with SkipIfStillRunning
│       ├── standups.go       # Standup prompt & digest jobs
│       └── sync.go           # Notion change detection engine
├── .env.example
├── go.mod
└── go.sum
```

---

## 🚀 Quickstart & Setup

### 1. Prerequisites
- **Go 1.26+** installed (`go version`)
- A **Telegram Bot Token** from [@BotFather](https://t.me/BotFather)
- A **Notion Integration Token** (shared with your workspace databases)
- A **Turso Database** (or local SQLite fallback)
- A **Google GenAI API Key** (for Gemma 4 31B)
- A **Google Cloud Project** with Google Calendar API enabled

---

### 2. Environment Configuration
Copy the configuration template:
```bash
cp .env.example .env
```

Configure your credentials in `.env`:
```env
# Telegram
TELEGRAM_BOT_TOKEN=123456789:ABCdefGHIjklMNOpqrSTUvwxYZ
TELEGRAM_GROUP_CHAT_ID=-1001234567890

# Turso DB (or leave empty for local SQLite)
TURSO_DATABASE_URL=libsql://your-database-name.turso.io
TURSO_AUTH_TOKEN=your_turso_auth_token

# Notion
NOTION_TOKEN=secret_your_notion_integration_token
NOTION_TASKS_DB_ID=your_tasks_database_id
NOTION_TEAM_DB_ID=your_the_team_database_id
NOTION_MEETINGS_DB_ID=your_meetings_database_id

# Google GenAI / Gemma 4 31B
GEMINI_API_KEY=your_gemini_api_key
GEMMA_MODEL_NAME=gemma-4-31b

# Google Calendar
GOOGLE_CLIENT_ID=your_client_id.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=your_client_secret
GOOGLE_REDIRECT_URL=http://localhost:8085/oauth/callback
GOOGLE_REFRESH_TOKEN=your_generated_refresh_token
```

---

### 3. Generate Google Calendar OAuth Refresh Token
Run the interactive CLI authorization tool:
```bash
go run ./cmd/auth
```
1. Open the generated link in your browser.
2. Sign in and grant Calendar permissions.
3. Copy the output `GOOGLE_REFRESH_TOKEN` into your `.env`.

---

### 4. Build and Run
```bash
# Run unit tests
go test -v ./...

# Start the bot daemon
go run ./cmd/bot
```

---

## 💬 Telegram Commands & AI Tool Parity

| Slash Command | AI Tool Counterpart | Description |
| :--- | :--- | :--- |
| `/register <email> [role]` | `register_member` | Register your identity (DM only) creating a page in Notion + Turso record |
| `/whoami` | `get_my_identity` | View your resolved identity details |
| `/tasks` | `list_open_tasks` | List all open workspace tasks grouped by assignee |
| `/mytasks` | `list_my_tasks` | DM the requester their personal open tasks |
| `/task <name/id>` | `get_task_details` | Quick lookup of a specific task |
| `/done <name/id>` | `mark_task_done` | Mark a task as Done in Notion |
| `/schedule <time> [mins] [title]` | `schedule_meeting` | Schedule Google Meet call + log Notion Meeting Notes row |
| `/standup` | `trigger_standup_round` | Manually initiate or query today's standup round |
| `/agenda` or `/meetings` | `get_upcoming_meetings` | View upcoming meetings and Google Doc discussion links |
| `/status` | `get_workspace_status` | Overview: open tasks count, next deadline, next meeting |
| `/help` | — | Display help guide |
| *Natural Language Mention* | `create_task` | e.g. `@bot I'll take the backend migration, due next Tuesday` |

---

## 🛡️ License
MIT License. Built for Leyendas Capstone.
