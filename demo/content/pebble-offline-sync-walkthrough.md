# Walkthrough: offline sync

## What changed

- `db/migrations/0007_outbox.sql` adds the outbox table
- `NoteRepository.save` writes the note and its outbox row in one transaction
- `SyncManager` drains the outbox in order on reconnect, backing off from 2 s up to 5 minutes
- `409` responses keep both versions and flag the note as conflicted

## How it was tested

- `./gradlew :app:testDebugUnitTest`: 14 new tests for outbox ordering, backoff and conflicts
- Pixel 8 emulator: airplane mode, edited three notes, force-quit, reopened, reconnected; all three synced in order

## Not done

- The conflict banner copy is a placeholder until design signs off
