-- Stage 23 creates the durable Architecture Design project boundary in one
-- vertical migration. Architecture remains independent from Interior so each
-- discipline can evolve without a premature shared Project relation.
CREATE TABLE public.architecture_projects (
    -- PostgreSQL owns the internal identity. Public routes use the canonical
    -- slug, while protected routes use this positive value after authentication.
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Slugs are bounded canonical URL segments. The named unique constraint
    -- also gives the administrator form one conflict it can explain safely.
    slug text NOT NULL,
    -- Title and typology are required public facts once a project is published.
    title text NOT NULL,
    typology text NOT NULL,
    -- Location, year, and description may remain absent while a Draft is being
    -- prepared. Empty text and NULL preserve genuinely unknown editorial data.
    location text NOT NULL DEFAULT '',
    project_year integer,
    -- Project status is required editorial copy such as Completed or Ongoing.
    -- It remains deliberately separate from the publication lifecycle below.
    project_status text NOT NULL,
    description text NOT NULL DEFAULT '',
    -- Editorial order is deterministic with id as the final tie-breaker.
    sort_order integer NOT NULL,
    -- Draft is fail-closed. Public readers may select only Published rows,
    -- while Archived rows stay available to authenticated administrators.
    publication_status text NOT NULL DEFAULT 'draft',
    -- Every protected text or cover mutation increments this revision so a
    -- stale form cannot silently overwrite newer administrator work.
    version bigint NOT NULL DEFAULT 1,
    -- The writer advances updated_at explicitly; no hidden trigger owns that
    -- behavior. Both timestamps begin at the transaction time on insertion.
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT architecture_projects_slug_length
        CHECK (char_length(slug) BETWEEN 1 AND 120),
    CONSTRAINT architecture_projects_slug_format
        CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    CONSTRAINT architecture_projects_slug_unique
        UNIQUE (slug),

    CONSTRAINT architecture_projects_title_trimmed
        CHECK (title = btrim(title)),
    CONSTRAINT architecture_projects_title_length
        CHECK (char_length(title) BETWEEN 1 AND 160),

    CONSTRAINT architecture_projects_typology_trimmed
        CHECK (typology = btrim(typology)),
    CONSTRAINT architecture_projects_typology_length
        CHECK (char_length(typology) BETWEEN 1 AND 80),

    CONSTRAINT architecture_projects_location_trimmed
        CHECK (location = btrim(location)),
    CONSTRAINT architecture_projects_location_length
        CHECK (char_length(location) <= 160),

    -- A missing year is valid. When supplied, four decimal digits preserve the
    -- learning model's integer year without accepting sentinel or negative data.
    CONSTRAINT architecture_projects_project_year_supported
        CHECK (
            project_year IS NULL OR
            project_year BETWEEN 1000 AND 9999
        ),

    CONSTRAINT architecture_projects_project_status_trimmed
        CHECK (project_status = btrim(project_status)),
    CONSTRAINT architecture_projects_project_status_length
        CHECK (char_length(project_status) BETWEEN 1 AND 80),

    CONSTRAINT architecture_projects_description_trimmed
        CHECK (description = btrim(description)),
    CONSTRAINT architecture_projects_description_length
        CHECK (char_length(description) <= 6000),

    CONSTRAINT architecture_projects_sort_order_positive
        CHECK (sort_order > 0),
    CONSTRAINT architecture_projects_publication_status_supported
        CHECK (publication_status IN ('draft', 'published', 'archived')),
    CONSTRAINT architecture_projects_version_positive
        CHECK (version > 0),
    CONSTRAINT architecture_projects_timestamp_order
        CHECK (updated_at >= created_at)
);

-- The public index contains no Draft or Archived rows and supplies the exact
-- (sort_order, id) order used to derive consecutive portfolio numbers.
CREATE INDEX architecture_projects_published_order_idx
    ON public.architecture_projects (sort_order, id)
    WHERE publication_status = 'published';

-- One Architecture project can own at most one current reviewed cover. Binary
-- data remains outside the ordinary project row so listing and detail metadata
-- reads do not load image bytes and a future gallery stays independently scoped.
CREATE TABLE public.architecture_project_cover_images (
    -- The owner is also the primary key, enforcing the one-cover Stage 23
    -- boundary. Removing a future project cannot leave orphan image data.
    architecture_project_id bigint NOT NULL,
    -- Image revision changes on upload or replacement and becomes part of the
    -- exact public and protected media path.
    version bigint NOT NULL DEFAULT 1,
    -- Type, bytes, dimensions, and digest are derived from normalized decoded
    -- pixels by Go rather than trusted from browser filenames or headers.
    content_type text NOT NULL,
    content bytea NOT NULL,
    byte_size integer NOT NULL,
    width integer NOT NULL,
    height integer NOT NULL,
    sha256 bytea NOT NULL,
    -- Meaningful alt text is required. Caption is optional reviewed visible copy.
    alt_text text NOT NULL,
    caption text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT architecture_project_cover_images_pkey
        PRIMARY KEY (architecture_project_id),
    CONSTRAINT architecture_project_cover_images_project_id_foreign
        FOREIGN KEY (architecture_project_id)
        REFERENCES public.architecture_projects (id)
        ON DELETE CASCADE,
    CONSTRAINT architecture_project_cover_images_version_positive
        CHECK (version > 0),
    CONSTRAINT architecture_project_cover_images_content_type_supported
        CHECK (content_type IN ('image/jpeg', 'image/png')),
    CONSTRAINT architecture_project_cover_images_byte_size_supported
        CHECK (byte_size BETWEEN 1 AND 8388608),
    CONSTRAINT architecture_project_cover_images_content_size_matches
        CHECK (octet_length(content) = byte_size),
    CONSTRAINT architecture_project_cover_images_width_supported
        CHECK (width BETWEEN 1 AND 10000),
    CONSTRAINT architecture_project_cover_images_height_supported
        CHECK (height BETWEEN 1 AND 10000),
    CONSTRAINT architecture_project_cover_images_pixel_count_supported
        CHECK ((width::bigint * height::bigint) <= 25000000),
    CONSTRAINT architecture_project_cover_images_sha256_length
        CHECK (octet_length(sha256) = 32),
    CONSTRAINT architecture_project_cover_images_alt_text_trimmed
        CHECK (alt_text = btrim(alt_text)),
    CONSTRAINT architecture_project_cover_images_alt_text_length
        CHECK (char_length(alt_text) BETWEEN 1 AND 300),
    CONSTRAINT architecture_project_cover_images_caption_trimmed
        CHECK (caption = btrim(caption)),
    CONSTRAINT architecture_project_cover_images_caption_length
        CHECK (char_length(caption) <= 500),
    CONSTRAINT architecture_project_cover_images_timestamp_order
        CHECK (updated_at >= created_at)
);
