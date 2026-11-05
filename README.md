# Leyendas Bot

Leyendas Bot is a Telegram bot written in Go that runs a team's project workflow from a group chat. It connects Notion tasks, Google Calendar meetings and daily standups, and it accepts both slash commands and natural-language requests.

Every command has a matching AI tool, so a request produces the same result whether it is typed as `/done` or written as a sentence.

## Features

- Standups: posts a daily prompt, collects threaded replies, sends reminders and publishes a digest
- Task sync: polls Notion for changes and notifies assignees about new and completed tasks
- Deadline reminders: sends direct messages 48 hours and 24 hours before a deadline and on the due date, then escalates to the group chat once a task is more than 24 hours overdue
- Meeting scheduling: creates Google Calendar events with Meet links and adds matching Notion meeting notes
- Natural-language actions: turns mentions into tool calls against Notion and Google Calendar, using the sender's identity
- Availability: checks calendar free/busy data to find open slots

## Architecture

```text
Telegram <--> Bot handlers <--> AI agent <--> Tool executor
                   |                               |
                   |                     +---------+---------+
                   |                     |                   |
              Scheduler              Notion API        Google Calendar
                   |
             Turso (libSQL)
```

- The bot layer handles commands, mentions and standup replies through `gotgbot`.
- The AI layer sends messages to Gemma 4 through the Google GenAI SDK and runs the returned tool calls.
- The executor holds the business logic shared by commands and AI tools.
- The scheduler runs cron jobs with overlap protection and panic recovery.
- The database stores team members, task snapshots, reminder state, and standup rounds and replies. Snapshots and reminder state keep notifications idempotent.
- The Notion client limits requests to three per second.

## Commands

| Command | AI tool | Description |
|---|---|---|
| `/register <email> [role]` | `register_member` | Registers a member in Notion and the database. Direct message only |
| `/whoami` | `get_my_identity` | Shows the sender's identity |
| `/tasks` | `list_open_tasks` | Lists open tasks by assignee |
| `/mytasks` | `list_my_tasks` | Sends the sender their open tasks by direct message |
| `/task <name or id>` | `get_task_details` | Shows one task |
| `/done <name or id>` | `mark_task_done` | Marks a task as done in Notion |
| `/schedule <time> [mins] [title]` | `schedule_meeting` | Schedules a Meet call and adds a Notion meeting row |
| `/standup` | `trigger_standup_round` | Starts or shows today's standup |
| `/agenda`, `/meetings` | `get_upcoming_meetings` | Lists upcoming meetings |
| `/status` | `get_workspace_status` | Shows open tasks, the next deadline and the next meeting |
| `/help` | None | Shows the help guide |
| Mention | `create_task` | Creates a task, for example `@bot I'll take the backend migration, due next Tuesday` |

## Getting started

### Requirements

- Go 1.26 or later
- A Telegram bot token from [@BotFather](https://t.me/BotFather)
- A Notion integration with access to the Tasks, Team and Meetings databases
- A [Turso](https://turso.tech/) database, or local SQLite
- A Google AI API key
- A Google Cloud project with the Calendar API enabled

### Configuration

```bash
cp .env.example .env
```

| Variable | Description |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Telegram bot token |
| `TELEGRAM_GROUP_CHAT_ID` | Team group chat ID |
| `TURSO_DATABASE_URL` | Database URL. Leave empty for local SQLite |
| `TURSO_AUTH_TOKEN` | Turso auth token |
| `NOTION_TOKEN` | Notion integration token |
| `NOTION_TASKS_DB_ID` | Tasks database ID |
| `NOTION_TEAM_DB_ID` | Team database ID |
| `NOTION_MEETINGS_DB_ID` | Meetings database ID |
| `GEMINI_API_KEY` | Google AI API key |
| `GEMMA_MODEL_NAME` | Model name. Defaults to `gemma-4-31b` |
| `GOOGLE_CLIENT_ID` | OAuth client ID |
| `GOOGLE_CLIENT_SECRET` | OAuth client secret |
| `GOOGLE_REDIRECT_URL` | OAuth redirect URL |
| `GOOGLE_REFRESH_TOKEN` | Generated in the next step |
| `STANDUP_CRON` | Standup prompt schedule. Example: `0 9 * * 1-5` |
| `STANDUP_DIGEST_CRON` | Digest schedule. Example: `0 15 * * 1-5` |
| `NOTION_POLL_INTERVAL` | Notion polling interval. Example: `5m` |
| `DEADLINE_CHECK_INTERVAL` | Deadline check interval. Example: `15m` |

### Authorize Google Calendar

```bash
go run ./cmd/auth
```

Open the printed link, grant Calendar access, and copy the resulting `GOOGLE_REFRESH_TOKEN` into `.env`.

### Run

```bash
go run ./cmd/bot
```

### Test

```bash
go test ./...
```

## Project layout

```text
cmd/
  bot/          Bot daemon
  auth/         Google OAuth token generator
internal/
  ai/           Model client, tool definitions and executor
  bot/          Telegram setup, commands, mentions and standup replies
  calendar/     OAuth client, events and free/busy lookups
  config/       Environment configuration
  db/           libSQL store, migrations and repositories
  notion/       Rate-limited client and database repositories
  scheduler/    Standups, reminders and Notion sync jobs
```

## Built with

- [gotgbot](https://github.com/PaulSonOfLars/gotgbot) for the Telegram Bot API
- [Google GenAI SDK for Go](https://pkg.go.dev/google.golang.org/genai)
- [Notion API](https://developers.notion.com/) and [Google Calendar API](https://developers.google.com/calendar)
- [Turso](https://turso.tech/) (libSQL)
- [robfig/cron](https://github.com/robfig/cron)
