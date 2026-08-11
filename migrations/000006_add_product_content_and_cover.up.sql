-- Stage 21 broadens the Product record with optional reviewed editorial text.
-- Empty defaults preserve every migration-4 row without inventing content.
ALTER TABLE public.products
    ADD COLUMN description text NOT NULL DEFAULT '',
    ADD COLUMN material text NOT NULL DEFAULT '',
    ADD COLUMN dimensions text NOT NULL DEFAULT '',
    ADD CONSTRAINT products_description_trimmed
        CHECK (description = btrim(description)),
    ADD CONSTRAINT products_description_length
        CHECK (char_length(description) <= 6000),
    ADD CONSTRAINT products_material_trimmed
        CHECK (material = btrim(material)),
    ADD CONSTRAINT products_material_length
        CHECK (char_length(material) <= 500),
    ADD CONSTRAINT products_dimensions_trimmed
        CHECK (dimensions = btrim(dimensions)),
    ADD CONSTRAINT products_dimensions_length
        CHECK (char_length(dimensions) <= 500);

-- One Product can own at most one current cover. Keeping binary data and its
-- review metadata in a separate table prevents catalogue-list queries from
-- loading image bytes and leaves a future gallery model free to evolve.
CREATE TABLE public.product_cover_images (
    -- The Product identity is also the primary key, enforcing the one-cover
    -- Stage 21 boundary. A future Product removal cannot leave orphan media.
    product_id bigint PRIMARY KEY
        REFERENCES public.products (id)
        ON DELETE CASCADE,
    -- Cover revision changes only when the image or its metadata is replaced.
    -- Public paths include it, while every request still rechecks publication.
    version bigint NOT NULL DEFAULT 1,
    -- Stage 21 accepts only formats decoded by the Go standard library.
    content_type text NOT NULL,
    content bytea NOT NULL,
    byte_size integer NOT NULL,
    width integer NOT NULL,
    height integer NOT NULL,
    sha256 bytea NOT NULL,
    -- Alternative text is mandatory for meaningful Product photography.
    -- Caption remains optional interface copy displayed beside the image.
    alt_text text NOT NULL,
    caption text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT product_cover_images_version_positive
        CHECK (version > 0),
    CONSTRAINT product_cover_images_content_type_supported
        CHECK (content_type IN ('image/jpeg', 'image/png')),
    CONSTRAINT product_cover_images_byte_size_supported
        CHECK (byte_size BETWEEN 1 AND 8388608),
    CONSTRAINT product_cover_images_content_size_matches
        CHECK (octet_length(content) = byte_size),
    CONSTRAINT product_cover_images_width_supported
        CHECK (width BETWEEN 1 AND 10000),
    CONSTRAINT product_cover_images_height_supported
        CHECK (height BETWEEN 1 AND 10000),
    CONSTRAINT product_cover_images_pixel_count_supported
        CHECK ((width::bigint * height::bigint) <= 25000000),
    CONSTRAINT product_cover_images_sha256_length
        CHECK (octet_length(sha256) = 32),
    CONSTRAINT product_cover_images_alt_text_trimmed
        CHECK (alt_text = btrim(alt_text)),
    CONSTRAINT product_cover_images_alt_text_length
        CHECK (char_length(alt_text) BETWEEN 1 AND 300),
    CONSTRAINT product_cover_images_caption_trimmed
        CHECK (caption = btrim(caption)),
    CONSTRAINT product_cover_images_caption_length
        CHECK (char_length(caption) <= 500),
    CONSTRAINT product_cover_images_timestamp_order
        CHECK (updated_at >= created_at)
);
