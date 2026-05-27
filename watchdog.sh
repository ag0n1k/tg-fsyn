#!/bin/sh
# Watchdog for the tg-fsyn Telegram bot, run on the Synology NAS.
# Invoked from /etc/crontab every 5 minutes as user ag0n1k:
#   */5 * * * * ag0n1k /var/services/homes/ag0n1k/tg-fsyn/watchdog.sh
#
# It restarts the bot if the process is gone. This covers BOTH failure
# modes we have hit: the NAS rebooting (boot-up task is flaky) and the
# bot process dying on a network error while the NAS stays up
# (a boot-up trigger can never recover from that).
#
# Idempotent and flock-guarded so overlapping cron ticks can never spawn
# a second instance — two bots on one token get Telegram 409 Conflict.

BOT_DIR=/var/services/homes/ag0n1k/tg-fsyn
LOCK="$BOT_DIR/.watchdog.lock"

# Serialize watchdog runs; bail out if another tick still holds the lock.
exec 9>"$LOCK"
flock -n 9 || exit 0

# busybox ps shows the bot as the relative path it was launched with
# ("./tg-fsyn"); the [.] trick keeps this grep from matching itself.
if ps -ef | grep -q "[.]/tg-fsyn"; then
  exit 0
fi

cd "$BOT_DIR" || exit 1
echo "$(date '+%Y/%m/%d %H:%M:%S') watchdog: bot not running, starting" >> "$BOT_DIR/watchdog.log"
nohup ./tg-fsyn >> "$BOT_DIR/tg-fsyn.log" 2>&1 < /dev/null &
