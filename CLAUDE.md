# tg-fsyn

Telegram-бот на Go для сохранения файлов и мониторинга задач скачивания Synology DownloadStation.

## Quick Reference

```bash
# Build
go build ./...

# Test
go test -race -v ./...

# Docker build + push (auto-increments version)
./build.sh

# Docker image: ag0n1k/tg-fsync
```

## Architecture

Монолитное Go-приложение без фреймворков, одна точка входа `main()`.

### Files

| File | Purpose |
|------|---------|
| `main.go` | Bot struct, Telegram message handlers, main() |
| `status_service.go` | StatusService — periodic polling, caching, notifications |
| `synology.go` | SynologyClient interface + HTTP implementation |
| `status_service_test.go` | Unit tests with mocks |

### Key Interfaces

- **`SynologyClient`** — `FetchTasks() ([]Task, error)`. Production: `synologyHTTPClient`. Tests: `mockSynologyClient`.
- **`BotSender`** — `Send(tgbotapi.Chattable) (tgbotapi.Message, error)`. Satisfied by `*tgbotapi.BotAPI`. Tests: `mockBotSender`.

### StatusService

- Polls Synology every 5 minutes (`StatusUpdateInterval`) via a `time.Ticker` that **never stops**
- Caches tasks in memory, protected by `sync.RWMutex`
- Detects status changes and sends Telegram notifications to admin users
- Graceful shutdown via `stopCh` channel
- `checkStatus()` is also called directly by `forceStatusUpdate()` (on file upload) — safe for concurrent use

### Bot Commands

| Command | Handler | Access |
|---------|---------|--------|
| `/start` | Welcome message | All allowed users |
| `/help` | Help text | All allowed users |
| `/id` | Show user ID | All allowed users |
| `/status` | Cached download tasks | All allowed users |
| `/admin list\|add\|remove\|status` | User management | Admin users only |

### Access Control

- `ALLOWED_USERS` env — comma-separated Telegram user IDs. Empty = allow all.
- `ADMIN_USERS` env — comma-separated admin IDs. Admins receive status change notifications.

## Environment Variables

Required: `TELEGRAM_BOT_TOKEN`, `SYNOLOGY_USERNAME`, `SYNOLOGY_PASSWORD`

Optional: `SYNOLOGY_HOST` (default `192.168.1.34`), `SYNOLOGY_PORT` (default `5000`), `STORAGE_PATH` (default `./files`), `ALLOWED_USERS`, `ADMIN_USERS`

## Docker

Multi-stage build. Runs as non-root user (UID/GID 1026 for Synology compatibility). Storage at `/app/files`.

## Conventions

- Go 1.25, module name `tg-fsyn`
- Telegram lib: `github.com/go-telegram-bot-api/telegram-bot-api/v5`
- No ORM, no database — in-memory state only
- Tests use short tick intervals (50ms) for fast execution
- Docker image versioned via `version` file, auto-incremented by `build.sh`
