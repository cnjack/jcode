#!/usr/bin/env bash
# Arm the computer-use eval campaign to fire the moment TokenHub quota returns.
#
# Why this exists: the campaign requires tencent-tokenhub/kimi-k2.7-code, whose
# free quota is exhausted (HTTP 402). No amount of retrying creates quota — it
# needs a human to enable postpaid billing at
#   https://console.cloud.tencent.com/tokenhub/inference
# Rather than leave the run as a manual TODO that gets forgotten, this polls
# cheaply (one 5-token request every 5 min) and launches the full campaign on the
# first 200.
#
#   nohup agent-eval/suite/run_when_quota.sh > /tmp/cu-armed.log 2>&1 &
#   tail -f /tmp/cu-armed.log
#
# Kill with: pkill -f run_when_quota
set -uo pipefail

MODEL="${MODEL:-kimi-k2.7-code}"
RUNS_DIR="${RUNS_DIR:-/tmp/cu-final}"
BIN="${BIN:-/tmp/jcode-cu}"
HARNESS="${HARNESS:-/tmp/acp-harness}"
REPEAT_SCALE="${REPEAT_SCALE:-10}"
WORKERS="${WORKERS:-2}"
POLL_SECS="${POLL_SECS:-300}"
MAX_HOURS="${MAX_HOURS:-24}"

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
KEY=$(python3 -c "import json,os;print(json.load(open(os.path.expanduser('~/.jcode/config.json')))['providers']['tencent-tokenhub']['api_key'])")

log() { echo "[$(date '+%F %T')] $*"; }

quota_ok() {
  local code
  code=$(curl -s -o /tmp/.quota-probe.json -w "%{http_code}" \
    -X POST https://tokenhub.tencentmaas.com/v1/chat/completions \
    -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
    -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}],\"max_tokens\":5}" \
    --max-time 20)
  [ "$code" = "200" ]
}

log "armed: polling $MODEL every ${POLL_SECS}s (giving up after ${MAX_HOURS}h)"
log "enable billing at https://console.cloud.tencent.com/tokenhub/inference to start"

deadline=$(( $(date +%s) + MAX_HOURS * 3600 ))
until quota_ok; do
  if [ "$(date +%s)" -ge "$deadline" ]; then
    log "gave up after ${MAX_HOURS}h — quota never returned. Nothing was run."
    exit 1
  fi
  sleep "$POLL_SECS"
done

log "quota is back — launching the campaign"

# Rebuild so the campaign runs against the current branch, not a stale binary.
( cd "$REPO" && CGO_ENABLED=0 go build -o "$BIN" ./cmd/jcode ) || { log "jcode build failed"; exit 1; }
( cd "$REPO/agent-eval/harness" && go build -o "$HARNESS" . ) || { log "harness build failed"; exit 1; }

rm -rf "$RUNS_DIR" && mkdir -p "$RUNS_DIR"
start=$(date +%s)
python3 "$REPO/agent-eval/suite/orchestrate.py" \
  --bin "$BIN" --harness "$HARNESS" \
  --runs-dir "$RUNS_DIR" --models "$MODEL" \
  --repeat-scale "$REPEAT_SCALE" --workers "$WORKERS"
elapsed=$(( ($(date +%s) - start) / 60 ))
log "campaign finished in ${elapsed} min"

# Report on runs that ACTUALLY ran. The harness scores 402'd runs as passes
# (agent-eval F2), so raw aggregates are worse than useless — see
# internal-doc/computer-use-test-report.md §3.
python3 - "$RUNS_DIR" <<'PY'
import json, glob, sys, collections
runs = glob.glob(f"{sys.argv[1]}/*/record.json")
def tok(d): return (d.get("usage_total") or {}).get("total", 0)
real, dead = [], []
for f in runs:
    d = json.load(open(f))
    (real if tok(d) > 0 else dead).append(d)
print(f"\n=== {len(runs)} runs: {len(real)} real, {len(dead)} killed by 402 ===")
if dead:
    print(f"!! {sum(1 for d in dead if d.get('task_passed'))} of the dead runs were still "
          f"scored as PASSING — agent-eval F2. Discard them.")
by = collections.defaultdict(lambda: [0, 0])
for d in real:
    t = d.get("tier", "?"); by[t][1] += 1
    if d.get("task_passed"): by[t][0] += 1
tot = [0, 0]
for t, (p, n) in sorted(by.items()):
    print(f"  {t:10s} {p:3d}/{n:3d}  {100*p/n:5.1f}%"); tot[0] += p; tot[1] += n
if tot[1]:
    print(f"  {'TOTAL':10s} {tot[0]:3d}/{tot[1]:3d}  {100*tot[0]/tot[1]:5.1f}%")
hours = sum(d.get("wall_s", 0) for d in real) / 3600
print(f"\nagent wall-clock across real runs: {hours:.2f} h")
PY
