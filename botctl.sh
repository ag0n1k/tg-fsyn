#!/bin/sh
# Control script for the tg-fsyn Telegram bot on the Synology NAS (giza).
#
#   ./botctl.sh start | stop | restart | status | log [n]
#
# Shares the watchdog's lock file, so a control action can never race a
# cron tick into spawning a second instance — two bots on one token get
# Telegram 409 Conflict.
#
# `stop` drops a .disabled flag that watchdog.sh honours; without it the
# watchdog would resurrect the bot within 5 minutes. `start` clears it.

BOT_DIR=/var/services/homes/ag0n1k/tg-fsyn
LOCK="$BOT_DIR/.watchdog.lock"
DISABLED="$BOT_DIR/.disabled"
LOG="$BOT_DIR/tg-fsyn.log"

# busybox ps shows the bot as the relative path it was launched with
# ("./tg-fsyn"); the [.] trick keeps this grep from matching itself.
bot_pids() { ps -ef | grep "[.]/tg-fsyn" | awk '{print $2}'; }

# Wait for the lock rather than bailing out: unlike the watchdog, an
# interactive action should happen, even if a cron tick is mid-flight.
exec 9>"$LOCK"
flock -w 30 9 || { echo "cannot acquire $LOCK after 30s — is a stale process holding it?"; exit 1; }

do_stop() {
  pids=$(bot_pids)
  if [ -z "$pids" ]; then
    echo "stopped: not running"
    return 0
  fi
  echo "stopping: $pids"
  kill $pids 2>/dev/null
  # Give it a few seconds to exit cleanly, then escalate.
  i=0
  while [ $i -lt 10 ]; do
    sleep 1
    [ -z "$(bot_pids)" ] && { echo "stopped"; return 0; }
    i=$((i + 1))
  done
  echo "did not exit on TERM, sending KILL"
  kill -9 $(bot_pids) 2>/dev/null
  sleep 1
  [ -z "$(bot_pids)" ] && echo "stopped" || { echo "FAILED to stop: $(bot_pids)"; return 1; }
}

do_start() {
  pids=$(bot_pids)
  if [ -n "$pids" ]; then
    echo "already running: $pids"
    return 0
  fi
  cd "$BOT_DIR" || exit 1
  # 9>&- so the bot does not inherit (and thus hold) the lock fd.
  nohup ./tg-fsyn >> "$LOG" 2>&1 < /dev/null 9>&- &
  # Startup does a getMe round-trip. On a DNS fault that call sits in a
  # ~10s resolver timeout and only then kills the process, so a short
  # sleep here would report success about an already-doomed bot.
  sleep 20
  pids=$(bot_pids)
  if [ -z "$pids" ]; then
    echo "FAILED to start — last log lines:"
    tail -6 "$LOG"
    return 1
  fi
  echo "started: $pids"
  grep -E "Authorized on account" "$LOG" | tail -1
  tail -2 "$LOG"
}

case "$1" in
  start)
    rm -f "$DISABLED"
    do_start
    ;;
  stop)
    # Flag first, so a cron tick that fires mid-stop won't restart it.
    date '+%Y/%m/%d %H:%M:%S stopped via botctl' > "$DISABLED"
    do_stop
    ;;
  restart|"")
    rm -f "$DISABLED"
    do_stop || exit 1
    # Telegram needs a moment to release the getUpdates session, or the
    # fresh instance gets 409 Conflict from the old one.
    sleep 5
    do_start
    ;;
  status)
    pids=$(bot_pids)
    if [ -n "$pids" ]; then
      echo "running: $pids"
      ps -ef | grep "[.]/tg-fsyn"
    else
      echo "NOT running"
    fi
    [ -f "$DISABLED" ] && echo "watchdog disabled: $(cat "$DISABLED")"
    echo "--- resolv.conf ---"
    cat /etc/resolv.conf
    echo "--- last log lines ---"
    tail -5 "$LOG"
    ;;
  log)
    tail -"${2:-40}" "$LOG"
    ;;
  *)
    echo "usage: $0 {start|stop|restart|status|log [n]}" >&2
    exit 2
    ;;
esac
