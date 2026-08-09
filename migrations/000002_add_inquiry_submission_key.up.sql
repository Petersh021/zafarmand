-- Stage 14 introduces one opaque key per inquiry so a repeated form request
-- can be recognized without comparing visitor-provided contact information.
-- The column begins nullable because version 000001 databases can already
-- contain rows that predate idempotent submission handling.
ALTER TABLE public.inquiries
    ADD COLUMN submission_key bytea;

-- PostgreSQL's built-in md5 function returns 16 bytes as hexadecimal text.
-- Decoding and joining two independently seeded digests produces the required
-- 32-byte value without depending on the optional pgcrypto extension. The
-- identity, clock, and independent random values make legacy-row collisions
-- vanishingly unlikely; the UNIQUE constraint below still rejects a collision
-- atomically instead of allowing two inquiries to share a key.
UPDATE public.inquiries
SET submission_key =
        decode(
            md5(
                id::text || ':' ||
                clock_timestamp()::text || ':' ||
                random()::text || ':first'
            ),
            'hex'
        ) ||
        decode(
            md5(
                random()::text || ':' ||
                clock_timestamp()::text || ':' ||
                id::text || ':second'
            ),
            'hex'
        )
WHERE submission_key IS NULL;

-- Storage constraints make the database preserve the same fixed-width,
-- required, and globally unique idempotency-key contract as future Go writes.
ALTER TABLE public.inquiries
    ADD CONSTRAINT inquiries_submission_key_length
        CHECK (octet_length(submission_key) = 32);

ALTER TABLE public.inquiries
    ALTER COLUMN submission_key SET NOT NULL;

ALTER TABLE public.inquiries
    ADD CONSTRAINT inquiries_submission_key_unique
        UNIQUE (submission_key);
