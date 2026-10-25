# Replace dashboard polling with server-sent events

## Context

Every open dashboard tab polls `GET /api/metrics` every 5 seconds. That is about 40% of all API requests, and most responses are unchanged.

## Approach

- New endpoint `GET /api/metrics/stream` (`text/event-stream`): one event per metric change, a heartbeat comment every 15 s
- `useMetrics()` subscribes to a single shared `EventSource` per tab and fans out to widgets; it falls back to polling after three stream errors
- nginx: `proxy_buffering off` and `proxy_read_timeout 1h` on the stream location

## Steps

1. Server: change feed from the metrics cache, plus the stream handler
2. Client: a `MetricsStream` provider replacing per-widget polling
3. nginx config for the stream route
4. Feature flag `dashboard.sse`, internal users first

## Verification

- Vitest for the provider with a mocked `EventSource`
- Playwright: change a metric and check the widget updates within a second
- Compare the request rate on `/api/metrics` before and after rollout
