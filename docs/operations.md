[Back to README](../README.md)

# Operations

Maintainer reference for image builds, releases, and the hosted instance. For
using Tracelet, start with the [README](../README.md).

## Docker

The image (`debian:trixie-slim` plus `libstdc++6`) wraps a `bin/tracelet`
built outside it, using the Dockerfile in the repository. Its data directory
is `/app/data` (`TRACELET_DATA_DIR`); mount a volume there to keep the
databases and telemetry files. It listens on `:8080`. The entrypoint is the
binary and the default command is `serve`, so `docker run … image` serves and
`docker run --rm image version` prints the version. The published image is
this Dockerfile around each architecture's CI binary (see
[Releases](#releases)).

`rite docker` builds the binary, then the image, starts it and checks
`/api/v1/health`. CI runs the same script (`scripts/docker-smoke.sh`) on both
architectures for every pull request.

The binary must be built on a glibc no newer than trixie's 2.41. CI builds on
`ubuntu-24.04` (2.39), pinned for this reason. A binary built on a newer
system, such as Ubuntu 26.04 (glibc 2.43), does not start in the image and
fails with `GLIBC_2.4x not found`. Never ship a binary built elsewhere; CI is
the authority for `rite docker`.

## Releases

Pushing a tag `vX.Y.Z` runs `.github/workflows/release.yml`:

1. `test` calls `ci.yml`, which runs `rite check` and `rite build` on
   `ubuntu-24.04` and `ubuntu-24.04-arm` and uploads each `bin/tracelet`.
2. `image` wraps each architecture's binary in the image, smoke-tests it,
   checks that `bin/tracelet version` equals the tag, and pushes
   `ghcr.io/abogoyavlensky/tracelet:X.Y.Z-amd64` and `-arm64`.
3. `cli` cross-compiles the CLI-only build (`rite build-cli`, no cgo, no
   DuckDB) for linux and darwin on amd64 and arm64.
4. `publish` joins the two images into one multi-arch image tagged `X.Y.Z`,
   `X.Y` and `latest`, then creates the GitHub release with:
   - `tracelet-{linux,darwin}-{amd64,arm64}.tar.gz`: the CLI, static;
   - `tracelet-server-linux-{amd64,arm64}.tar.gz`: the server and CLI in one
     binary, glibc 2.39 or newer;
   - `checksums.txt`.

Every archive holds one file, `tracelet`, at its root. The release comes last,
so it never exists without its image.

The CLI takes the plain asset names on purpose. mise's GitHub backend prefers
an asset named after the repository followed only by platform tokens, so
`mise use github:abogoyavlensky/tracelet` installs the small CLI, never the
server tarball.

```sh
git tag v0.1.0
git push origin v0.1.0
```

Running the workflow by hand (Actions tab) is a dry run: the tests, both
images and all CLI builds, nothing pushed, no release. A new ghcr package may
start out private; pull it once while logged out of ghcr
(`docker logout ghcr.io; docker pull ghcr.io/abogoyavlensky/tracelet:X.Y.Z`),
and if that is denied, set the package's visibility to public in its
settings.

Releases and the master deploy are independent of each other.

## Deployment

Every push to `master` deploys to `https://$APP_DOMAIN` on the `personal`
[uncloud](https://uncloud.run/docs) cluster. `.github/workflows/deploy.yml`
runs the tests (`ci.yml`), downloads the amd64 `bin/tracelet` that CI built,
runs `uc deploy` with `compose.yaml`, and then waits until `/api/v1/health`
over HTTPS reports the version of the binary it just shipped. A version match
proves the new container is the one answering. uncloud builds the image on the
runner and ships it over SSH; its Caddy terminates TLS.

The repository needs these before the first deploy. A missing one fails the
deploy loudly:

| Kind | Name | Value |
|---|---|---|
| variable | `SERVER_IP` | the `personal` server's address |
| variable | `APP_DOMAIN` | e.g. `tracelet.absky.dev` (a deploy fails if unset) |
| secret | `SSH_PRIVATE_KEY` | a key authorized for `root@SERVER_IP` |
| secret | `ADMIN_TOKEN` | `tl_` plus at least 16 characters, e.g. `echo "tl_$(openssl rand -hex 20)"` |

plus a DNS A record for `APP_DOMAIN` pointing at the server. The first deploy
also waits for Caddy to get the certificate.

### First run

Point the CLI at the instance with the admin token, then create a project and
an ingest token:

```sh
export TRACELET_URL=https://$APP_DOMAIN
export TRACELET_TOKEN=tl_...              # the ADMIN_TOKEN secret
tracelet projects create shop --name "Shop"
tracelet tokens create --project shop --scope ingest --name "api server"
```

### Admin token

The server stores the hash of `TRACELET_ADMIN_TOKEN` only when no admin token
is active. Changing the `ADMIN_TOKEN` secret later does not rotate it. To
rotate: create a new admin token with the CLI, revoke the old one, and then
set the secret to the new value.

### Data and updates

The data lives on the server in `/root/tracelet-data`, a bind mount that
survives deploys. It is not backed up yet
([backup backlog](backlog/backup-and-restore-command.md)).

DuckDB lets one process open its file, so updates are stop-first: the old
container gets SIGTERM, drains the ingest queue and closes the databases
(`stop_grace_period` is 30 s), then the new one starts. A deploy is a few
seconds of refused ingestion; OpenTelemetry SDKs retry.

The container is limited to 256 MB, with DuckDB capped at 128 MB
(`TRACELET_DUCKDB_MEMORY_LIMIT`). That is sized for a few personal services,
not the design's `busy` workload. A flush that runs out of memory fails, is
logged, shows on health as `last_flush_error`, and retries on the next tick.
If the instance is OOM-killed or flushes keep failing, raise the limit in
`compose.yaml`.

Logs and state, from a machine with the SSH key:

```sh
uc --context personal --connect root@<SERVER_IP> logs tracelet
uc --context personal --connect root@<SERVER_IP> ls
```

### First deploy

12 October 2026, run 38217122035 (the merge of PR #5): the `deploy` job took
3m38s including the tests, `uc deploy` created service `tracelet`, and the
version check passed with `cee187f`. The instance reported `ready: true`, one
completed flush and no `last_flush_error`, with no telemetry ingested yet, so
the flush ran on empty hours. Peak container memory and flush behaviour under
real load are not measured yet: nothing sends data, and the deploy machine had
no uncloud access. Check `memory.peak` (`uc exec tracelet -- cat
/sys/fs/cgroup/memory.peak`) and `last_flush_error` after the first services
send logs, and raise the limits in `compose.yaml` if they show pressure.
