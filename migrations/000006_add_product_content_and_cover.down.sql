-- Remove the dependent cover record before narrowing public.products back to
-- its migration-5 shape. No inquiry or administrator relation is affected.
DROP TABLE public.product_cover_images;

ALTER TABLE public.products
    DROP CONSTRAINT products_dimensions_length,
    DROP CONSTRAINT products_dimensions_trimmed,
    DROP CONSTRAINT products_material_length,
    DROP CONSTRAINT products_material_trimmed,
    DROP CONSTRAINT products_description_length,
    DROP CONSTRAINT products_description_trimmed,
    DROP COLUMN dimensions,
    DROP COLUMN material,
    DROP COLUMN description;
