# Move session storage from Redis to Postgres

## Context

Redis only backs web sessions and is being retired next quarter. There are about 2M live sessions with a 14-day TTL, and one is read on every authenticated request.

## Plan

1. Migration `0042_sessions.sql`: `sessions(id bytea primary key, user_id bigint, data jsonb, expires_at timestamptz)`, indexed on `expires_at`
2. `session/postgres` implementing `session.Store`: reads by primary key, writes with `ON CONFLICT DO UPDATE`
3. Dual-write behind `SESSIONS_DUAL_WRITE`: write both stores, read Redis, fall back to Postgres
4. After 14 days every live session exists in Postgres: flip reads, then stop writing to Redis
5. Hourly job deleting expired rows in batches of 10k

## Risks

- Read latency: one indexed lookup per request, 0.4 ms p99 on staging
- Bloat from the hourly deletes: tune autovacuum on the table

## Verification

- The `session.Store` conformance suite runs against both implementations
- 48-hour dual-write soak on staging, comparing hit rates between the stores
