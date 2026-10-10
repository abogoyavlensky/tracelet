# OTLP/HTTP requests in JSON encoding are refused with 415

**Status: open**

## Problem

`POST /v1/logs` accepts only `application/x-protobuf`
(`internal/httpapi/otlp.go`, the `mediaType` switch). A request with
`Content-Type: application/json` gets 415 with a message naming protobuf.
The Go, Java, Python, and .NET SDKs default to protobuf, so server-side apps
work. Browser and React Native exporters default to JSON, so the browser and
mobile slice cannot ship without this.

## Proposed fix

OTLP/JSON is not plain `protojson`: trace and span IDs are hex strings, not
base64, and enum values may be names or numbers. Decode with
`protojson.Unmarshal` into `logspb.LogsData` after rewriting
`traceId`/`spanId` fields from hex to base64, or walk the JSON by hand into
the same types. Keep `otlp.ConvertLogs` unchanged: it is encoding-agnostic.
The response must then be JSON too (`{"partialSuccess": {...}}`), so
`otlp.PartialSuccess` needs a JSON sibling. Test against payloads captured
from the JS SDK, including 64-bit integers sent as strings.

## Origin

Deferred by design decision 7 of `docs/plans/2026-10-10-1027-logs-slice.md` (10 October 2026), tagged for the
browser and mobile slice.
