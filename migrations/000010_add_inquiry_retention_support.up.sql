-- Stage 25 keeps replay protection after an archived inquiry's personal data
-- is removed. Only a SHA-256 digest of the opaque form key survives; the table
-- contains no visitor name, address, message, or workflow identifier.
CREATE TABLE public.inquiry_submission_tombstones (
    submission_key_hash bytea PRIMARY KEY,
    purged_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT inquiry_submission_tombstones_hash_length
        CHECK (octet_length(submission_key_hash) = 32)
);

-- Retention examines only deliberately archived inquiries whose last real
-- workflow transition predates an operator-confirmed cutoff. The partial index
-- avoids scanning unresolved personal data during that maintenance operation.
CREATE INDEX inquiries_archived_updated_at_id_idx
    ON public.inquiries (updated_at, id)
    WHERE status = 'archived';
