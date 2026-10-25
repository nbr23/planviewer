package main

import "time"

type eventKind int

const (
	promptEvent eventKind = iota
	replyEvent
	planEvent
	draftEvent
	approveEvent
	rejectEvent
	workEvent
)

type event struct {
	ago  time.Duration
	kind eventKind
	text string
	file string
}

type session struct {
	id, project, branch, title string
	events                     []event
}

const (
	minute = time.Minute
	hour   = time.Hour
	day    = 24 * time.Hour
)

func prompt(ago time.Duration, text string) event {
	return event{ago: ago, kind: promptEvent, text: text}
}

func reply(ago time.Duration, text string) event {
	return event{ago: ago, kind: replyEvent, text: text}
}

func plan(ago time.Duration, file, key string) event {
	return event{ago: ago, kind: planEvent, file: file, text: key}
}

func draft(ago time.Duration, file, key string) event {
	return event{ago: ago, kind: draftEvent, file: file, text: key}
}

func approve(ago time.Duration) event {
	return event{ago: ago, kind: approveEvent}
}

func reject(ago time.Duration, feedback string) event {
	return event{ago: ago, kind: rejectEvent, text: feedback}
}

func work(ago time.Duration, command string) event {
	return event{ago: ago, kind: workEvent, text: command}
}

var claudeSessions = []session{
	{
		id: "7c1e9a52-3b4f-4d8e-9a61-2f0c8b5d4e17", project: "tidepool", branch: "feat/tenant-limits", title: "Per-tenant rate limiting for ingest",
		events: []event{
			prompt(52*minute, "Ingest is getting hammered by a couple of tenants and everyone else sees 503s. Plan per-tenant rate limiting for `POST /v1/events`: a token bucket per tenant, limits configurable per plan tier. Leave the batch endpoint alone for now."),
			reply(50*minute, "I read through `internal/ingest` and `internal/tenant`. The tenant ID is already in the request context after `authMiddleware`, and tier lookups are cached in `tenant.Store`, so the limiter can sit right after auth without extra queries. Writing up a plan."),
			plan(47*minute, "quiet-amber-heron.md", "tidepool-rate-limit.md"),
			approve(44 * minute),
			work(43*minute, "go test ./internal/ratelimit/..."),
			work(36*minute, "go test -race ./..."),
			reply(31*minute, "Steps 1 to 4 are in:\n\n- `internal/ratelimit` has the token bucket and an LRU of buckets keyed by tenant, capped at 50k entries\n- the middleware answers `429` with `Retry-After` and `X-RateLimit-Remaining`\n- limits come from `config/tiers.yaml`; `free` is 20 req/s with a burst of 40\n\n`go test -race ./...` passes. Metrics (step 5) are next."),
			prompt(24*minute, "Good. Use the existing `metrics.Registry` instead of a new one, and add the tier as a label."),
			work(23*minute, "go test ./internal/ingest/..."),
			reply(20*minute, "Added `tidepool_ratelimit_rejected_total{tier}` to the shared registry and a panel to `dashboards/ingest.json`. Ready for review."),
		},
	},
	{
		id: "4f2a7d93-c81b-4e06-b5d2-93e1f7a08c64", project: "lumen", branch: "sse-updates", title: "Live dashboard updates",
		events: []event{
			prompt(14*minute, "The dashboard polls `/api/metrics` every 5s per open tab and it's a big chunk of our API traffic. Plan switching to server-sent events."),
			reply(13*minute, "Polling lives in `useMetrics()`, which six widgets call independently. The API sits behind nginx, which needs buffering turned off for SSE. Plan coming up."),
			plan(9*minute, "gentle-slate-finch.md", "lumen-sse.md"),
		},
	},
	{
		id: "b3d84f10-6a2e-4c95-8f37-1e9d0a6c52b8", project: "tidepool", branch: "main", title: "Move sessions to Postgres",
		events: []event{
			prompt(3*hour+20*minute, "We're dropping Redis next quarter. Plan moving the web session store to Postgres. About 2M live sessions, 14-day TTL."),
			reply(3*hour+18*minute, "The session store is behind `session.Store` with two implementations, `redis` and `memory`, so this is mostly a new implementation plus a migration window. Drafting the plan."),
			plan(3*hour+12*minute, "brisk-cobalt-otter.md", "tidepool-sessions-postgres.md"),
			approve(3*hour + 5*minute),
			reply(3*hour+4*minute, "Starting with the migration and the `postgres` store; the dual-write flag comes after that."),
		},
	},
	{
		id: "e91b0c47-2d5a-4f83-a6c9-58b7e3d1f290", project: "infra", branch: "main", title: "Per-environment Terraform state",
		events: []event{
			prompt(2*day+hour, "`terraform plan` takes 6 minutes because everything is in one state. Plan splitting it per environment."),
			reply(2*day+58*minute, "There are 412 resources in a single state, with staging and prod selected by `var.env`. Splitting means three root modules and a `terraform state mv` per resource. Plan below."),
			plan(2*day+50*minute, "rustic-pine-badger.md", "infra-split-state.md"),
			reject(2*day+45*minute, "No, too much churn right now. Keep one codebase and use a workspace per environment instead."),
			reply(2*day+44*minute, "Got it. Workspaces keep the module layout unchanged and still give each environment its own state, so a plan only refreshes one environment. I'll rework the plan around `terraform workspace` and a one-off state migration script."),
		},
	},
	{
		id: "2a6c8e14-9f73-4b0d-8e25-c4a1d7b39f06", project: "lumen", branch: "dark-mode", title: "Dark mode for settings",
		events: []event{
			prompt(5*day+2*hour, "Plan dark mode for the settings pages. We already have design tokens in `theme.css`."),
			reply(5*day+hour+58*minute, "The settings pages hard-code colors in 23 places outside the tokens. I'll draft a plan that moves those onto tokens first."),
			draft(5*day+hour+50*minute, "hollow-coral-lynx.md", "lumen-dark-mode.md"),
			prompt(5*day+hour+46*minute, "Hold off, design is redoing the palette. Let's pick this up next sprint."),
			reply(5*day+hour+45*minute, "Sure. The draft is saved; the token migration in step 1 doesn't depend on the palette, so it can go first whenever you're ready."),
		},
	},
	{
		id: "91d3f5a8-0e6b-4c27-b4f9-7a2c6e8d1b53", project: "tidepool", branch: "main", title: "Investigate p99 latency spike after deploy",
		events: []event{
			prompt(day+40*minute, "p99 on `POST /v1/events` went from 80ms to 400ms after yesterday's deploy. Can you look at what changed?"),
			reply(day+36*minute, "Between `v1.18.2` and `v1.18.3` the only change on the ingest path is #482, which switched to `json.NewDecoder` with `DisallowUnknownFields`. That alone shouldn't cost 300 ms.\n\nThe other change is in `deploy/ingest.yaml`: the CPU limit went from `2` to `500m`. The service builds with Go 1.23, so `GOMAXPROCS` is the node's 16 cores, and the pod spends most of each CFS period throttled. That matches the spike showing up only at p99."),
			prompt(day+30*minute, "That limit was a typo. What should GOMAXPROCS be?"),
			reply(day+28*minute, "Revert the limit to `2` and derive `GOMAXPROCS` from it so they can't drift apart again:\n\n```yaml\nenv:\n  - name: GOMAXPROCS\n    valueFrom:\n      resourceFieldRef:\n        resource: limits.cpu\n```\n\n`resourceFieldRef` rounds up to whole cores, so a `2` limit gives `GOMAXPROCS=2`."),
		},
	},
	{
		id: "c4e07b2d-5a19-4f6e-9d83-0b7f2a64c1e9", project: "ferry", branch: "main",
		events: []event{
			prompt(12*day+30*minute, "Why do some uploads give up after 3 retries while others keep going?"),
			reply(12*day+27*minute, "There are two retry paths in `src/upload.rs`:\n\n1. **Per request**: `send_chunk` wraps each PUT in `backoff::retry` with exponential backoff (base 500 ms, factor 2, ±20% jitter) and a `max_elapsed_time` of 2 minutes. The budget is time, not attempts, so fast failures retry many times.\n2. **Per upload**: `Uploader::run` restarts the whole file on `UploadError::Session` and caps that at `MAX_SESSION_RETRIES = 3`.\n\nSo an upload that keeps hitting session expiry gives up after 3 restarts, while flaky network errors keep retrying until the 2-minute budget runs out."),
			prompt(12*day+20*minute, "Can the session restart use the same budget?"),
			reply(12*day+18*minute, "Yes: move the restart inside the same `ExponentialBackoff` and drop the counter.\n\n```rust\nlet policy = ExponentialBackoff {\n    max_elapsed_time: Some(Duration::from_secs(120)),\n    ..Default::default()\n};\nbackoff::future::retry(policy, || async {\n    self.upload_once(&file).await.map_err(classify)\n})\n.await\n```\n\n`classify` maps `UploadError::Session` and network errors to `backoff::Error::transient` and everything else to `permanent`."),
		},
	},
}

var claudeLoosePlans = []event{
	draft(21*day, "misty-ochre-wren.md", "release-checklist.md"),
}

var codexSessions = []session{
	{
		id: "019a1c3e-7b42-7d10-9c5e-4a8f2b61d0e3", project: "ferry", branch: "resumable-uploads", title: "Resumable uploads",
		events: []event{
			prompt(6*hour+20*minute, "Large uploads (over 5 GB) restart from zero when the connection drops. Plan resumable chunked uploads on top of the S3 multipart API."),
			reply(6*hour+17*minute, "Uploads currently stream the whole file through a single `PutObject`. The storage client already wraps `aws-sdk-s3`, which has `CreateMultipartUpload`, `UploadPart` and `ListParts`, so resuming can be driven by the parts the server already has instead of local bookkeeping alone."),
			plan(6*hour+12*minute, "", "ferry-resumable.md"),
			approve(6 * hour),
			work(5*hour+58*minute, "cargo test -p ferry-upload"),
			work(5*hour+40*minute, "cargo test --workspace"),
			reply(5*hour+35*minute, "Implemented:\n\n- `MultipartUpload` with 64 MiB parts, 4 in flight\n- resume through `ListParts`, skipping parts that are already uploaded\n- failed parts go through the existing retry policy\n\n`cargo test --workspace` passes, including a test that drops the connection mid-upload."),
			prompt(4*hour+15*minute, "Before we merge: plan checksum verification so a resumed upload can't stitch together a corrupted file."),
			plan(4*hour+10*minute, "", "ferry-checksums.md"),
		},
	},
	{
		id: "0199f8a2-4c6d-7e31-8b07-d5e2a9c4f716", project: "infra", branch: "ci-oidc", title: "CI deploy credentials via OIDC",
		events: []event{
			prompt(day+3*hour, "Our deploy jobs use a long-lived AWS access key stored as a CI secret. Plan moving to GitHub OIDC."),
			reply(day+2*hour+55*minute, "Three workflows read `AWS_ACCESS_KEY_ID`: `deploy-staging.yml`, `deploy-prod.yml` and `nightly-snapshot.yml`, all as the same `ci-deploy` IAM user. Here's a plan."),
			plan(day+2*hour+50*minute, "", "infra-oidc.md"),
			approve(day + 2*hour + 40*minute),
			reply(day+2*hour+39*minute, "Starting with the Terraform for the OIDC provider and the two roles."),
		},
	},
	{
		id: "0199e4b7-1f8a-7c52-a3d6-6b9e0f4c2a81", project: "ferry", branch: "main", title: "Parallel directory walk",
		events: []event{
			prompt(3*day+hour, "`ferry sync` spends most of its time walking large trees before any upload starts. Plan making the walk parallel."),
			reply(3*day+57*minute, "The walk is a single-threaded `walkdir::WalkDir` in `scan.rs`, with hashing done inline. On a 400k-file tree that's about 90 s before the first upload."),
			plan(3*day+52*minute, "", "ferry-parallel-walk.md"),
			reject(3*day+45*minute, "Too much churn. Don't swap in a new crate, just move hashing onto a bounded worker pool in scan.rs."),
			reply(3*day+43*minute, "Makes sense: the walk itself is cheap, it's the inline hashing that dominates. A bounded `crossbeam_channel` feeding N hashing workers keeps `WalkDir` as is and caps memory. I'll write that up as a smaller plan."),
		},
	},
	{
		id: "019a21d0-8e3f-7a94-b1c5-2d7f6e0a9b38", project: "tidepool", branch: "main",
		events: []event{
			prompt(2*hour+15*minute, "Why does `go test -race ./internal/ingest/...` hang on CI but not locally?"),
			reply(2*hour+10*minute, "It's `TestFlushOnShutdown`. It waits on `flushed` with no timeout, and the flusher only runs when its ticker fires. Under `-race` on a 2-core CI runner, `Close()` can win the race against the first tick: the ticker goroutine exits without flushing and the test waits forever.\n\nLocally you have more cores, so the tick almost always lands first."),
			prompt(2*hour+8*minute, "Fix it"),
			work(2*hour+5*minute, "go test -race -count=50 -run TestFlushOnShutdown ./internal/ingest/"),
			reply(2*hour+minute, "`Close()` now flushes synchronously before stopping the ticker, and the test waits with a 5 s timeout so a regression fails instead of hanging. 50 runs under `-race` pass."),
		},
	},
	{
		id: "0199d6f3-5a2b-7e08-94c1-8f3e7b2d6a50", project: "lumen", branch: "vite-6", title: "Bump Vite to 6",
		events: []event{
			prompt(6*day+2*hour, "Bump Vite to 6 and fix whatever breaks in the build."),
			work(6*day+hour+50*minute, "pnpm up vite@6 @vitejs/plugin-react@latest"),
			work(6*day+hour+45*minute, "pnpm build"),
			reply(6*day+hour+40*minute, "Upgraded `vite` to 6 and `@vitejs/plugin-react` to match. Two fixes were needed:\n\n- setting `resolve.conditions` no longer keeps the defaults, so the config now spreads `defaultClientConditions`\n- the Sass legacy API warning: `css.preprocessorOptions.scss.api` is now `modern-compiler`\n\n`pnpm build` and `pnpm test` pass, and the bundle is 3% smaller."),
		},
	},
}

var agySessions = []session{
	{
		id: "6d0f3b8e-2c47-4a91-b5e8-1f9a7c3d0e62", project: "pebble", title: "Offline sync for notes",
		events: []event{
			prompt(2*hour+10*minute, "Notes edited offline are lost if the app is killed before it reconnects. Plan proper offline sync."),
			plan(2*hour+5*minute, "implementation_plan.md", "pebble-offline-sync-plan.md"),
			reply(2*hour+4*minute, "Unsynced edits only live in memory in `SyncManager` and reach SQLite after the server acks them. The implementation plan persists every edit to a local outbox first. Please review it before I start."),
			approve(hour + 50*minute),
			draft(hour+49*minute, "task.md", "pebble-offline-sync-task.md"),
			work(hour+40*minute, "./gradlew :app:testDebugUnitTest"),
			work(hour+20*minute, "./gradlew :app:connectedDebugAndroidTest"),
			draft(hour+12*minute, "walkthrough.md", "pebble-offline-sync-walkthrough.md"),
			reply(hour+10*minute, "Offline sync is in. The walkthrough covers what changed and how I tested it; the conflict banner still has placeholder copy."),
		},
	},
	{
		id: "a8e25c71-4f0b-4d36-9c1a-7e3b5d9f2048", project: "lumen", title: "CSV and PNG chart export",
		events: []event{
			prompt(22*minute, "Add export to the chart widgets: PNG for slides, CSV for the underlying data."),
			plan(15*minute, "implementation_plan.md", "lumen-chart-export.md"),
			reply(14*minute, "I've written an implementation plan for chart export. Please review it before I start."),
		},
	},
	{
		id: "3b9e6a04-d17c-4f52-8a3e-c05b2f8d7194", project: "pebble",
		events: []event{
			prompt(4*day+hour, "Why does the Android build fail after upgrading to Gradle 8.10?"),
			reply(4*day+55*minute, "The failure is `Unable to load class 'org.gradle.api.internal.HasConvention'`. That class is gone from Gradle's internals, and `app/build.gradle` still applies `kotlin-android-extensions` through Kotlin Gradle plugin `1.7.10`, which calls it at configuration time.\n\nBump the Kotlin plugin to `2.0.21` and drop `kotlin-android-extensions`. The app only uses it for synthetic view imports in two activities, which can move to view binding."),
			prompt(4*day+40*minute, "Do it"),
			work(4*day+30*minute, "./gradlew assembleDebug"),
			reply(4*day+28*minute, "The Kotlin plugin is on `2.0.21`, synthetics are replaced with view binding in `MainActivity` and `EditorActivity`, and `./gradlew assembleDebug` succeeds on Gradle 8.10."),
		},
	},
}
