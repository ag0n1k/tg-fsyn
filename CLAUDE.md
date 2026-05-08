# tg-fsyn

Telegram-бот на Go для сохранения файлов и мониторинга задач скачивания Synology DownloadStation. Запускается нативно на хосте Synology (не в Docker), чтобы иметь прямой доступ к `synoindex`.

## Quick Reference

```bash
# Build (local arch)
go build ./...

# Test
go test -race -v ./...

# Cross-compile for the NAS (Synology aarch64)
GOOS=linux GOARCH=arm64 go build -o tg-fsyn-arm64 .
```

История прежнего Docker-варианта осталась в git history (см. коммиты до удаления `Dockerfile`/`build.sh`/`cmd/reindex-sidecar`).

## Deployment (Synology NAS)

Target: `192.168.1.34` (aarch64), user `ag0n1k` (UID 1026), binary at `~/tg-fsyn/tg-fsyn`, env file `~/tg-fsyn/.env` (godotenv loads it from cwd at startup).

DSM quirks that bit us:
- **No SFTP subsystem** — must use `scp -O` (legacy SCP protocol). Plain `scp` fails with `subsystem request failed on channel 0`.
- **`docker` not in SSH PATH** — call it as `/usr/local/bin/docker` (or `/var/packages/ContainerManager/target/usr/bin/docker`).
- **busybox `ps`** shows the binary as the relative path it was invoked with (`./tg-fsyn`), not the absolute path. So `pgrep -f tg-fsyn/tg-fsyn` and `grep "tg-fsyn/tg-fsyn"` find nothing — use `ps -ef | grep "\./tg-fsyn"` or `pkill -f '\./tg-fsyn$'`. **This caused three stacked instances on stacked redeploys** (each kill missed, each new bot got 409 Conflict from Telegram). Always kill by PID after a verifying `ps` listing.
- **No `pgrep`** in default PATH; `pkill` exists but be wary of patterns.
- **Telegram getUpdates**: only one bot instance per token. If a redeploy leaves the old process running, both get 409 Conflict.

Redeploy procedure (manual, no script yet):
```bash
GOOS=linux GOARCH=arm64 go build -o tg-fsyn-arm64 .
scp -O tg-fsyn-arm64 192.168.1.34:~/tg-fsyn/tg-fsyn.new
ssh 192.168.1.34 '
  pkill -f "\./tg-fsyn$" || true
  sleep 5  # let Telegram release the getUpdates session
  mv ~/tg-fsyn/tg-fsyn.new ~/tg-fsyn/tg-fsyn
  chmod +x ~/tg-fsyn/tg-fsyn
  cd ~/tg-fsyn && nohup ./tg-fsyn >> tg-fsyn.log 2>&1 < /dev/null &
  disown
'
```

Verify after start: `tail ~/tg-fsyn/tg-fsyn.log` — expect `Authorized on account TorDownBot` and *no* `Conflict: terminated by other getUpdates request` lines after that point.

Autostart: DSM Control Panel → Task Scheduler → Triggered Task → Boot-up → User: `ag0n1k` → command:
```
pkill -f '\./tg-fsyn$' ; cd /var/services/homes/ag0n1k/tg-fsyn && nohup ./tg-fsyn >> tg-fsyn.log 2>&1 &
```
The leading `pkill` is defensive — guards against zombie processes from a botched manual restart.

### Secrets handling

`~/tg-fsyn/.env` (mode 600) is the source of truth. To migrate from the old Docker container without leaking secrets to a transcript:
```bash
ssh 192.168.1.34 'umask 077 && /usr/local/bin/docker inspect <container> --format "{{range .Config.Env}}{{println .}}{{end}}" | grep -E "^(SYNOLOGY_|TELEGRAM_|ALLOWED_|ADMIN_|STORAGE_)" > ~/tg-fsyn/.env'
```
Then patch `STORAGE_PATH` (was `/app/files` in container, now `/volume1/torrents` natively) and append `REINDEX_PATHS=/volume1/video`. **Never `docker inspect ... .Config.Env` without filtering the output** — it dumps `TELEGRAM_BOT_TOKEN` and `SYNOLOGY_PASSWORD` to stdout.

### Old Docker container

Image `ag0n1k/tg-fsync:v0.3.2` (note: image name had `fsync`, repo is `fsyn`), container name `tg-fsync`. After successful native deploy it's left as `Exited` for emergency rollback (`docker start tg-fsync`). Mount was `/volume1/torrents → /app/files`.

## Architecture

Монолитное Go-приложение без фреймворков, одна точка входа `main()`.

### Files

| File | Purpose |
|------|---------|
| `main.go` | Bot struct, Telegram message handlers, main() |
| `status_service.go` | StatusService — periodic polling, caching, notifications |
| `synology.go` | SynologyClient interface + HTTP implementation |
| `reindex.go` | Reindexer — wraps `synoindex -A` via os/exec |
| `status_service_test.go` | Unit tests with mocks |

### Key Interfaces

- **`SynologyClient`** — `FetchTasks() ([]Task, error)`. Production: `synologyHTTPClient`. Tests: `mockSynologyClient`.
- **`BotSender`** — `Send(tgbotapi.Chattable) (tgbotapi.Message, error)`. Satisfied by `*tgbotapi.BotAPI`. Tests: `mockBotSender`.
- **`Reindexer`** — `Reindex() (string, error)`. Production: `synoindexRunner` (calls `synoindex -A <path>` for each configured path). Set to nil if the synoindex binary isn't present at startup — `/reindex` then replies that it's unavailable.

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
| `/reindex` | Recent finished tasks + inline button to run `synoindex -A` on configured paths | All allowed users |
| `/admin list\|add\|remove\|status` | User management | Admin users only |

Callback queries are dispatched in `handleCallback` (currently only `reindex`).

### Access Control

- `ALLOWED_USERS` env — comma-separated Telegram user IDs. Empty = allow all.
- `ADMIN_USERS` env — comma-separated admin IDs. Admins receive status change notifications.

## Environment Variables

Required: `TELEGRAM_BOT_TOKEN`, `SYNOLOGY_USERNAME`, `SYNOLOGY_PASSWORD`

Optional:
- `SYNOLOGY_HOST` (default `192.168.1.34`), `SYNOLOGY_PORT` (default `5000`)
- `STORAGE_PATH` (default `./files`)
- `ALLOWED_USERS`, `ADMIN_USERS`
- `SYNOINDEX_BIN` (default `/usr/syno/bin/synoindex`) — feature auto-disables if file doesn't exist
- `REINDEX_PATHS` (default `/volume1/video`) — comma-separated paths to pass to `synoindex -A`

## Conventions

- Go 1.25, module name `tg-fsyn`
- Telegram lib: `github.com/go-telegram-bot-api/telegram-bot-api/v5`
- No ORM, no database — in-memory state only
- Tests use short tick intervals (50ms) for fast execution
