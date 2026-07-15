#!/usr/bin/env bash
# Watch a running campaign's cumulative token spend and stop it at a ceiling.
#
# orchestrate.py has no budget concept: it runs every job it planned, however
# many tokens that takes. When a caller says "do not exceed N tokens" the only
# honest way to honor it is to measure and stop, not to estimate up front and
# hope — a per-run average is a guess, and a campaign that drifts 30% over it
# blows the ceiling silently.
#
#   ./budget_guard.sh <runs-dir> <max-tokens> <orchestrate-pid>
#
# Polls every 30s, kills the campaign the moment cumulative usage crosses the
# ceiling, and reports what it stopped at. Runs already in flight are allowed to
# finish; their spend is counted but cannot be un-spent.
set -uo pipefail

RUNS_DIR="${1:?usage: budget_guard.sh <runs-dir> <max-tokens> <pid>}"
MAX="${2:?}"
PID="${3:?}"
POLL="${POLL:-30}"

log() { echo "[budget $(date '+%H:%M:%S')] $*"; }

total() {
  python3 - "$RUNS_DIR" <<'PY'
import json, glob, sys
t = 0
for f in glob.glob(f"{sys.argv[1]}/*/record.json"):
    try:
        t += (json.load(open(f)).get("usage_total") or {}).get("total", 0)
    except Exception:
        pass
print(t)
PY
}

log "watching pid $PID · ceiling $(printf "%'d" "$MAX") tokens"
while kill -0 "$PID" 2>/dev/null; do
  T=$(total)
  PCT=$(( T * 100 / MAX ))
  log "$(printf "%'d" "$T") tokens · ${PCT}% of ceiling"
  if [ "$T" -ge "$MAX" ]; then
    log "CEILING REACHED — stopping the campaign"
    pkill -TERM -P "$PID" 2>/dev/null
    kill -TERM "$PID" 2>/dev/null
    sleep 5
    pkill -f acp-harness 2>/dev/null
    log "stopped at $(printf "%'d" "$(total)") tokens"
    exit 0
  fi
  sleep "$POLL"
done
log "campaign ended on its own at $(printf "%'d" "$(total)") tokens (under ceiling)"
