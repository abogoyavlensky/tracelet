#!/usr/bin/env bash
# Run the storage spike experiments from the plan one after another, each on
# a fresh data dir, so no two measurements compete for the machine. Reports
# land in <repo>/.tmp/experiments/; progress goes to its status.log.
#
# Run it detached so it survives the terminal that started it:
#   setsid nohup spike/storage/scripts/experiments.sh > /dev/null 2>&1 &
set -uo pipefail

spike_dir="$(cd "$(dirname "$0")/.." && pwd)"
repo_dir="$(cd "$spike_dir/../.." && pwd)"
bin="$spike_dir/bin/spike"
out="$repo_dir/.tmp/experiments"
status="$out/status.log"

rm -rf "$out"
mkdir -p "$out"
log() { echo "$(date -u +%FT%TZ) $*" >> "$status"; }

(cd "$spike_dir" && go build -trimpath -o bin/spike ./cmd/spike) || { log "FAIL build"; exit 1; }

# step <name> <command...>: runs the command with stdout to <name>.json and
# stderr plus /usr/bin/time -v to <name>.log.
step() {
  local name="$1"
  shift
  log "start $name"
  if /usr/bin/time -v "$@" > "$out/$name.json" 2> "$out/$name.log"; then
    log "ok    $name"
  else
    log "FAIL  $name (exit $?, see $name.log)"
    exit 1
  fi
}

full="$out/spike-full"

# 1. Full 7-day dataset, no retention.
step full-7d "$bin" run --profile busy --days 7 --data-dir "$full"

# 2. Retention run: 3 days by age and a 2 GB ring buffer.
step retained-7d "$bin" run --profile busy --days 7 --retention-days 3 --max-bytes 2000000000 \
  --data-dir "$out/spike-retained"

# 3. Real-time ingestion with two query goroutines, on top of the full dataset.
step full-rt "$bin" run --profile busy --real-time --minutes 5 --query-load 2 --data-dir "$full"

# 4. Trace lookup over the full window.
step lookup "$bin" lookup --data-dir "$full" --samples 100

# 5. Backup at scale.
step backup "$bin" backup --data-dir "$full" --out "$out/spike-backup"

log "done"
