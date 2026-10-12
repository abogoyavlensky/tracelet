# Records sent with the deprecated deployment.environment attribute file under "default"

**Status: open**

## Problem

The server reads a record's environment from the resource attribute
`deployment.environment.name` (`internal/otlp/resource.go`) and files it
under `default` when that is missing. Semantic conventions before v1.27
named the attribute `deployment.environment`, and SDK code built against
older `semconv` packages (`semconv.DeploymentEnvironment` in Go) still sends
that key. Such an app's records all land in `default`, and environment
filters in the UI, API, and CLI miss them, with nothing saying why. This
showed up while seeding the explore UI's test data with the old key.

## Proposed fix

Fall back to `deployment.environment` when `deployment.environment.name` is
absent, and prefer the new key when both are present. Cover both in the
conversion tests and mention the fallback in the README's list of resource
attributes.

## Origin

Found while checking `docs/plans/2026-10-12-1216-explore-ui.md` against
seeded data (12 October 2026).
