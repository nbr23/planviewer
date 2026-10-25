# Offline sync for notes

## Goal

Edits made offline survive the app being killed, and sync in order once the device is back online.

## Proposed changes

### Storage

- **[NEW]** `outbox` table: `id`, `note_id`, `op` (`upsert` or `delete`), `payload`, `base_version`, `created_at`
- Every edit writes the note and its outbox row in one SQLite transaction

### Sync

- **[MODIFY]** `SyncManager`: drain the outbox in order when connectivity returns, one request per row
- Failed requests stay in the outbox and retry with exponential backoff, capped at 5 minutes
- The server answers `409` when `base_version` is stale; the client keeps both versions and marks the note as conflicted

### UI

- **[NEW]** "Waiting to sync" badge on notes with pending outbox rows
- **[NEW]** Conflict banner with "Keep mine" and "Keep theirs"

## Verification plan

- Unit tests for outbox ordering and the conflict path
- Manual: airplane mode, edit three notes, force-quit, reopen, reconnect, and check that all three sync in order
