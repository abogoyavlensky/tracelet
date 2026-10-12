#!/usr/bin/env bash
# Build the image around bin/tracelet, start it, and check /api/v1/health.
# `rite docker` builds the binary first. The server can accept a connection
# and reset it while it starts, which curl does not count as retryable,
# hence --retry-all-errors. No --rm, so a container that crashed still has
# its logs to print. With no TRACELET_ADMIN_TOKEN the server prints a
# bootstrap token to its log, which is harmless in a throwaway container.
set -u
docker build -t tracelet:local . || exit 1
docker rm -f tracelet-smoke > /dev/null 2>&1
docker run -d --name tracelet-smoke -p 8080:8080 tracelet:local > /dev/null || exit 1
status=0
curl -fsS --retry 15 --retry-all-errors --retry-delay 1 localhost:8080/api/v1/health || status=$?
echo
docker ps -a --filter name=tracelet-smoke
docker logs tracelet-smoke
docker rm -f tracelet-smoke > /dev/null
exit $status
