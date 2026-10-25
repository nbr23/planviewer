# Add per-tenant rate limiting to the ingest API

## Context

A few tenants send bursts large enough to saturate the ingest workers, and every other tenant sees `503`s while the queue drains. Limiting each tenant at the edge of `POST /v1/events` keeps one noisy tenant from degrading the rest. The batch endpoint is out of scope.

## Approach

A token bucket per tenant, held in memory in each ingest pod. The limiter runs after `authMiddleware`, which already puts the tenant in the request context.

| Tier       | Rate (req/s) | Burst |
|------------|-------------:|------:|
| free       |           20 |    40 |
| team       |          200 |   400 |
| enterprise |        1,000 | 2,000 |

Limits live in `config/tiers.yaml`, so ops can change them without a deploy. Rejected requests get `429 Too Many Requests` with a `Retry-After` header, so the SDK clients back off and retry instead of hammering the endpoint.

```go
type Limiter struct {
	mu      sync.Mutex
	buckets *lru.Cache[tenant.ID, *bucket]
	tiers   map[tenant.Tier]Limit
}

func (l *Limiter) Allow(id tenant.ID, tier tenant.Tier) (ok bool, wait time.Duration)
```

## Steps

1. New package `internal/ratelimit`: `bucket`, `Limiter`, an LRU capped at 50k tenants
2. Load `config/tiers.yaml` at startup and on `SIGHUP`
3. Middleware in `internal/ingest/router.go`, mounted after `authMiddleware`
4. Respond `429` with `Retry-After` and `X-RateLimit-Remaining`
5. Metric `tidepool_ratelimit_rejected_total{tier}` and a panel on the ingest dashboard

## Out of scope

- Global limits across pods; per-pod buckets are enough while ingest runs 3 replicas
- `POST /v1/batch`

## Verification

- Unit tests for refill math and burst behaviour, driven by a fake clock
- `go test -race ./...`
- Load test with `vegeta` at 5x the free tier against staging: the noisy tenant gets `429`s while p99 for everyone else stays under 150 ms
