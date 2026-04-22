-- Add object_key column to attachment so the DownloadFile proxy can look up
-- the S3/TOS object key directly instead of parsing it from the stored URL.
-- This prevents a hostile URL ever persisted into attachment.url from turning
-- the download proxy into a cross-bucket exfil primitive via
-- S3Storage.KeyFromURL's "everything after last slash" fallback.
--
-- Nullable for backfill safety: legacy rows keep NULL and the handler falls
-- back to KeyFromURL for them. Tightening to NOT NULL is a follow-up once all
-- rows are backfilled.
ALTER TABLE attachment ADD COLUMN object_key TEXT;

-- Backfill: our random-hex keys contain no slashes, so the last path segment
-- of the stored URL equals the S3 key for both CDN and bucket URLs.
UPDATE attachment SET object_key = split_part(url, '/', -1);
