# The runtime image only: bin/tracelet is built outside it by `rite build`.
# CI builds and tests that binary natively on each architecture and this file
# wraps it, so the image runs the binary that passed `rite check`. Building
# inside Docker would also raise the binary's glibc floor to the base image's.
#
# The DuckDB driver is cgo, so the binary links glibc and libstdc++
# dynamically: a glibc base, not Alpine. Trixie, not bookworm: a binary built
# on ubuntu-24.04 (the CI runner) needs glibc 2.39, and bookworm ships 2.36.
# Trixie ships 2.41.
#
# A binary built on a newer glibc than trixie's does not start in this image.
# A dev box with glibc 2.42 or newer fails with "GLIBC_2.4x not found". CI is
# the authority for `rite docker` and the smoke test.
#
# The same file makes the published image: .github/workflows/release.yml
# builds it around each architecture's CI binary and pushes it to ghcr.io.
FROM debian:trixie-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

LABEL org.opencontainers.image.source="https://github.com/abogoyavlensky/tracelet" \
      org.opencontainers.image.description="Self-hosted logs and traces for small apps, in a single binary"

WORKDIR /app
# The databases and telemetry files live in /app/data; mount a volume there to
# keep them.
RUN mkdir -p /app/data
ENV TRACELET_DATA_DIR=/app/data
COPY bin/tracelet /app/tracelet

EXPOSE 8080
ENTRYPOINT ["/app/tracelet"]
CMD ["serve"]
