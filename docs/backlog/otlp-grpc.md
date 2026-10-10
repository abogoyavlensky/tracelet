# OTLP over gRPC is not accepted

**Status: open**

## Problem

Only OTLP/HTTP is served (`POST /v1/logs` in `internal/httpapi/handler.go`).
An SDK or collector configured with `OTEL_EXPORTER_OTLP_PROTOCOL=grpc` (the
default in some languages' older SDKs and in many collector configs) cannot
send to Tracelet. Users have to set `http/protobuf`, which every current SDK
supports.

## Why it is left alone

gRPC needs `google.golang.org/grpc` and a second listener (port 4317 by
convention), a large dependency for a project that keeps its tree small.
`internal/otlp` deliberately decodes into `logspb.LogsData` so the
gRPC-importing `collector/logs/v1` package stays out of the build; adding
gRPC would reverse that. Revisit when a real user's SDK or environment cannot
use OTLP/HTTP.

## Origin

Out of scope in `docs/plans/2026-10-10-1027-logs-slice.md` (10 October 2026); `docs/DESIGN.md` already defers
gRPC "until a real SDK needs it".
