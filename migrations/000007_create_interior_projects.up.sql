-- Stage 22 creates the durable Interior Design project boundary in one
-- vertical migration. Interior records remain separate from Architecture so
-- the next discipline can evolve without a premature shared Project schema.
CREATE TABLE public.interior_projects (
    -- PostgreSQL owns the internal identity. Public routes use the canonical slug,
    -- while protected routes use this positive value only after authentication.
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Slugs are bounded canonical URL segments. The unique constraint also
    -- gives the administrator form one named conflict it can explain safely.
    slug text NOT NULL,
    -- Title and typology are required public facts once a project is published.
    title text NOT NULL,
    typology text NOT NULL,
    -- Location, year, and description may remain absent while a Draft is being
    -- prepared. Empty text defaults avoid inventing content; NULL is the honest
    -- representation for an unknown year.
    location text NOT NULL DEFAULT '',
    project_year integer,
    -- Project status is required editorial content such as Completed or
    -- Ongoing. It is deliberately distinct from the publication lifecycle.
    project_status text NOT NULL,
    description text NOT NULL DEFAULT '',
    -- Editorial order is deterministic with id as the final tie-breaker.
    sort_order integer NOT NULL,
    -- Draft is fail-closed. Public repositories may select only Published rows,
    -- while Archived records remain available to authenticated administrators.
    publication_status text NOT NULL DEFAULT 'draft',
    -- Every protected text or cover mutation increments this revision so a
    -- stale browser form cannot silently overwrite newer administrator work.
    version bigint NOT NULL DEFAULT 1,
    -- The writer deliberately advances updated_at; no hidden trigger owns that
    -- behavior. Both values start at the transaction timestamp on insertion.
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT interior_projects_slug_length
        CHECK (char_length(slug) BETWEEN 1 AND 120),
    CONSTRAINT interior_projects_slug_format
        CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    CONSTRAINT interior_projects_slug_unique
        UNIQUE (slug),

    CONSTRAINT interior_projects_title_trimmed
        CHECK (title = btrim(title)),
    CONSTRAINT interior_projects_title_length
        CHECK (char_length(title) BETWEEN 1 AND 160),

    CONSTRAINT interior_projects_typology_trimmed
        CHECK (typology = btrim(typology)),
    CONSTRAINT interior_projects_typology_length
        CHECK (char_length(typology) BETWEEN 1 AND 80),

    CONSTRAINT interior_projects_location_trimmed
        CHECK (location = btrim(location)),
    CONSTRAINT interior_projects_location_length
        CHECK (char_length(location) <= 160),

    -- A missing year is valid. When supplied, four decimal digits preserve the
    -- learning model's integer year without accepting sentinel or negative data.
    CONSTRAINT interior_projects_project_year_supported
        CHECK (
            project_year IS NULL OR
            project_year BETWEEN 1000 AND 9999
        ),

    CONSTRAINT interior_projects_project_status_trimmed
        CHECK (project_status = btrim(project_status)),
    CONSTRAINT interior_projects_project_status_length
        CHECK (char_length(project_status) BETWEEN 1 AND 80),

    CONSTRAINT interior_projects_description_trimmed
        CHECK (description = btrim(description)),
    CONSTRAINT interior_projects_description_length
        CHECK (char_length(description) <= 6000),

    CONSTRAINT interior_projects_sort_order_positive
        CHECK (sort_order > 0),
    CONSTRAINT interior_projects_publication_status_supported
        CHECK (publication_status IN ('draft', 'published', 'archived')),
    CONSTRAINT interior_projects_version_positive
        CHECK (version > 0),
    CONSTRAINT interior_projects_timestamp_order
        CHECK (updated_at >= created_at)
);

-- The public index contains no Draft or Archived rows and already supplies the
-- exact (sort_order, id) order used to derive consecutive portfolio numbers.
CREATE INDEX interior_projects_published_order_idx
    ON public.interior_projects (sort_order, id)
    WHERE publication_status = 'published';

-- One Interior project can own at most one current reviewed cover. Binary data
-- remains outside the ordinary project row so list/detail metadata queries do
-- not load image bytes and a later gallery model can be designed independently.
CREATE TABLE public.interior_project_cover_images (
    -- The owner is also the primary key, enforcing the one-cover Stage 22
    -- boundary. Removing a future project cannot leave orphan image data.
    interior_project_id bigint NOT NULL,
    -- Image revision changes on upload or replacement and becomes part of the
    -- public/protected exact media path.
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

    CONSTRAINT interior_project_cover_images_pkey
        PRIMARY KEY (interior_project_id),
    CONSTRAINT interior_project_cover_images_project_id_foreign
        FOREIGN KEY (interior_project_id)
        REFERENCES public.interior_projects (id)
        ON DELETE CASCADE,
    CONSTRAINT interior_project_cover_images_version_positive
        CHECK (version > 0),
    CONSTRAINT interior_project_cover_images_content_type_supported
        CHECK (content_type IN ('image/jpeg', 'image/png')),
    CONSTRAINT interior_project_cover_images_byte_size_supported
        CHECK (byte_size BETWEEN 1 AND 8388608),
    CONSTRAINT interior_project_cover_images_content_size_matches
        CHECK (octet_length(content) = byte_size),
    CONSTRAINT interior_project_cover_images_width_supported
        CHECK (width BETWEEN 1 AND 10000),
    CONSTRAINT interior_project_cover_images_height_supported
        CHECK (height BETWEEN 1 AND 10000),
    CONSTRAINT interior_project_cover_images_pixel_count_supported
        CHECK ((width::bigint * height::bigint) <= 25000000),
    CONSTRAINT interior_project_cover_images_sha256_length
        CHECK (octet_length(sha256) = 32),
    CONSTRAINT interior_project_cover_images_alt_text_trimmed
        CHECK (alt_text = btrim(alt_text)),
    CONSTRAINT interior_project_cover_images_alt_text_length
        CHECK (char_length(alt_text) BETWEEN 1 AND 300),
    CONSTRAINT interior_project_cover_images_caption_trimmed
        CHECK (caption = btrim(caption)),
    CONSTRAINT interior_project_cover_images_caption_length
        CHECK (char_length(caption) <= 500),
    CONSTRAINT interior_project_cover_images_timestamp_order
        CHECK (updated_at >= created_at)
);
