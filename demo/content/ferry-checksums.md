# Verify chunk checksums before commit

## Summary

A resumed upload trusts that the parts already on the server match the local file. Check that before completing the upload.

## Changes

- Compute a SHA-256 per part while reading and send it as `x-amz-checksum-sha256`
- Keep part checksums in the resume state
- On resume, compare the checksums from `ListParts` with the local file and re-upload any mismatch
- Send the composite checksum with `CompleteMultipartUpload`, so S3 rejects the upload if it doesn't match

## Tests

- Flip one byte of the local file between runs and check that only the affected part is re-uploaded
