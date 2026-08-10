-- Reverse only the revision boundary introduced by version 000005. PostgreSQL
-- removes the column's dependent check constraint with the column itself.
ALTER TABLE public.products
    DROP COLUMN version;
