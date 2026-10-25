# Offline sync

- [x] `outbox` table migration
- [x] Write the note and its outbox row in one transaction
- [x] Drain the outbox in `SyncManager` on reconnect
- [x] Backoff for failed requests
- [x] Conflict handling for `409`
- [x] "Waiting to sync" badge
- [ ] Conflict banner copy (waiting on design)
