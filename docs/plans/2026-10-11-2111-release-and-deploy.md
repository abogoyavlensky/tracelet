# Release and Deploy Implementation Plan

> **For agentic workers:** Use executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A pushed tag `vX.Y.Z` publishes a multi-arch Docker image to `ghcr.io/abogoyavlensky/tracelet` and a GitHub release with a small static CLI for Linux and macOS (amd64, arm64) and the combined server binary for Linux (amd64, arm64). Every push to `master` deploys Tracelet to the `personal` uncloud cluster.

**Tech Stack:** GitHub Actions, Docker (`debian:trixie-slim`), uncloud `uc` 0.20.0 (new tool in `.mise.toml`), `webfactory/ssh-agent`, `docker/setup-buildx-action`, `crazy-max/ghaction-github-runtime`. No Go or npm dependencies change. The CLI-only build is a Go build tag on `cmd/tracelet`. Modelled on `../pagelet` (`.github/workflows/{test,release,deploy}.yml`, `Dockerfile`, `compose.yaml`, `scripts/docker-smoke.sh`, `docs/operations.md`).

---

## Design

### What this delivers

```sh
git tag v0.1.0 && git push origin v0.1.0
# → ghcr.io/abogoyavlensky/tracelet:0.1.0, :0.1, :latest (linux/amd64 + linux/arm64)
# → GitHub release v0.1.0:
#     tracelet-{linux,darwin}-{amd64,arm64}.tar.gz    the CLI, static, no DuckDB
#     tracelet-server-linux-{amd64,arm64}.tar.gz      server + CLI, glibc 2.39 or newer
#     checksums.txt
mise use github:abogoyavlensky/tracelet             # installs the CLI on Linux and macOS

git push origin master
# → tests on both architectures, then `uc deploy` to https://$APP_DOMAIN,
#   then a health check that the new version is the one answering
```

This is the packaging half of the "packaging and dogfood" step promised after the traces slice. It also ships the CLI-only build and closes `docs/backlog/cli-only-build-without-duckdb.md`. Out of scope: Windows CLI builds (cheap to add later, but nobody has asked for them), backup (`docs/backlog/backup-and-restore-command.md`), and measuring trace lookup under concurrent load (`docs/backlog/trace-lookup-under-concurrent-load.md`). Those stay open.

### Key decisions

1. **Build the binary once, in `ci.yml`, and ship that binary.** `ci.yml` already runs `rite check` and `rite build` natively on `ubuntu-24.04` and `ubuntu-24.04-arm`. It gains a `workflow_call` trigger and uploads `bin/tracelet` per architecture as the artifact `tracelet-linux-<arch>`. `release.yml` and `deploy.yml` call it and download its artifacts, so the binary that ships is the one that passed `rite check` on its own architecture. Pagelet rebuilt in its release job because its test job ran on amd64 only; Tracelet's CI already covers both. Artifact downloads lose the executable bit, so every consumer runs `chmod +x` after downloading.

2. **Workflow triggers follow pagelet.** `ci.yml` runs on `pull_request` and `workflow_call`. Its `push: branches: [master]` trigger is dropped, because `deploy.yml` runs on every master push and calls `ci.yml` first, and otherwise master would be tested twice. The concurrency group becomes `ci-${{ github.workflow }}-${{ github.ref }}`; `github.workflow` is the caller's name when called, so a release dry run on master never cancels a deploy's tests, and the other way round. `release.yml` runs on tags `v[0-9]+.[0-9]+.[0-9]+` and on `workflow_dispatch`, which is a dry run (tests, both images built and smoke-tested, nothing pushed). `deploy.yml` runs on `push` to `master`, with `concurrency: deploy-personal`, `cancel-in-progress: false`.

3. **The image wraps the CI binary; it does not build Go inside Docker.** The `Dockerfile` copies `bin/tracelet` into `debian:trixie-slim` with `ca-certificates` and `libstdc++6`. The DuckDB driver is cgo, so the binary links glibc dynamically, which rules out Alpine. Trixie ships glibc 2.41, and the ubuntu-24.04 runners link against 2.39, which trixie satisfies. A binary built in the image would raise the release binary's glibc floor to 2.41 and lock out Ubuntu 24.04 hosts. The cost: **a binary built on this dev box (Ubuntu 26.04, glibc 2.43) does not start in the image.** CI is the authority for the smoke test, and the Dockerfile comment says so. `.dockerignore` admits only `bin/tracelet`.
   - `ENV TRACELET_DATA_DIR=/app/data`, `VOLUME`-free (mount at `/app/data`), `EXPOSE 8080`.
   - `ENTRYPOINT ["/app/tracelet"]`, `CMD ["serve"]`: `docker run … image` serves, and `docker run --rm image version` prints the version.
   - Labels: `org.opencontainers.image.source` and `.description` only. There is no licence label, because the licence is still an open decision in `docs/VISION.md`.

4. **Smoke test: `scripts/docker-smoke.sh`, run by `rite docker` and by CI.** Build `tracelet:local` from `bin/tracelet`, start it with `-p 8080:8080`, curl `localhost:8080/api/v1/health` with `--retry 15 --retry-all-errors --retry-delay 1`, print `docker ps -a` and the container logs, remove the container, and exit with curl's status. This is pagelet's script with the names changed. The health endpoint needs no token. With no `TRACELET_ADMIN_TOKEN`, the server prints a bootstrap token to stderr, which is harmless in a throwaway container. `ci.yml` runs the script on both architectures after `rite build`, so a broken Dockerfile fails the PR. `release.yml` runs it again before pushing, so the image pushed is the image smoke-tested, not a rebuild.

**4a. The CLI-only build: a `cli` build tag on `cmd/tracelet`.** `internal/cli` imports no storage package. `go list -deps ./internal/cli` shows no DuckDB, SQLite, or `internal/web`, and it cross-compiles for darwin with `CGO_ENABLED=0`. Only `cmd/tracelet` pulls in the server, through `internal/app`. The split:
   - Move `serve()` out of `main.go` into `cmd/tracelet/serve.go` (`//go:build !cli`), unchanged.
   - Add `cmd/tracelet/serve_cli.go` (`//go:build cli`), whose `serve(args []string) error` imports nothing from `internal/` and returns: `this tracelet is the CLI-only build; run the server from the Docker image ghcr.io/abogoyavlensky/tracelet or a tracelet-server release`.
   - `main.go` keeps dispatch and `version` for both builds. The help text still lists `serve`, which in the CLI build answers with that error. That is clearer than a usage text that varies by build.
   - A build tag keeps the executable's name `tracelet` in both builds, so every README example works with either. A separate `cmd/tracelet-cli` would split the docs.
   - `rite build-cli` runs `CGO_ENABLED=0 go build -tags cli -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty)" -o bin/cli/tracelet ./cmd/tracelet`. `GOOS` and `GOARCH` come from the environment, so the release job cross-compiles from one runner.
   - It does not depend on `web-build`; the CLI embeds no frontend.
   - `rite check-cli`, added to `check`'s `:depends`, builds the CLI with `CGO_ENABLED=0` and runs `go vet -tags cli ./cmd/tracelet`, because golangci-lint does not see the tagged file. It then fails if `go list -deps -tags cli ./cmd/tracelet` lists anything matching `duckdb|sqlite|tracelet/internal/(app|web|duckdb|sqlite|httpapi)`. CI therefore checks the boundary on every PR, and so does local `rite check`.

**4b. Release asset names decide what mise installs.** mise's GitHub backend (`src/backend/asset_matcher.rs`, `score_preferred_name_match`) adds a bonus to an asset named `<repo>` followed only by platform and version tokens. `tracelet-linux-amd64.tar.gz` gets it; `tracelet-server-linux-amd64.tar.gz` does not, because `server` is not a platform token. So the CLI takes the plain names, `tracelet-<os>-<arch>.tar.gz` for linux and darwin on amd64 and arm64, and `mise use github:abogoyavlensky/tracelet` installs the small CLI everywhere. The combined binary is `tracelet-server-linux-<arch>.tar.gz`. Every archive holds one file, `tracelet`, at its root.

5. **Release mirrors pagelet's `release.yml`.**
   - **`test` job:** `uses: ./.github/workflows/ci.yml`.
   - **`image` job:** a matrix over `amd64`/`ubuntu-24.04` and `arm64`/`ubuntu-24.04-arm`, native because the Dockerfile's `apt-get` needs the target architecture. Steps:
     1. Check out, download `tracelet-linux-<arch>` into `bin/`, and `chmod +x`.
     2. **Check the version:** `bin/tracelet version` must print `$GITHUB_REF_NAME` exactly on a tag push. This catches a shallow checkout where `git describe` missed the tag. The fix in that case is `fetch-depth: 0` on `ci.yml`'s checkout.
     3. Run the smoke script.
     4. Pack `tracelet-server-linux-<arch>.tar.gz` (the binary only, at the archive root) and upload it as artifact `release-server-linux-<arch>`.
     5. On a tag push only: log in to ghcr.io with `GITHUB_TOKEN` and push `$IMAGE:<version>-<arch>`.
   - **`cli` job:** after `test`, on `ubuntu-24.04`, with `contents: read`. It checks out the repository and runs `jdx/mise-action` (with the Go cache as in `ci.yml`), since nothing carries over from `test`. Then, for each of `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, run `GOOS=… GOARCH=… rite build-cli` and pack `bin/cli/tracelet` into `tracelet-<os>-<arch>.tar.gz`. On a tag push, check that the linux/amd64 build's `version` prints `$GITHUB_REF_NAME`, as the image job does. Upload all four as artifact `release-cli`. It runs on dry runs too, so a broken cross-compile shows before a tag.
   - **`publish` job:** needs `image` and `cli`; tag push only. Download `release-*` (merged) into `dist/` and write `checksums.txt` with `sha256sum`. Run `docker buildx imagetools create` to tag `<version>`, `<major.minor>` and `latest` from the two arch images, and `imagetools inspect` to check the result. Last, `gh release create "$GITHUB_REF_NAME" dist/* --generate-notes --verify-tag`, so a release never exists without its image.
   - **Permissions:** top-level `contents: read`. The `image` job adds `packages: write`; the `publish` job has `contents: write` and `packages: write`. Job permissions replace the top-level map, so `contents: read` is restated where needed.
   - GitHub documents new ghcr packages as private by default. Pagelet's came out public on the first push from its public repository, and Tracelet's should too, but the first release checks with an anonymous pull (Task 7). If the pull fails, the owner switches the package to public in its settings.

6. **Deploy mirrors pagelet's `deploy.yml` and `compose.yaml`.**
   - **`deploy.yml`:**
     - `test` calls `ci.yml`.
     - `deploy` runs on `ubuntu-24.04` after it with `contents: read`, in these steps:
       1. Check out the repository.
       2. Load `SSH_PRIVATE_KEY` into `webfactory/ssh-agent`.
       3. Run `jdx/mise-action`, which installs `uc` from `.mise.toml`.
       4. Download `tracelet-linux-amd64` into `bin/` and `chmod +x`.
       5. Set up `docker/setup-buildx-action` and `crazy-max/ghaction-github-runtime`, for the `type=gha` build cache.
       6. Deploy with `ADMIN_TOKEN` and `APP_DOMAIN` in the environment: `uc --context personal --connect root@${{ vars.SERVER_IP }} deploy -f compose.yaml --yes tracelet`.
       7. **Check the deployed version.** Retry for about two minutes until `https://$APP_DOMAIN/api/v1/health` answers and its `.version` (read with `jq`) equals `bin/tracelet version`. A version match proves the new container is the one answering, not just that something answers. The first deploy also waits here for Caddy's certificate.
   - **`compose.yaml`** (`x-context: personal`, service `tracelet`):
     - `build`: `platforms: [linux/amd64]`, with `cache_from`/`cache_to` set to `type=gha` (`mode=max` on the latter).
     - `x-ports`: `- ${APP_DOMAIN:?APP_DOMAIN must be set}:8080/https`. Pagelet needed an `x-caddy` block only for its 64 KB body limit; Tracelet enforces its own 4 MB limit before and after decompression, so the standard port mapping suffices. The `:?` form makes loading fail when the variable is unset, rather than deploying with an empty host.
     - `volumes`: `- /root/tracelet-data:/app/data`. It is a bind mount that survives deploys and is not backed up yet.
     - `deploy.update_config.order: stop-first`. DuckDB locks its file to one process. uncloud 0.20.0 picks start-first for bind mounts unless told otherwise (pagelet's `docs/KNOWLEDGE.md`, "Update order"), and start-first would crash the new container on the lock.
     - `stop_grace_period: 30s`. Shutdown allows the HTTP server 10 s, then drains the ingest queue and closes the writer and databases; 30 s leaves room for that.
     - `mem_limit: 256m`, `mem_reservation: 128m`, with `TRACELET_DUCKDB_MEMORY_LIMIT: 128MB`. The owner asked for 256 MB at most. The design's 1.5 GB budget is for the `busy` workload (100 logs/s, 50 spans/s), where the two hot hours alone fill 208 MB of DuckDB's memory. A few personal services send a small fraction of that, so the hot window and the hourly flush sort shrink with it. The memory follow-up (`docs/spikes/memory.md`) found the rest of the process to be Go at about 20 MB, about 85 MB of fixed residual (mostly the 80 MB binary's mapped code), and jemalloc retention that grows with churn. DuckDB's limit goes to 128 MB so it stays well inside the container's limit. At that limit, a flush that runs out of memory fails, is logged, shows on health as `last_flush_error`, and retries next tick. Without it, the kernel would kill the whole process. `128MB` failed only at `busy`, sorting a full hour of logs; at the dogfood rate it is untested, and the first deploy measures it (Task 7). If the instance is OOM-killed or flushes fail, that is a finding to record and a reason to raise the limit, not to tune further in this plan.
     - `environment`: `TRACELET_ADMIN_TOKEN: secret://admin_token` and `TRACELET_DUCKDB_MEMORY_LIMIT: 128MB`. Everything else keeps the image and server defaults: data dir `/app/data`, addr `:8080`, retention 30 days, disk floor 1 GB.
     - `secrets`: `admin_token: { x-command: printenv ADMIN_TOKEN }`, as pagelet does with `ADMIN_PASSWORD`.
   - **Admin token semantics:** the server stores the hash of `TRACELET_ADMIN_TOKEN` only when no admin token is active (`internal/project/service.go` `Bootstrap`). Changing the secret later does not rotate the token. Rotation means creating a new admin token through the CLI, revoking the old one, and then setting the secret to the new value. The operations doc says so.

7. **Tool versions.** `.mise.toml` gains `uc = "0.20.0"` with `uc = "github:psviderski/uncloud"` under `[tool_alias]`, the version pagelet's deploy runs on the same cluster. This is a new tool dependency and is mentioned as such. CI installs it on every run through `mise-action`, which costs a few seconds. Action majors follow `ci.yml`'s existing pins: `actions/checkout@v7`, `jdx/mise-action@v5`, `actions/cache@v6`. Look up the current majors of the other actions with `gh api repos/<owner>/<repo>/releases/latest --jq .tag_name` before writing them, and use those.

### What the user sets up before the first deploy

The first master push after this merges deploys, and a missing variable or secret fails it loudly. That is deliberate: a silent skip would hide misconfiguration. Set these up **before merging**:

| Kind | Name | Value |
|---|---|---|
| variable | `SERVER_IP` | the `personal` cluster server's address (the same as pagelet's) |
| variable | `APP_DOMAIN` | e.g. `tracelet.absky.dev`; the deploy fails if unset |
| secret | `SSH_PRIVATE_KEY` | a key authorized for `root@SERVER_IP` (pagelet's can be reused) |
| secret | `ADMIN_TOKEN` | `tl_` plus at least 16 characters, e.g. `echo "tl_$(openssl rand -hex 20)"` |

Also add a DNS A record for `APP_DOMAIN` pointing at the server. The release workflow needs nothing beyond the built-in `GITHUB_TOKEN`.

### Testing

The only Go change is the `serve` split. `rite check`, now including `check-cli`, is the regression check for both builds. The new behaviour is checked by:
- the smoke script in CI on both architectures (on the PR);
- `check-cli` in CI and locally, which proves the CLI builds without cgo and links no storage;
- a `release.yml` dry run started by the owner from the Actions tab (the agent's `gh` token cannot dispatch workflows, per pagelet's notes);
- an offline load of `compose.yaml` through uncloud's own compose loader (Task 5);
- the first real deploy after merge and the first tag.

## File Structure

- Create: `Dockerfile`: the runtime image around `bin/tracelet`.
- Create: `.dockerignore`: `*` then `!bin/tracelet`.
- Create: `scripts/docker-smoke.sh` (executable): build, run, health check.
- Modify: `rite.edn`: `docker`, `build-cli`, and `check-cli` tasks; `check-cli` joins `check`.
- Modify: `cmd/tracelet/main.go`: `serve` moves out.
- Create: `cmd/tracelet/serve.go` (`//go:build !cli`) and `cmd/tracelet/serve_cli.go` (`//go:build cli`).
- Modify: `docs/backlog/cli-only-build-without-duckdb.md`: status done.
- Modify: `.github/workflows/ci.yml`: triggers, concurrency group, smoke step, artifact upload.
- Create: `.github/workflows/release.yml`.
- Create: `.github/workflows/deploy.yml`.
- Create: `compose.yaml`.
- Modify: `.mise.toml`: `uc`.
- Create: `docs/operations.md`: maintainer reference for the image, releases, deployment, and the required variables and secrets.
- Modify: `README.md`: install from a release or the image; link to operations.
- Modify: `docs/DESIGN.md`: status line and "Platforms" record what now ships.
- Modify: `AGENTS.md`: the workflows, the image, and the glibc rule.

### Task 1: Image and smoke test

**Files:**
- Create: `Dockerfile`, `.dockerignore`, `scripts/docker-smoke.sh`
- Modify: `rite.edn`

- [x] **Step 1: Write the Dockerfile, `.dockerignore`, and smoke script**
  Follow design decisions 3 and 4. Start the Dockerfile with a comment block in pagelet's style. It should explain why the binary is built outside the image, why the base is glibc and not Alpine, why trixie, and that a dev-box binary will not start in it. `chmod +x scripts/docker-smoke.sh`. The container name is `tracelet-smoke`.

- [x] **Step 2: Add the `rite docker` task**
  `docker {:doc "Build the image around bin/tracelet and check its health (needs a binary built on glibc 2.41 or older, as CI builds)" :depends [build] :do {:sh "scripts/docker-smoke.sh"}}`, placed after `build`.

- [x] **Step 3: Check what can be checked locally**
  Run: `rite build && docker build -t tracelet:local . && docker run --rm tracelet:local version`
  Expected: the image builds. On this dev box (glibc 2.43) `version` fails with `GLIBC_2.4x not found`. That failure confirms the documented limit and is not a bug. If Docker is unavailable, say so and rely on CI.
  Then run: `rite check`. Expected: PASS (no Go or web change).

- [x] **Step 4: Commit**
  `git commit -m "Add the Docker image and its smoke test"`

### Task 2: The CLI-only build

**Files:**
- Create: `cmd/tracelet/serve.go`, `cmd/tracelet/serve_cli.go`
- Modify: `cmd/tracelet/main.go`, `rite.edn`

- [x] **Step 1: Split `serve` behind the build tag**
  Follow design decision 4a. `main.go` must no longer import `internal/app`; only `serve.go` does.

- [x] **Step 2: Add `build-cli` and `check-cli` to `rite.edn`**
  As in decision 4a. Write the dependency check as a shell one-liner in the task, but make a failing `go list` fail the task too: an inverted pipe alone passes when `go list` errors. For example, `deps=$(go list -deps -tags cli ./cmd/tracelet) && ! printf '%s\n' "$deps" | grep -E '<pattern>'`. Add `check-cli` to `check`'s `:depends`.

- [x] **Step 3: Verify**
  Run: `rite check-cli && GOOS=darwin GOARCH=arm64 rite build-cli && file bin/cli/tracelet && rite build-cli && ls -lh bin/cli/tracelet && bin/cli/tracelet version; bin/cli/tracelet serve; echo "exit=$?"`
  Expected: the darwin build is a Mach-O arm64 executable. The linux build is statically linked and well under 20 MB. `version` prints the describe string, and `serve` prints the CLI-only error with `exit=1`.
  Then make the check fail on purpose. Temporarily add `_ "github.com/abogoyavlensky/tracelet/internal/app"` to `serve_cli.go`; `rite check-cli` must fail. Revert.
  Then run: `rite check`. Expected: PASS.

- [x] **Step 4: Commit**
  `git commit -m "Build the CLI without DuckDB behind a cli build tag"`

### Task 3: CI builds the shipped binary and smoke-tests the image

**Files:**
- Modify: `.github/workflows/ci.yml`

- [x] **Step 1: Change triggers and concurrency**
  `on: pull_request:` and `workflow_call:`, with no `push`. Add a comment in pagelet's style: master pushes are tested through `deploy.yml`, tags through `release.yml`. Set the concurrency group to `ci-${{ github.workflow }}-${{ github.ref }}`, with a comment saying why.

- [x] **Step 2: Add the smoke and artifact steps**
  After "Smoke test the binary", add a "Smoke-test the image" step that runs `scripts/docker-smoke.sh` directly (`rite build` already ran). Then upload `bin/tracelet` with `actions/upload-artifact` as `tracelet-linux-${{ matrix.arch }}`, with `if-no-files-found: error` and `retention-days: 7`.

- [x] **Step 3: Lint the workflow**
  Run: `docker run --rm -v "$PWD:/repo" -w /repo rhysd/actionlint:latest -color` (or `actionlint` if installed).
  Expected: no errors. If neither is available, say so and rely on the PR run.

- [x] **Step 4: Commit**
  `git commit -m "Build the shipped binary in CI and smoke-test its image"`

### Task 4: Release workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [x] **Step 1: Write it**
  Follow design decision 5. Set `env: IMAGE: ghcr.io/abogoyavlensky/tracelet`. Use `concurrency: release-${{ github.ref }}` with `cancel-in-progress: false`. The version check step, run on a tag push only:
  ```sh
  test "$(bin/tracelet version)" = "$GITHUB_REF_NAME"
  ```
  The `publish` job's version and minor: `version="${GITHUB_REF_NAME#v}"; minor="${version%.*}"`. Comment the non-obvious steps as pagelet's file does.

- [x] **Step 2: Lint**
  Run actionlint as in Task 3. Expected: no errors.

- [x] **Step 3: Commit**
  `git commit -m "Publish the image and Linux binaries on a version tag"`

### Task 5: Deploy to uncloud

**Files:**
- Create: `compose.yaml`, `.github/workflows/deploy.yml`
- Modify: `.mise.toml`

- [x] **Step 1: Add `uc` to `.mise.toml`**
  Under `[tools]`, add `uc = "0.20.0"` with a comment saying it deploys `compose.yaml` to the uncloud cluster (CI; see `deploy.yml`). Add the alias under `[tool_alias]`. Run `mise install` and `uc --version`. Expected: `0.20.0`.

- [x] **Step 2: Write `compose.yaml`**
  Follow design decision 6. Add a header comment that says how it deploys, plus a comment on each non-default setting (stop-first, grace period, memory, secret).

- [x] **Step 3: Check `compose.yaml` offline with uncloud's loader**
  `uc deploy` has no dry run. Clone uncloud v0.20.0 into `.tmp/uncloud` (`git clone --depth 1 --branch v0.20.0 https://github.com/psviderski/uncloud .tmp/uncloud`). Add a scratch test under `pkg/client/compose/` that calls `LoadProject` on `../../../../compose.yaml` (or an absolute path), then `ServiceSpecFromCompose` and `spec.Validate()`, and prints the spec as JSON. Run it with `APP_DOMAIN=tracelet.example.com ADMIN_TOKEN=tl_0123456789abcdef0123` (`go test ./pkg/client/compose/ -run <Name> -v`).
  Expected: `UpdateConfig.Order: stop-first`, a bind volume `/root/tracelet-data` at `/app/data`, memory 256 MiB and reservation 128 MiB, `TRACELET_DUCKDB_MEMORY_LIMIT=128MB`, stop grace 30 s, the port published as HTTPS on `tracelet.example.com` to container port 8080, and `TRACELET_ADMIN_TOKEN` from the secret. Then run it with `APP_DOMAIN` unset. Expected: loading fails with "required variable APP_DOMAIN is missing a value". Adapt the function names to v0.20.0's actual API if they differ. If the loader cannot be driven this way within a reasonable effort, record that and rely on the first deploy. Delete `.tmp/uncloud` afterwards; `.tmp/` is gitignored either way.

- [x] **Step 4: Write `deploy.yml`**
  Follow design decision 6. The version check step:
  ```sh
  want="$(bin/tracelet version)"
  for i in $(seq 60); do
    got="$(curl -fsS --connect-timeout 5 --max-time 10 "https://$APP_DOMAIN/api/v1/health" | jq -r .version)" && [ "$got" = "$want" ] && exit 0
    sleep 2
  done
  echo "health at https://$APP_DOMAIN never reported $want (last: ${got:-no answer})"; exit 1
  ```
  `APP_DOMAIN` comes from `vars.APP_DOMAIN` in the step's `env`.

- [x] **Step 5: Lint**
  Run actionlint as in Task 3. Expected: no errors.

- [x] **Step 6: Commit**
  `git commit -m "Deploy master to the personal uncloud cluster"`

> Deviation: uc 0.20.0 has no `--version` flag; `uc version` prints 0.20.0.
> Deviation: the offline loader check needed `GOTMPDIR` off the quota-limited `/tmp` tmpfs. Results matched every expectation; unset `APP_DOMAIN` fails with "required variable APP_DOMAIN is missing a value".
> Deviation: the version-check loop uses `_` instead of `i` (actionlint's shellcheck flags the unused variable). The deploy step gained an `ADMIN_TOKEN` guard (`: "${ADMIN_TOKEN:?…}"`), because `printenv` exits 0 on an empty secret and the server would deploy with a token of its own making.

### Task 6: Documentation

**Files:**
- Create: `docs/operations.md`
- Modify: `README.md`, `docs/DESIGN.md`, `AGENTS.md`

- [x] **Step 1: `docs/operations.md`**
  This is the maintainer reference, in pagelet's shape, with these sections:
  - **Docker:** what the image is, `/app/data`, the glibc rule, `rite docker`.
  - **Releases:** the tag flow, what gets published, and the dry run from the Actions tab.
  - **Deployment:** what happens on a master push, then the variables-and-secrets table and DNS record from the design. Also cover the first-run steps: point the CLI at `https://$APP_DOMAIN` with `TRACELET_URL` and the admin token, then create a project and an ingest token. Then the admin token rotation note, data at `/root/tracelet-data` (not backed up; link the backlog entry), stop-first meaning a few seconds of refused ingestion per deploy (SDKs retry), and the `uc logs tracelet` and `uc ls` commands.

- [x] **Step 2: README**
  Add an "Install" section before the Quickstart. The CLI: `mise use github:abogoyavlensky/tracelet`, or the `tracelet-<os>-<arch>.tar.gz` tarball, on Linux or macOS. The server: the Docker image, or `tracelet-server-linux-<arch>.tar.gz` (glibc 2.39 or newer), which is also a full CLI. Then `docker run -p 8080:8080 -v tracelet-data:/app/data -e TRACELET_ADMIN_TOKEN=… ghcr.io/abogoyavlensky/tracelet`. Point `TRACELET_URL` at the server when using the CLI elsewhere. Link `docs/operations.md` from "Development".

- [x] **Step 3: DESIGN and AGENTS**
  In DESIGN's status line, say that packaging shipped: a Docker image and Linux release binaries on tags, plus a master deploy. Update "Platforms" to what ships: the combined executable for Linux amd64 and arm64, the Docker image, and the CLI-only build for Linux and macOS (`-tags cli`, no cgo). Windows is added on demand. This amends a **decided** item, at the owner's request, and the text says so. Mark `docs/backlog/cli-only-build-without-duckdb.md` `**Status: done**` with a line naming this plan. In AGENTS.md, add a short "Release and deploy" note: `cmd/tracelet` has a `cli` build tag, and the CLI build must not link storage (`rite check-cli`); `ci.yml` builds the binary that ships; `release.yml` on tags and `deploy.yml` on master call it; never ship a binary built on a newer glibc than trixie's 2.41; see `docs/operations.md`.

- [x] **Step 4: Check and commit**
  Run: `rite check`. Expected: PASS.
  `git commit -m "Document releases and the master deploy"`

### Task 7: PR, first deploy, first release

- [x] **Step 1: Open the PR**
  Push the branch and open a PR to `master`, then link it with `link_pull_request`. The PR body lists the variables, secrets and DNS record from the design, under a "Before merging" heading.
  Expected: `ci` passes on both architectures, including the image smoke test.

- [x] **Step 2: Hand over to the owner**
  Stop and ask the owner to:
  1. set the variables and secrets and the DNS record;
  2. optionally start a `release` dry run from the Actions tab;
  3. merge.
  Do not merge without the owner.

- [x] **Step 3: Watch the first deploy**
  After the merge, watch with `gh run watch` on the `deploy` run. Expected: `uc deploy` creates service `tracelet`, and the version check passes. After the first hourly flush, check memory and flushing: `uc --context personal --connect root@<SERVER_IP> exec tracelet -- cat /sys/fs/cgroup/memory.peak`, or `docker stats --no-stream` on the server, plus `last_flush`/`last_flush_error` from the health endpoint. Record the run ID, the deploy job's duration, the peak memory, the ingest rate at the time, and anything surprising in `docs/operations.md` under a short "First deploy" note, in a follow-up commit on master or a small PR, as the owner prefers.

- [ ] **Step 4: First release, only if the owner asks for it**
  `git tag v0.1.0 && git push origin v0.1.0`, then watch `release`. Expected: ghcr has `0.1.0`, `0.1` and `latest` as an index of `linux/amd64` and `linux/arm64`. The release has six tarballs (four CLI, two server) and `checksums.txt`, and the downloaded server amd64 binary's `version` prints `v0.1.0`. Check the package is public by pulling it anonymously, logged out of ghcr: `docker logout ghcr.io; docker pull ghcr.io/abogoyavlensky/tracelet:0.1.0`. If the pull is denied, ask the owner to set the package's visibility to public under its package settings. Check that mise installs the release on Linux: `MISE_DATA_DIR=$(mktemp -d) mise exec github:abogoyavlensky/tracelet@0.1.0 -- tracelet version` prints `v0.1.0`, and `tracelet serve` under the same `mise exec` prints the CLI-only error. That proves mise picked the CLI, not the server tarball.

> Status: Tasks 1-7 done except Step 4 (the first release), which waits for the owner to ask. PR #5 merged and the first deploy succeeded (run 38217122035, 3m38s); `https://tracelet.absky.dev/api/v1/health` reports `cee187f`, and the admin token authenticates against the live API. Peak memory is unmeasured: the agent has no uncloud access. See "First deploy" in `docs/operations.md`.

## Summary

Implemented: the Docker image and smoke test (`Dockerfile`, `scripts/docker-smoke.sh`, `rite docker`); the CLI-only build behind a `cli` build tag (`rite build-cli`, `rite check-cli` in `rite check`); `ci.yml` as a reusable workflow that builds and smoke-tests the shipped binary per architecture; `release.yml` (tags, with a dry run); `compose.yaml` and `deploy.yml` (every master push, with a version check); documentation in `docs/operations.md`, the README, DESIGN and AGENTS.

Issues: the offline compose check hit the `/tmp` quota (worked around with `GOTMPDIR`); a stale codex report file nearly passed for a review (fixed by deleting it before each run).

Deviations: `uc version` instead of `uc --version`; the version-check loop uses `_`; an `ADMIN_TOKEN` guard in the deploy step (`printenv` passes an empty secret); the plan file is committed on the branch.

Not done: the first release (`v0.1.0`, Step 4) and the peak-memory measurement.

Plan could have specified better: the actionlint-clean loop, the `ADMIN_TOKEN` guard (an empty secret does not fail "loudly"), and who measures memory when the agent has no server access.
