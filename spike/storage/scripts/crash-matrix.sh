#!/usr/bin/env bash
# Crash the spike at each flush step as a real process death, then reconcile
# and verify that nothing was lost or duplicated. Run from anywhere; data dirs
# go under <repo>/.tmp/crash-<step>.
set -uo pipefail

spike_dir="$(cd "$(dirname "$0")/.." && pwd)"
repo_dir="$(cd "$spike_dir/../.." && pwd)"
bin="$spike_dir/bin/spike"

(cd "$spike_dir" && go build -trimpath -o bin/spike ./cmd/spike) || exit 1

failed=0
for step in written renamed recorded deleted; do
  dir="$repo_dir/.tmp/crash-$step"
  rm -rf "$dir"
  mkdir -p "$dir"

  "$bin" run --profile small --hours 3 --crash-at "$step" --data-dir "$dir" > "$dir.run.json" 2> "$dir.run.log"
  code=$?
  if [ "$code" -ne 137 ]; then
    echo "FAIL $step: run exited $code, want 137 (see $dir.run.log)"
    failed=1
    continue
  fi

  if ! "$bin" reconcile --data-dir "$dir" > "$dir.reconcile.json" 2>&1; then
    echo "FAIL $step: reconcile failed (see $dir.reconcile.json)"
    failed=1
    continue
  fi

  if ! "$bin" verify --data-dir "$dir" > "$dir.verify.json" 2>&1; then
    echo "FAIL $step: verify failed (see $dir.verify.json)"
    failed=1
    continue
  fi

  echo "ok   $step"
done

exit "$failed"
