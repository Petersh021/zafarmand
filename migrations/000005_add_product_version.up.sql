-- Stage 20 adds one revision value for optimistic Product editing. A browser
-- must submit the version it read, so a stale form cannot overwrite a newer
-- administrator save without an explicit refresh.
ALTER TABLE public.products
    ADD COLUMN version bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT products_version_positive
        CHECK (version > 0);
