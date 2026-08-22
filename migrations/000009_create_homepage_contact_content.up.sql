-- Stage 24 makes the small set of global public-site content durable without
-- coupling it to the discipline repositories. Each parent is a singleton: the
-- fixed key keeps reads explicit while named checks reject accidental extra rows.
CREATE TABLE public.homepage_content (
    -- The public site has exactly one homepage document. Unlike a generated
    -- identity, this fixed key makes the singleton invariant visible in SQL.
    id smallint PRIMARY KEY,
    -- These two fields preserve the current studio identity while allowing an
    -- authenticated administrator to revise the visible homepage copy later.
    studio_name text NOT NULL,
    descriptor text NOT NULL,
    -- False deliberately keeps the reviewed repository placeholder in use until
    -- a managed hero image has been stored successfully in the child table.
    managed_hero_enabled boolean NOT NULL DEFAULT false,
    -- Each optional selection points at one existing discipline record. RESTRICT
    -- makes an editorial reference explicit instead of silently clearing it when
    -- a selected record is deleted; public readers still recheck publication and
    -- current-cover eligibility before showing any selection.
    featured_product_id bigint,
    featured_interior_project_id bigint,
    featured_architecture_project_id bigint,
    -- Managed SEO fields store the complete browser title and description. The
    -- title is not a fragment for the base template to decorate a second time.
    seo_title text NOT NULL,
    seo_description text NOT NULL,
    -- Optimistic concurrency protects one administrator's revision from silently
    -- overwriting another. Writers advance version and updated_at explicitly.
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT homepage_content_singleton
        CHECK (id = 1),
    CONSTRAINT homepage_content_studio_name_trimmed
        CHECK (studio_name = btrim(studio_name)),
    CONSTRAINT homepage_content_studio_name_length
        CHECK (char_length(studio_name) BETWEEN 1 AND 120),
    CONSTRAINT homepage_content_studio_name_single_line
        CHECK (
            position(chr(10) IN studio_name) = 0 AND
            position(chr(13) IN studio_name) = 0
        ),
    CONSTRAINT homepage_content_descriptor_trimmed
        CHECK (descriptor = btrim(descriptor)),
    CONSTRAINT homepage_content_descriptor_length
        CHECK (char_length(descriptor) BETWEEN 1 AND 160),
    CONSTRAINT homepage_content_descriptor_single_line
        CHECK (
            position(chr(10) IN descriptor) = 0 AND
            position(chr(13) IN descriptor) = 0
        ),
    CONSTRAINT homepage_content_featured_product_id_foreign
        FOREIGN KEY (featured_product_id)
        REFERENCES public.products (id)
        ON DELETE RESTRICT,
    CONSTRAINT homepage_content_featured_interior_project_id_foreign
        FOREIGN KEY (featured_interior_project_id)
        REFERENCES public.interior_projects (id)
        ON DELETE RESTRICT,
    CONSTRAINT homepage_content_featured_architecture_project_id_foreign
        FOREIGN KEY (featured_architecture_project_id)
        REFERENCES public.architecture_projects (id)
        ON DELETE RESTRICT,
    CONSTRAINT homepage_content_seo_title_trimmed
        CHECK (seo_title = btrim(seo_title)),
    CONSTRAINT homepage_content_seo_title_length
        CHECK (char_length(seo_title) BETWEEN 1 AND 160),
    CONSTRAINT homepage_content_seo_title_single_line
        CHECK (
            position(chr(10) IN seo_title) = 0 AND
            position(chr(13) IN seo_title) = 0
        ),
    CONSTRAINT homepage_content_seo_description_trimmed
        CHECK (seo_description = btrim(seo_description)),
    CONSTRAINT homepage_content_seo_description_length
        CHECK (char_length(seo_description) BETWEEN 1 AND 320),
    CONSTRAINT homepage_content_seo_description_single_line
        CHECK (
            position(chr(10) IN seo_description) = 0 AND
            position(chr(13) IN seo_description) = 0
        ),
    CONSTRAINT homepage_content_version_positive
        CHECK (version > 0),
    CONSTRAINT homepage_content_timestamp_order
        CHECK (updated_at >= created_at)
);

-- Seed only the current structural identity and SEO fallback. Nullable featured
-- references stay empty, so this migration never invents portfolio records.
INSERT INTO public.homepage_content (
    id,
    studio_name,
    descriptor,
    seo_title,
    seo_description
) VALUES (
    1,
    'Zafarmand',
    'Design Studio',
    'Home | Zafarmand',
    'Zafarmand design studio'
);

-- The separately stored singleton image keeps binary bytes out of ordinary
-- homepage text reads. There is no caption: Stage 24 needs only reviewed image
-- bytes and meaningful alternative text for the visual hero.
CREATE TABLE public.homepage_hero_images (
    -- The owning singleton is also the primary key, so the homepage can have at
    -- most one current managed hero. Removing the homepage removes its image.
    homepage_content_id smallint NOT NULL,
    -- Replacing the image advances its independent media revision. Metadata is
    -- derived from decoded, normalized pixels rather than browser declarations.
    version bigint NOT NULL DEFAULT 1,
    content_type text NOT NULL,
    content bytea NOT NULL,
    byte_size integer NOT NULL,
    width integer NOT NULL,
    height integer NOT NULL,
    sha256 bytea NOT NULL,
    alt_text text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT homepage_hero_images_pkey
        PRIMARY KEY (homepage_content_id),
    CONSTRAINT homepage_hero_images_homepage_content_id_foreign
        FOREIGN KEY (homepage_content_id)
        REFERENCES public.homepage_content (id)
        ON DELETE CASCADE,
    CONSTRAINT homepage_hero_images_version_positive
        CHECK (version > 0),
    CONSTRAINT homepage_hero_images_content_type_supported
        CHECK (content_type IN ('image/jpeg', 'image/png')),
    CONSTRAINT homepage_hero_images_byte_size_supported
        CHECK (byte_size BETWEEN 1 AND 8388608),
    CONSTRAINT homepage_hero_images_content_size_matches
        CHECK (octet_length(content) = byte_size),
    CONSTRAINT homepage_hero_images_width_supported
        CHECK (width BETWEEN 1 AND 10000),
    CONSTRAINT homepage_hero_images_height_supported
        CHECK (height BETWEEN 1 AND 10000),
    CONSTRAINT homepage_hero_images_pixel_count_supported
        CHECK ((width::bigint * height::bigint) <= 25000000),
    CONSTRAINT homepage_hero_images_sha256_length
        CHECK (octet_length(sha256) = 32),
    CONSTRAINT homepage_hero_images_alt_text_trimmed
        CHECK (alt_text = btrim(alt_text)),
    CONSTRAINT homepage_hero_images_alt_text_length
        CHECK (char_length(alt_text) BETWEEN 1 AND 300),
    CONSTRAINT homepage_hero_images_timestamp_order
        CHECK (updated_at >= created_at)
);

-- Contact presentation is independent from inquiry submissions: changing this
-- singleton edits public copy and contact details without touching visitor data.
CREATE TABLE public.contact_content (
    id smallint PRIMARY KEY,
    -- Eyebrow, heading, and introduction preserve the current Contact composition
    -- while remaining concise enough for the established responsive layout.
    eyebrow text NOT NULL,
    heading text NOT NULL,
    introduction text NOT NULL,
    -- Optional studio details use empty text as one consistent absent value. A
    -- displayed phone and its normalized E.164 counterpart form one atomic pair.
    contact_email text NOT NULL DEFAULT '',
    phone_display text NOT NULL DEFAULT '',
    phone_e164 text NOT NULL DEFAULT '',
    address text NOT NULL DEFAULT '',
    seo_title text NOT NULL,
    seo_description text NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT contact_content_singleton
        CHECK (id = 1),
    CONSTRAINT contact_content_eyebrow_trimmed
        CHECK (eyebrow = btrim(eyebrow)),
    CONSTRAINT contact_content_eyebrow_length
        CHECK (char_length(eyebrow) BETWEEN 1 AND 80),
    CONSTRAINT contact_content_eyebrow_single_line
        CHECK (
            position(chr(10) IN eyebrow) = 0 AND
            position(chr(13) IN eyebrow) = 0
        ),
    CONSTRAINT contact_content_heading_trimmed
        CHECK (heading = btrim(heading)),
    CONSTRAINT contact_content_heading_length
        CHECK (char_length(heading) BETWEEN 1 AND 160),
    CONSTRAINT contact_content_heading_single_line
        CHECK (
            position(chr(10) IN heading) = 0 AND
            position(chr(13) IN heading) = 0
        ),
    CONSTRAINT contact_content_introduction_trimmed
        CHECK (introduction = btrim(introduction)),
    CONSTRAINT contact_content_introduction_length
        CHECK (char_length(introduction) BETWEEN 1 AND 1200),
    CONSTRAINT contact_content_email_normalized
        CHECK (contact_email = lower(btrim(contact_email))),
    CONSTRAINT contact_content_email_length
        CHECK (char_length(contact_email) <= 254),
    -- This deliberately modest shape catches whitespace, missing parts, and a
    -- missing domain dot; Go validation mirrors it before attempting a write.
    CONSTRAINT contact_content_email_shape
        CHECK (
            contact_email = '' OR
            contact_email ~ '^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$'
        ),
    CONSTRAINT contact_content_phone_display_trimmed
        CHECK (phone_display = btrim(phone_display)),
    CONSTRAINT contact_content_phone_display_length
        CHECK (char_length(phone_display) <= 60),
    CONSTRAINT contact_content_phone_display_single_line
        CHECK (
            position(chr(10) IN phone_display) = 0 AND
            position(chr(13) IN phone_display) = 0
        ),
    CONSTRAINT contact_content_phone_e164_normalized
        CHECK (phone_e164 = btrim(phone_e164)),
    CONSTRAINT contact_content_phone_pair
        CHECK (
            (phone_display = '' AND phone_e164 = '') OR
            (
                phone_display <> '' AND
                btrim(phone_e164) ~ '^\+[1-9][0-9]{7,14}$'
            )
        ),
    CONSTRAINT contact_content_address_trimmed
        CHECK (address = btrim(address)),
    CONSTRAINT contact_content_address_length
        CHECK (char_length(address) <= 500),
    CONSTRAINT contact_content_seo_title_trimmed
        CHECK (seo_title = btrim(seo_title)),
    CONSTRAINT contact_content_seo_title_length
        CHECK (char_length(seo_title) BETWEEN 1 AND 160),
    CONSTRAINT contact_content_seo_title_single_line
        CHECK (
            position(chr(10) IN seo_title) = 0 AND
            position(chr(13) IN seo_title) = 0
        ),
    CONSTRAINT contact_content_seo_description_trimmed
        CHECK (seo_description = btrim(seo_description)),
    CONSTRAINT contact_content_seo_description_length
        CHECK (char_length(seo_description) BETWEEN 1 AND 320),
    CONSTRAINT contact_content_seo_description_single_line
        CHECK (
            position(chr(10) IN seo_description) = 0 AND
            position(chr(13) IN seo_description) = 0
        ),
    CONSTRAINT contact_content_version_positive
        CHECK (version > 0),
    CONSTRAINT contact_content_timestamp_order
        CHECK (updated_at >= created_at)
);

-- Empty contact details avoid seeding personal information. The public page can
-- omit those optional values until an authenticated administrator supplies them.
INSERT INTO public.contact_content (
    id,
    eyebrow,
    heading,
    introduction,
    seo_title,
    seo_description
) VALUES (
    1,
    'Contact',
    'Begin a conversation',
    'Choose a discipline and share the context Zafarmand should review.',
    'Contact | Zafarmand',
    'Zafarmand design studio'
);
