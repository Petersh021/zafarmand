-- Reverse only the idempotency boundary introduced by version 000002. Named
-- constraints are removed explicitly so an unexpected schema drift fails
-- visibly, while the inquiries table and all version 000001 data remain.
ALTER TABLE public.inquiries
    DROP CONSTRAINT inquiries_submission_key_unique,
    DROP CONSTRAINT inquiries_submission_key_length,
    DROP COLUMN submission_key;
