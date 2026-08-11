package main

import (
	"context"
	"math"
)

// adminInteriorProjectCoverWriteInput contains only normalized image facts and
// reviewed text. Browser filenames and claimed media headers never enter this
// persistence contract.
type adminInteriorProjectCoverWriteInput struct {
	// ContentType is derived from the standard-library image decoder.
	ContentType string
	// Content is one complete normalized JPEG or PNG file.
	Content []byte
	// ByteSize must equal len(Content).
	ByteSize int
	// Width is the decoded pixel width.
	Width int
	// Height is the decoded pixel height.
	Height int
	// SHA256 is the integrity digest of the exact normalized bytes.
	SHA256 [32]byte
	// AltText is required reviewed alternative text.
	AltText string
	// Caption is optional reviewed visible copy.
	Caption string
}

// adminInteriorProjectCoverWriteResult contains database-owned identities and
// revisions that a handler must verify before redirecting.
type adminInteriorProjectCoverWriteResult struct {
	// ProjectID is the positive owning Interior-project identity.
	ProjectID int64
	// ProjectVersion is the new optimistic project revision.
	ProjectVersion int64
	// CoverVersion is the inserted or incremented image revision.
	CoverVersion int64
}

// upsertAdminInteriorProjectCoverSQL advances the project revision and cover in
// one statement. A stale project revision gives the cover CTE no source row, so
// it cannot insert or replace image content independently.
const upsertAdminInteriorProjectCoverSQL = `WITH current_project AS MATERIALIZED (
    SELECT id
    FROM public.interior_projects
    WHERE id = $1
),
updated_project AS (
    UPDATE public.interior_projects
    SET
        updated_at = CURRENT_TIMESTAMP,
        version = version + 1
    WHERE id = $1 AND version = $2
    RETURNING id, version
),
upserted_cover AS (
    INSERT INTO public.interior_project_cover_images (
        interior_project_id,
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
    FROM updated_project
    ON CONFLICT (interior_project_id) DO UPDATE
    SET
        version = interior_project_cover_images.version + 1,
        content_type = EXCLUDED.content_type,
        content = EXCLUDED.content,
        byte_size = EXCLUDED.byte_size,
        width = EXCLUDED.width,
        height = EXCLUDED.height,
        sha256 = EXCLUDED.sha256,
        alt_text = EXCLUDED.alt_text,
        caption = EXCLUDED.caption,
        updated_at = CURRENT_TIMESTAMP
    RETURNING interior_project_id, version
)
SELECT
    COALESCE((SELECT id FROM updated_project), 0),
    COALESCE((SELECT version FROM updated_project), 0),
    COALESCE((SELECT version FROM upserted_cover), 0),
    EXISTS(SELECT 1 FROM current_project)`

// UpsertCover inserts or replaces one reviewed cover only while the submitted
// project version remains current. Project and cover revisions change atomically.
func (writer *postgresAdminInteriorProjectWriter) UpsertCover(
	ctx context.Context,
	projectID int64,
	expectedProjectVersion int64,
	input adminInteriorProjectCoverWriteInput,
) (adminInteriorProjectCoverWriteResult, error) {
	if ctx == nil || projectID <= 0 || expectedProjectVersion <= 0 ||
		expectedProjectVersion == math.MaxInt64 ||
		!isValidAdminInteriorProjectCoverWriteInput(input) {
		return adminInteriorProjectCoverWriteResult{},
			errAdminInteriorProjectWriteInvalid
	}
	if writer == nil || writer.queryRow == nil {
		return adminInteriorProjectCoverWriteResult{},
			errAdminInteriorProjectWriteFailed
	}

	row := writer.queryRow(
		ctx,
		upsertAdminInteriorProjectCoverSQL,
		projectID,
		expectedProjectVersion,
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
		return adminInteriorProjectCoverWriteResult{},
			errAdminInteriorProjectWriteFailed
	}

	var result adminInteriorProjectCoverWriteResult
	var projectExists bool
	if err := row.Scan(
		&result.ProjectID,
		&result.ProjectVersion,
		&result.CoverVersion,
		&projectExists,
	); err != nil {
		return adminInteriorProjectCoverWriteResult{},
			errAdminInteriorProjectWriteFailed
	}
	if result == (adminInteriorProjectCoverWriteResult{}) {
		if projectExists {
			return adminInteriorProjectCoverWriteResult{},
				errAdminInteriorProjectWriteConflict
		}

		return adminInteriorProjectCoverWriteResult{},
			errAdminInteriorProjectNotFound
	}
	if !projectExists || result.ProjectID != projectID ||
		result.ProjectVersion != expectedProjectVersion+1 ||
		result.CoverVersion <= 0 {
		return adminInteriorProjectCoverWriteResult{},
			errAdminInteriorProjectWriteFailed
	}

	return result, nil
}

// isValidAdminInteriorProjectCoverWriteInput proves that every persisted image
// fact matches the encoded bytes and all reviewed text satisfies migration 7.
func isValidAdminInteriorProjectCoverWriteInput(
	input adminInteriorProjectCoverWriteInput,
) bool {
	if input.ByteSize != len(input.Content) ||
		!isValidRequiredReviewedCoverText(
			input.AltText,
			reviewedCoverAltTextMaximumLength,
		) ||
		!isValidOptionalReviewedCoverText(
			input.Caption,
			reviewedCoverCaptionMaximumLength,
		) {
		return false
	}

	inspection, err := inspectReviewedCover(input.Content, false)
	if err != nil {
		return false
	}

	return input.ContentType == inspection.ContentType &&
		input.Width == inspection.Width &&
		input.Height == inspection.Height &&
		input.SHA256 == inspection.SHA256
}
