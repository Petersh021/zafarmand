-- Stage 18 creates the minimal durable identity, public text, ordering,
-- publication-state, and timestamp fields needed by the Product catalogue.
-- Content management, media, and featured placement remain future concerns.
CREATE TABLE public.products (
    -- PostgreSQL owns the internal identity; public routes address records by
    -- their stable human-readable slug instead of exposing this value.
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Slugs are required, bounded, lowercase URL segments. The format check
    -- permits single hyphens only between nonempty alphanumeric components.
    slug text NOT NULL,
    -- Names and categories become public catalogue facts only after the row is
    -- explicitly moved into the published state.
    name text NOT NULL,
    category text NOT NULL,
    -- Positive positions provide deterministic editorial order. The generated
    -- identity is the final tie-breaker when two records share one position.
    sort_order integer NOT NULL,
    -- Draft is the fail-closed default. Public queries introduced in this stage
    -- may select only published rows; archived rows remain stored but private.
    publication_status text NOT NULL DEFAULT 'draft',
    -- Both timestamps begin at the transaction timestamp for the insert. There
    -- is no update trigger; a later writer must deliberately change updated_at.
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT products_slug_length
        CHECK (char_length(slug) BETWEEN 1 AND 120),
    CONSTRAINT products_slug_format
        CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    CONSTRAINT products_slug_unique
        UNIQUE (slug),
    CONSTRAINT products_name_trimmed
        CHECK (name = btrim(name)),
    CONSTRAINT products_name_length
        CHECK (char_length(name) BETWEEN 1 AND 160),
    CONSTRAINT products_category_trimmed
        CHECK (category = btrim(category)),
    CONSTRAINT products_category_length
        CHECK (char_length(category) BETWEEN 1 AND 80),
    CONSTRAINT products_sort_order_positive
        CHECK (sort_order > 0),
    CONSTRAINT products_publication_status_supported
        CHECK (publication_status IN ('draft', 'published', 'archived')),
    -- Future updates must never claim to predate the record's creation.
    CONSTRAINT products_timestamp_order
        CHECK (updated_at >= created_at)
);

-- The public catalogue reads only published rows in editorial order. A partial
-- index keeps drafts and archives out of this public-read access path while id
-- supplies deterministic ordering for equal positions.
CREATE INDEX products_published_order_idx
    ON public.products (sort_order, id)
    WHERE publication_status = 'published';
