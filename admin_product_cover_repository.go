package main

import (
	"context"
	"math"
)

// adminProductCoverWriteInput contains only fully decoded, bounded image facts
// and reviewed text. Browser filenames and media headers never enter this type.
type adminProductCoverWriteInput struct {
	// ContentType is derived from the standard-library decoder.
	ContentType string
	// Content is the complete bounded JPEG or PNG file.
	Content []byte
	// ByteSize must exactly equal len(Content).
	ByteSize int
	// Width is the decoded pixel width.
	Width int
	// Height is the decoded pixel height.
	Height int
	// SHA256 provides an integrity digest for the exact encoded bytes.
	SHA256 [32]byte
	// AltText is required reviewed alternative text.
	AltText string
	// Caption is optional reviewed visible copy.
	Caption string
}

// adminProductCoverWriteResult contains database-owned identities and revisions
// that the handler verifies before using its fixed redirect.
type adminProductCoverWriteResult struct {
	// ProductID is the positive owning Product identity.
	ProductID int64
	// ProductVersion is the new optimistic Product revision.
	ProductVersion int64
	// CoverVersion is the inserted or incremented media revision.
	CoverVersion int64
}

// upsertAdminProductCoverSQL changes the Product revision and cover in one
// PostgreSQL statement. If the submitted Product revision is stale, the cover
// CTE receives no source row and therefore cannot insert or replace media.
const upsertAdminProductCoverSQL = `WITH current_product AS MATERIALIZED (
    SELECT id
    FROM public.products
    WHERE id = $1
),
updated_product AS (
    UPDATE public.products
    SET
        updated_at = CURRENT_TIMESTAMP,
        version = version + 1
    WHERE id = $1 AND version = $2
    RETURNING id, version
),
upserted_cover AS (
    INSERT INTO public.product_cover_images (
        product_id,
        version,
        content_type,
        content,
        byte_size,
        width,
        height,
        sha256,
        alt_text,
        caption
    )
    SELECT
        id,
        1,
        $3,
        $4,
        $5,
        $6,
        $7,
        $8,
        $9,
        $10
    FROM updated_product
    ON CONFLICT (product_id) DO UPDATE
    SET
        version = product_cover_images.version + 1,
        content_type = EXCLUDED.content_type,
        content = EXCLUDED.content,
        byte_size = EXCLUDED.byte_size,
        width = EXCLUDED.width,
        height = EXCLUDED.height,
        sha256 = EXCLUDED.sha256,
        alt_text = EXCLUDED.alt_text,
        caption = EXCLUDED.caption,
        updated_at = CURRENT_TIMESTAMP
    RETURNING product_id, version
)
SELECT
    COALESCE((SELECT id FROM updated_product), 0),
    COALESCE((SELECT version FROM updated_product), 0),
    COALESCE((SELECT version FROM upserted_cover), 0),
    EXISTS(SELECT 1 FROM current_product)`

// UpsertCover inserts or replaces one reviewed cover only when the submitted
// Product version is current. Product and cover revisions change atomically.
func (writer *postgresAdminProductWriter) UpsertCover(
	ctx context.Context,
	productID int64,
	expectedProductVersion int64,
	input adminProductCoverWriteInput,
) (adminProductCoverWriteResult, error) {
	if ctx == nil || productID <= 0 || expectedProductVersion <= 0 ||
		expectedProductVersion == math.MaxInt64 ||
		!isValidAdminProductCoverWriteInput(input) {
		return adminProductCoverWriteResult{}, errAdminProductWriteInvalid
	}
	if writer == nil || writer.queryRow == nil {
		return adminProductCoverWriteResult{}, errAdminProductWriteFailed
	}

	row := writer.queryRow(
		ctx,
		upsertAdminProductCoverSQL,
		productID,
		expectedProductVersion,
		input.ContentType,
		input.Content,
		input.ByteSize,
		input.Width,
		input.Height,
		input.SHA256[:],
		input.AltText,
		input.Caption,
	)
	if row == nil {
		return adminProductCoverWriteResult{}, errAdminProductWriteFailed
	}

	var result adminProductCoverWriteResult
	var productExists bool
	if err := row.Scan(
		&result.ProductID,
		&result.ProductVersion,
		&result.CoverVersion,
		&productExists,
	); err != nil {
		return adminProductCoverWriteResult{}, errAdminProductWriteFailed
	}
	if result == (adminProductCoverWriteResult{}) {
		if productExists {
			return adminProductCoverWriteResult{}, errAdminProductWriteConflict
		}

		return adminProductCoverWriteResult{}, errAdminProductNotFound
	}
	if !productExists || result.ProductID != productID ||
		result.ProductVersion != expectedProductVersion+1 ||
		result.CoverVersion <= 0 {
		return adminProductCoverWriteResult{}, errAdminProductWriteFailed
	}

	return result, nil
}

// isValidAdminProductCoverWriteInput proves that persisted image facts match
// the encoded bytes and that all reviewed text satisfies migration 6.
func isValidAdminProductCoverWriteInput(
	input adminProductCoverWriteInput,
) bool {
	if input.ByteSize != len(input.Content) ||
		!isValidRequiredProductCoverText(
			input.AltText,
			productCoverAltTextMaximumLength,
		) ||
		!isValidOptionalProductCoverText(
			input.Caption,
			productCoverCaptionMaximumLength,
		) {
		return false
	}

	inspection, err := inspectProductCover(input.Content, false)
	if err != nil {
		return false
	}

	return input.ContentType == inspection.ContentType &&
		input.Width == inspection.Width &&
		input.Height == inspection.Height &&
		input.SHA256 == inspection.SHA256
}
