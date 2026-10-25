# Resumable chunked uploads

## Summary

Switch uploads over 64 MiB to S3 multipart uploads and resume from the parts the server already has.

## Changes

- `upload/multipart.rs`: `MultipartUpload { upload_id, part_size, parts }`
- 64 MiB parts, up to 4 in flight, bounded by a semaphore
- On start, look up an existing `upload_id` in `~/.local/state/ferry/uploads.json`, call `ListParts` and skip parts already uploaded
- Each part goes through the existing retry policy; a part that runs out of attempts aborts the run but keeps the `upload_id` for the next resume
- `CompleteMultipartUpload` once every part is in, then drop the state entry
- `ferry gc` aborts multipart uploads older than 7 days

## Tests

- Unit: part splitting for sizes around the part boundaries
- Integration against MinIO: drop the connection after part 3, rerun, and check that only parts 4 and up are sent
