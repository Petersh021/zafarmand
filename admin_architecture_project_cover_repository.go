package main

import (
	"context"
	"math"
)

// adminArchitectureProjectCoverWriteInput contains normalized image facts and
// reviewed copy. Browser filenames and claimed media headers never enter this
// persistence contract.
type adminArchitectureProjectCoverWriteInput struct {
	// ContentType is derived from the standard-library image decoder.
	ContentType string
	// Content is one complete normalized JPEG or PNG file.
	Content []byte
	// ByteSize must exactly equal len(Content).
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

// adminArchitectureProjectCoverWriteResult contains database-owned identities
// and revisions that a handler must verify before redirecting.
type adminArchitectureProjectCoverWriteResult struct {
	// ProjectID is the positive owning Architecture-project identity.
	ProjectID int64
	// ProjectVersion is the new optimistic project revision.
	ProjectVersion int64
	// CoverVersion is the inserted or incremented image revision.
	CoverVersion int64
}

// upsertAdminArchitectureProjectCoverSQL advances project and cover revisions
// atomically. A stale project revision gives the cover CTE no source row.
const upsertAdminArchitectureProjectCoverSQL = `WITH current_project AS MATERIALIZED (
    SELECT id
    FROM public.architecture_projects
    WHERE id = $1
),
updated_project AS (
    UPDATE public.architecture_projects
    SET
        updated_at = CURRENT_TIMESTAMP,
        version = version + 1
    WHERE id = $1 AND version = $2
    RETURNING id, version
),
upserted_cover AS (
    INSERT INTO public.architecture_project_cover_images (
        architecture_project_id,
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
    ON CONFLICT (architecture_project_id) DO UPDATE
    SET
        version = architecture_project_cover_images.version + 1,
        content_type = EXCLUDED.content_type,
        content = EXCLUDED.content,
        byte_size = EXCLUDED.byte_size,
        width = EXCLUDED.width,
        height = EXCLUDED.height,
        sha256 = EXCLUDED.sha256,
        alt_text = EXCLUDED.alt_text,
        caption = EXCLUDED.caption,
        updated_at = CURRENT_TIMESTAMP
    RETURNING architecture_project_id, version
)
SELECT
    COALESCE((SELECT id FROM updated_project), 0),
    COALESCE((SELECT version FROM updated_project), 0),
    COALESCE((SELECT version FROM upserted_cover), 0),
    EXISTS(SELECT 1 FROM current_project)`

// UpsertCover inserts or replaces one reviewed cover only while the submitted
// project revision remains current. Both revisions change in one statement.
func (writer *postgresAdminArchitectureProjectWriter) UpsertCover(
	ctx context.Context,
	projectID int64,
	expectedProjectVersion int64,
	input adminArchitectureProjectCoverWriteInput,
) (adminArchitectureProjectCoverWriteResult, error) {
	if ctx == nil || projectID <= 0 || expectedProjectVersion <= 0 ||
		expectedProjectVersion == math.MaxInt64 ||
		!isValidAdminArchitectureProjectCoverWriteInput(input) {
		return adminArchitectureProjectCoverWriteResult{},
			errAdminArchitectureProjectWriteInvalid
	}
	if writer == nil || writer.queryRow == nil {
		return adminArchitectureProjectCoverWriteResult{},
			errAdminArchitectureProjectWriteFailed
	}

	row := writer.queryRow(
		ctx,
		upsertAdminArchitectureProjectCoverSQL,
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
		return adminArchitectureProjectCoverWriteResult{},
			errAdminArchitectureProjectWriteFailed
	}

	var result adminArchitectureProjectCoverWriteResult
	var projectExists bool
	if err := row.Scan(
		&result.ProjectID,
		&result.ProjectVersion,
		&result.CoverVersion,
		&projectExists,
	); err != nil {
		return adminArchitectureProjectCoverWriteResult{},
			errAdminArchitectureProjectWriteFailed
	}
	if result == (adminArchitectureProjectCoverWriteResult{}) {
		if projectExists {
			return adminArchitectureProjectCoverWriteResult{},
				errAdminArchitectureProjectWriteConflict
		}
		return adminArchitectureProjectCoverWriteResult{},
			errAdminArchitectureProjectNotFound
	}
	if !projectExists || result.ProjectID != projectID ||
		result.ProjectVersion != expectedProjectVersion+1 ||
		result.CoverVersion <= 0 {
		return adminArchitectureProjectCoverWriteResult{},
			errAdminArchitectureProjectWriteFailed
	}

	return result, nil
}

// isValidAdminArchitectureProjectCoverWriteInput proves every stored image
// fact matches its encoded bytes and all reviewed text satisfies migration 8.
func isValidAdminArchitectureProjectCoverWriteInput(
	input adminArchitectureProjectCoverWriteInput,
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
