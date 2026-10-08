#!/usr/bin/env bash
# Run the memory follow-up experiments one after another, each on a fresh
# data dir. Reports land in <repo>/.tmp/memory/; progress goes to its
# status.log, and every invocation ends by writing summary.md.
#
# The script resumes: an existing <name>.json always means a completed run,
# and only steps without one run. FRESH=1 starts over.
#
#   E0-E5, one-day single-variable runs:
#     setsid nohup spike/storage/scripts/memory-experiments.sh > /dev/null 2>&1 < /dev/null &
#   E0 again at a 100 ms sample interval, when the 1 s peak missed VmHWM:
#     FINE=1 setsid nohup spike/storage/scripts/memory-experiments.sh > /dev/null 2>&1 < /dev/null &
#   E6-E7, confirmation with a combination of flags (may be empty):
#     CONFIRM=1 COMBO="--allocator-flush-threshold 16MB" setsid nohup spike/storage/scripts/memory-experiments.sh > /dev/null 2>&1 < /dev/null &
set -uo pipefail

spike_dir="$(cd "$(dirname "$0")/.." && pwd)"
repo_dir="$(cd "$spike_dir/../.." && pwd)"
bin="$spike_dir/bin/spike"
out="$repo_dir/.tmp/memory"
status="$out/status.log"
combo_file="$out/combo.txt"

CONFIRM="${CONFIRM:-0}"
COMBO="${COMBO-}"

if [[ "${FRESH:-0}" == 1 ]]; then
  rm -rf "$out"
fi
mkdir -p "$out"
log() { echo "$(date -u +%FT%TZ) $*" >> "$status"; }

(cd "$spike_dir" && go build -trimpath -o bin/spike ./cmd/spike) || { log "FAIL build"; exit 1; }

e6_dir="$out/data-e6-confirm-7d"

# A different combination invalidates the confirmation runs.
if [[ "$CONFIRM" == 1 && -f "$combo_file" && "$(cat "$combo_file")" != "$COMBO" ]]; then
  log "combo changed from '$(cat "$combo_file")' to '$COMBO': removing E6 and E7"
  rm -rf "$out/e6-confirm-7d.json" "$out/e7-confirm-rt.json" "$e6_dir"
fi
# E7 runs on E6's data dir, so an E7 that started and did not finish leaves
# it changed: both run again.
if [[ -f "$out/e7-confirm-rt.json.part" ]]; then
  log "E7 did not finish: removing E6 so both run again"
  rm -f "$out/e6-confirm-7d.json"
fi
rm -f "$out"/*.part

# step <name> <data-dir> <command...>: runs the command unless <name>.json
# exists, with stdout to <name>.json.part and stderr plus /usr/bin/time -v to
# <name>.log; the report is renamed into place only when the command exits 0.
# A step without a report starts from an empty data dir, unless called as
# keep=1 step ... to run on another step's data.
step() {
  local name="$1" data="$2"
  shift 2
  if [[ -f "$out/$name.json" ]]; then
    log "skip  $name (report exists)"
    return
  fi
  [[ "${keep:-0}" == 1 ]] || rm -rf "$data"
  log "start $name"
  if /usr/bin/time -v "$@" --data-dir "$data" > "$out/$name.json.part" 2> "$out/$name.log"; then
    mv "$out/$name.json.part" "$out/$name.json"
    log "ok    $name"
  else
    log "FAIL  $name (exit $?, see $name.log)"
    summarize
    exit 1
  fi
}

summarize() {
  "$bin" summarize "$out" > "$out/summary.md" 2>> "$status" || log "FAIL  summarize"
}

day=("$bin" run --profile busy --days 1 --threads 2)

step e0-baseline "$out/data-e0" "${day[@]}"
step e1-flush-threshold "$out/data-e1" "${day[@]}" --allocator-flush-threshold 16MB
step e2-background-threads "$out/data-e2" "${day[@]}" --allocator-background-threads
step e3-memory-limit-128 "$out/data-e3" "${day[@]}" --memory-limit 128MB
step e4-row-group-30k "$out/data-e4" "${day[@]}" --row-group-size 30000
step e5-unsorted "$out/data-e5" "${day[@]}" --unsorted-flush
if [[ "${FINE:-0}" == 1 ]]; then
  step e0-baseline-100ms "$out/data-e0-100ms" "${day[@]}" --memory-sample-interval 100ms
fi

if [[ "$CONFIRM" == 1 ]]; then
  echo "$COMBO" > "$combo_file"
  read -ra combo <<< "$COMBO"
  log "confirm with combo '$COMBO'"
  export GOMEMLIMIT=192MiB
  step e6-confirm-7d "$e6_dir" "$bin" run --profile busy --days 7 --threads 2 "${combo[@]}"
  # E7 adds to E6's data, so it must not wipe the data dir.
  keep=1 step e7-confirm-rt "$e6_dir" "$bin" run --profile busy --real-time --minutes 5 --query-load 2 --threads 2 \
    "${combo[@]}"
fi

summarize
log "done"
