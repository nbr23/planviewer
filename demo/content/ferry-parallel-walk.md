# Parallelize the directory walker

## Summary

Replace `walkdir` with `ignore::WalkParallel` and stream entries to the uploader as they are found.

## Changes

- `scan.rs`: `WalkBuilder::new(root).threads(num_cpus::get()).build_parallel()`
- Honour `.ferryignore` through the `ignore` crate's custom ignore files
- Hash on the walker threads and send `(path, size, hash)` to the uploader over a channel
- Start uploading before the walk finishes

## Risks

- Entry order is no longer deterministic, so the manifest must be sorted before it is written
- `ignore` has different symlink defaults from `walkdir`
