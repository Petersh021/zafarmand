package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// productDescriptionMaximumLength bounds the optional long-form public copy
	// stored with one Product. The value counts Unicode characters, not bytes.
	productDescriptionMaximumLength = 6000
	// productMaterialMaximumLength bounds the optional reviewed material fact.
	productMaterialMaximumLength = 500
	// productDimensionsMaximumLength bounds the optional reviewed dimensions fact.
	productDimensionsMaximumLength = 500
	// reviewedCoverMaximumBytes prevents one reviewed image from consuming
	// unbounded request, memory, database, or response resources. Product and
	// Interior cover workflows intentionally share this mechanical boundary.
	reviewedCoverMaximumBytes = 8 * 1024 * 1024
	// reviewedCoverMaximumDimension rejects implausibly wide or tall decoded
	// images before a full pixel allocation is accepted.
	reviewedCoverMaximumDimension = 10000
	// reviewedCoverMaximumPixels bounds decoded memory independently of compressed
	// file size. A 6000 by 4000 photograph remains within this limit.
	reviewedCoverMaximumPixels = 25_000_000
	// reviewedCoverAltTextMaximumLength keeps meaningful alternative text concise.
	reviewedCoverAltTextMaximumLength = 300
	// reviewedCoverCaptionMaximumLength bounds optional visible cover copy.
	reviewedCoverCaptionMaximumLength = 500
	// reviewedCoverJPEGContentType is derived from a successful JPEG decode rather
	// than trusting the browser-supplied multipart media type.
	reviewedCoverJPEGContentType = "image/jpeg"
	// reviewedCoverPNGContentType is the equivalent trusted PNG response type.
	reviewedCoverPNGContentType = "image/png"
	// reviewedCoverJPEGQuality is the explicit quality used when uploaded pixels
	// are re-encoded without ancillary camera or location metadata.
	reviewedCoverJPEGQuality = 90

	// Product aliases retain the Stage 21 vocabulary in Product HTTP,
	// repository, form, and test code while their mechanics use the shared
	// reviewed-cover implementation introduced for Stage 22.
	productCoverMaximumBytes         = reviewedCoverMaximumBytes
	productCoverMaximumDimension     = reviewedCoverMaximumDimension
	productCoverAltTextMaximumLength = reviewedCoverAltTextMaximumLength
	productCoverCaptionMaximumLength = reviewedCoverCaptionMaximumLength
	productCoverJPEGContentType      = reviewedCoverJPEGContentType
	productCoverPNGContentType       = reviewedCoverPNGContentType
)

// Reviewed-cover errors and Product-facing categories are fixed,
// credential-free values. They never wrap image bytes, administrator text,
// SQL, or driver diagnostics.
var (
	// errReviewedCoverImageInvalid is the shared internal result for unsafe image
	// bytes. Domain HTTP layers translate it into their own fixed form errors.
	errReviewedCoverImageInvalid = errors.New("reviewed cover image is invalid")
	// errProductCoverNotFound represents a missing cover, a hidden Product, or a
	// stale cover revision without revealing which condition occurred publicly.
	errProductCoverNotFound = errors.New("product cover not found")
	// errProductCoverReadFailed collapses database and stored-media contract
	// failures into one safe service category.
	errProductCoverReadFailed = errors.New("product cover read failed")
	// errProductCoverImageInvalid identifies bytes that are empty, unsupported,
	// corrupt, oversized, or unsafe to decode within the Stage 21 limits.
	errProductCoverImageInvalid = errReviewedCoverImageInvalid
)

// productCoverMetadata is the small, binary-free projection safe to join into
// Product list and detail queries. A nil pointer means that no cover exists.
type productCoverMetadata struct {
	// Version changes only when cover bytes or review metadata are replaced.
	Version int64
	// Width is the decoded image width in pixels.
	Width int
	// Height is the decoded image height in pixels.
	Height int
	// AltText is the required meaningful image alternative.
	AltText string
	// Caption is optional visible editorial copy.
	Caption string
}

// reviewedCoverAssetMetadata is the complete binary-free projection needed to
// validate conditional media responses. Repositories load it before bytea so
// matching validators and HEAD requests never copy an image into Go memory.
type reviewedCoverAssetMetadata struct {
	// OwnerID is the positive identity selected through the public owner join.
	OwnerID int64
	// Version is the exact current revision encoded in the request path.
	Version int64
	// ContentType is the decoder-derived JPEG or PNG response media type.
	ContentType string
	// ByteSize is the bounded encoded representation length.
	ByteSize int
	// Width and Height are the decoder-derived image dimensions.
	Width  int
	Height int
	// SHA256 supplies the strong validator for the stored representation.
	SHA256 [sha256.Size]byte
	// AltText and Caption retain the reviewed record invariants even though they
	// are not written into a binary HTTP response.
	AltText string
	Caption string
	// CreatedAt and UpdatedAt prove replacement time cannot predate creation.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// productCoverAsset contains one complete validated image response record. It
// crosses only repository-to-media-handler boundaries, never an HTML template.
type productCoverAsset struct {
	// ProductID is the positive owning Product identity.
	ProductID int64
	// Version is the public-path revision for these exact bytes.
	Version int64
	// ContentType is a trusted JPEG or PNG media type.
	ContentType string
	// Content contains the complete bounded encoded image.
	Content []byte
	// ByteSize duplicates len(Content) as a database integrity assertion.
	ByteSize int
	// Width is the decoded pixel width.
	Width int
	// Height is the decoded pixel height.
	Height int
	// SHA256 verifies the exact stored bytes and supplies a strong ETag.
	SHA256 [sha256.Size]byte
	// AltText is carried for repository contract validation, not image responses.
	AltText string
	// Caption is the optional reviewed cover caption.
	Caption string
	// CreatedAt is the database creation timestamp for the cover record.
	CreatedAt time.Time
	// UpdatedAt is the database replacement timestamp for this revision.
	UpdatedAt time.Time
}

// responseMetadata returns the Product asset's binary-free conditional-response
// projection so the two repository phases can be compared exactly.
func (asset productCoverAsset) responseMetadata() reviewedCoverAssetMetadata {
	return reviewedCoverAssetMetadata{
		OwnerID:     asset.ProductID,
		Version:     asset.Version,
		ContentType: asset.ContentType,
		ByteSize:    asset.ByteSize,
		Width:       asset.Width,
		Height:      asset.Height,
		SHA256:      asset.SHA256,
		AltText:     asset.AltText,
		Caption:     asset.Caption,
		CreatedAt:   asset.CreatedAt,
		UpdatedAt:   asset.UpdatedAt,
	}
}

// reviewedCoverInspection contains trusted facts derived from encoded bytes.
// These facts, rather than browser-claimed headers, populate each
// discipline-specific cover write input.
type reviewedCoverInspection struct {
	// ContentType is derived from the decoder format.
	ContentType string
	// Width is the decoded pixel width.
	Width int
	// Height is the decoded pixel height.
	Height int
	// SHA256 is calculated over the complete encoded file.
	SHA256 [sha256.Size]byte
}

// inspectedProductCover preserves the Product-facing Stage 21 type name while
// making its implementation the shared reviewed-cover inspection value.
type inspectedProductCover = reviewedCoverInspection

// productCoverRowScanner is the smallest database/sql surface needed by both
// public and protected cover reads.
type productCoverRowScanner interface {
	// Scan copies the fixed twelve-column cover projection into destinations.
	Scan(...any) error
}

// reviewedCoverMetadataRowScanner is the one-method SQL seam for the shared
// eleven-column binary-free media projection.
type reviewedCoverMetadataRowScanner interface {
	// Scan copies metadata without accepting a destination for encoded content.
	Scan(...any) error
}

// isValidOptionalEditorialText accepts an empty value or one already-trimmed,
// valid UTF-8 string within the supplied Unicode-character limit. NUL is
// rejected because PostgreSQL text cannot store it; intended newlines and tabs
// remain available to long-form editorial copy in either managed discipline.
func isValidOptionalEditorialText(value string, maximumLength int) bool {
	return maximumLength > 0 && utf8.ValidString(value) &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) == value &&
		utf8.RuneCountInString(value) <= maximumLength
}

// isValidOptionalProductText preserves the Product-specific call site while
// delegating its mechanical string boundary to the shared editorial validator.
func isValidOptionalProductText(value string, maximumLength int) bool {
	return isValidOptionalEditorialText(value, maximumLength)
}

// isValidReviewedCoverMetadata verifies the shared binary-free invariants
// before either discipline constructs an image path or element.
func isValidReviewedCoverMetadata(
	version int64,
	width int,
	height int,
	altText string,
	caption string,
) bool {
	return version > 0 &&
		isValidReviewedCoverDimensions(width, height) &&
		isValidRequiredReviewedCoverText(
			altText,
			reviewedCoverAltTextMaximumLength,
		) &&
		isValidOptionalReviewedCoverText(
			caption,
			reviewedCoverCaptionMaximumLength,
		)
}

// isValidProductCoverMetadata verifies every binary-free cover invariant before
// a path or image element is rendered.
func isValidProductCoverMetadata(metadata productCoverMetadata) bool {
	return isValidReviewedCoverMetadata(
		metadata.Version,
		metadata.Width,
		metadata.Height,
		metadata.AltText,
		metadata.Caption,
	)
}

// isValidRequiredReviewedCoverText applies UTF-8, trimming, single-line, and
// nonempty rules to administrator-authored alternative text.
func isValidRequiredReviewedCoverText(value string, maximumLength int) bool {
	return value != "" && isValidOptionalReviewedCoverText(value, maximumLength)
}

// isValidOptionalReviewedCoverText adds a single-line control-character rule
// to the shared optional editorial-text boundary.
func isValidOptionalReviewedCoverText(value string, maximumLength int) bool {
	if !isValidOptionalEditorialText(value, maximumLength) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) ||
			unicode.In(character, unicode.Zl, unicode.Zp) {
			return false
		}
	}

	return true
}

// isValidRequiredProductCoverText keeps existing Product call sites explicit
// while sharing the discipline-neutral cover-text mechanics.
func isValidRequiredProductCoverText(value string, maximumLength int) bool {
	return isValidRequiredReviewedCoverText(value, maximumLength)
}

// isValidOptionalProductCoverText keeps existing Product call sites explicit
// while sharing the discipline-neutral cover-text mechanics.
func isValidOptionalProductCoverText(value string, maximumLength int) bool {
	return isValidOptionalReviewedCoverText(value, maximumLength)
}

// isValidReviewedCoverDimensions enforces both per-axis and total decoded-
// pixel limits without allowing integer multiplication to overflow.
func isValidReviewedCoverDimensions(width int, height int) bool {
	if width <= 0 || height <= 0 ||
		width > reviewedCoverMaximumDimension ||
		height > reviewedCoverMaximumDimension {
		return false
	}

	return int64(width)*int64(height) <= reviewedCoverMaximumPixels
}

// isValidProductCoverDimensions preserves the Product vocabulary around the
// shared reviewed-image dimension boundary.
func isValidProductCoverDimensions(width int, height int) bool {
	return isValidReviewedCoverDimensions(width, height)
}

// inspectReviewedCover validates a complete JPEG or PNG and returns only facts
// derived by the standard-library decoder. fullDecode is true for uploads so a
// truncated file cannot be persisted, and false for the cheaper read recheck.
func inspectReviewedCover(
	content []byte,
	fullDecode bool,
) (reviewedCoverInspection, error) {
	if len(content) == 0 || len(content) > reviewedCoverMaximumBytes {
		return reviewedCoverInspection{}, errReviewedCoverImageInvalid
	}

	configuration, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || !isValidReviewedCoverDimensions(
		configuration.Width,
		configuration.Height,
	) {
		return reviewedCoverInspection{}, errReviewedCoverImageInvalid
	}

	contentType := ""
	switch format {
	case "jpeg":
		contentType = reviewedCoverJPEGContentType
	case "png":
		contentType = reviewedCoverPNGContentType
	default:
		return reviewedCoverInspection{}, errReviewedCoverImageInvalid
	}

	if fullDecode {
		decoded, decodedFormat, decodeErr := image.Decode(bytes.NewReader(content))
		if decodeErr != nil || decodedFormat != format {
			return reviewedCoverInspection{}, errReviewedCoverImageInvalid
		}
		bounds := decoded.Bounds()
		if bounds.Dx() != configuration.Width ||
			bounds.Dy() != configuration.Height {
			return reviewedCoverInspection{}, errReviewedCoverImageInvalid
		}
	}

	return reviewedCoverInspection{
		ContentType: contentType,
		Width:       configuration.Width,
		Height:      configuration.Height,
		SHA256:      sha256.Sum256(content),
	}, nil
}

// inspectProductCover retains the Product-specific API while delegating to the
// shared Stage 22 reviewed-cover implementation.
func inspectProductCover(
	content []byte,
	fullDecode bool,
) (inspectedProductCover, error) {
	return inspectReviewedCover(content, fullDecode)
}

// normalizeReviewedCover decodes one complete supported upload and re-encodes
// only its pixels. Re-encoding strips EXIF, XMP, PNG text, hidden thumbnails,
// and other ancillary metadata before public persistence. JPEG orientation must
// therefore already be baked into the selected pixels by the administrator.
func normalizeReviewedCover(
	content []byte,
) ([]byte, reviewedCoverInspection, error) {
	if len(content) == 0 || len(content) > reviewedCoverMaximumBytes {
		return nil, reviewedCoverInspection{}, errReviewedCoverImageInvalid
	}

	configuration, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || !isValidReviewedCoverDimensions(
		configuration.Width,
		configuration.Height,
	) {
		return nil, reviewedCoverInspection{}, errReviewedCoverImageInvalid
	}
	if format != "jpeg" && format != "png" {
		return nil, reviewedCoverInspection{}, errReviewedCoverImageInvalid
	}

	decoded, decodedFormat, err := image.Decode(bytes.NewReader(content))
	if err != nil || decodedFormat != format {
		return nil, reviewedCoverInspection{}, errReviewedCoverImageInvalid
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != configuration.Width || bounds.Dy() != configuration.Height {
		return nil, reviewedCoverInspection{}, errReviewedCoverImageInvalid
	}

	var normalized bytes.Buffer
	contentType := reviewedCoverPNGContentType
	if format == "jpeg" {
		contentType = reviewedCoverJPEGContentType
		err = jpeg.Encode(
			&normalized,
			decoded,
			&jpeg.Options{Quality: reviewedCoverJPEGQuality},
		)
	} else {
		encoder := png.Encoder{CompressionLevel: png.DefaultCompression}
		err = encoder.Encode(&normalized, decoded)
	}
	if err != nil || normalized.Len() == 0 ||
		normalized.Len() > reviewedCoverMaximumBytes {
		return nil, reviewedCoverInspection{}, errReviewedCoverImageInvalid
	}

	normalizedContent := append([]byte(nil), normalized.Bytes()...)
	return normalizedContent, reviewedCoverInspection{
		ContentType: contentType,
		Width:       configuration.Width,
		Height:      configuration.Height,
		SHA256:      sha256.Sum256(normalizedContent),
	}, nil
}

// normalizeProductCover retains the Product-specific API while delegating to
// the shared metadata-stripping reviewed-cover implementation.
func normalizeProductCover(
	content []byte,
) ([]byte, inspectedProductCover, error) {
	return normalizeReviewedCover(content)
}

// isValidReviewedCoverAsset rechecks database metadata, encoded image
// configuration, digest, timestamps, and reviewed text before either domain
// emits a response. Full pixel decoding occurs once at the upload boundary.
func isValidReviewedCoverAsset(
	ownerID int64,
	version int64,
	contentType string,
	content []byte,
	byteSize int,
	width int,
	height int,
	digest [sha256.Size]byte,
	altText string,
	caption string,
	createdAt time.Time,
	updatedAt time.Time,
) bool {
	metadata := reviewedCoverAssetMetadata{
		OwnerID:     ownerID,
		Version:     version,
		ContentType: contentType,
		ByteSize:    byteSize,
		Width:       width,
		Height:      height,
		SHA256:      digest,
		AltText:     altText,
		Caption:     caption,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
	if !isValidReviewedCoverAssetMetadata(metadata) ||
		byteSize != len(content) {
		return false
	}

	inspection, err := inspectReviewedCover(content, false)
	if err != nil {
		return false
	}

	return contentType == inspection.ContentType &&
		width == inspection.Width &&
		height == inspection.Height &&
		digest == inspection.SHA256
}

// isValidReviewedCoverAssetMetadata verifies every persisted response fact
// that can be checked without loading encoded content. The content lookup later
// rechecks the digest, size, type, and dimensions against the actual bytes.
func isValidReviewedCoverAssetMetadata(
	metadata reviewedCoverAssetMetadata,
) bool {
	validContentType := metadata.ContentType == reviewedCoverJPEGContentType ||
		metadata.ContentType == reviewedCoverPNGContentType

	return metadata.OwnerID > 0 &&
		metadata.Version > 0 &&
		validContentType &&
		metadata.ByteSize > 0 &&
		metadata.ByteSize <= reviewedCoverMaximumBytes &&
		isValidReviewedCoverDimensions(metadata.Width, metadata.Height) &&
		isValidRequiredReviewedCoverText(
			metadata.AltText,
			reviewedCoverAltTextMaximumLength,
		) &&
		isValidOptionalReviewedCoverText(
			metadata.Caption,
			reviewedCoverCaptionMaximumLength,
		) &&
		!metadata.CreatedAt.IsZero() &&
		!metadata.UpdatedAt.IsZero() &&
		!metadata.UpdatedAt.Before(metadata.CreatedAt)
}

// scanReviewedCoverAssetMetadata reads the shared public metadata projection,
// converts its digest to fixed-size storage, and rejects malformed records.
func scanReviewedCoverAssetMetadata(
	scanner reviewedCoverMetadataRowScanner,
) (reviewedCoverAssetMetadata, error) {
	if scanner == nil {
		return reviewedCoverAssetMetadata{}, errProductCoverReadFailed
	}

	var metadata reviewedCoverAssetMetadata
	var digest []byte
	if err := scanner.Scan(
		&metadata.OwnerID,
		&metadata.Version,
		&metadata.ContentType,
		&metadata.ByteSize,
		&metadata.Width,
		&metadata.Height,
		&digest,
		&metadata.AltText,
		&metadata.Caption,
		&metadata.CreatedAt,
		&metadata.UpdatedAt,
	); err != nil {
		return reviewedCoverAssetMetadata{}, err
	}
	if len(digest) != sha256.Size {
		return reviewedCoverAssetMetadata{}, errProductCoverReadFailed
	}
	copy(metadata.SHA256[:], digest)
	if !isValidReviewedCoverAssetMetadata(metadata) {
		return reviewedCoverAssetMetadata{}, errProductCoverReadFailed
	}

	return metadata, nil
}

// isValidProductCoverAsset applies the shared reviewed-cover record boundary
// to a Product-owned asset without exposing that owner model to Interior code.
func isValidProductCoverAsset(asset productCoverAsset) bool {
	return isValidReviewedCoverAsset(
		asset.ProductID,
		asset.Version,
		asset.ContentType,
		asset.Content,
		asset.ByteSize,
		asset.Width,
		asset.Height,
		asset.SHA256,
		asset.AltText,
		asset.Caption,
		asset.CreatedAt,
		asset.UpdatedAt,
	)
}

// scanProductCoverAsset reads one binary cover projection, copies its digest
// into a fixed-size value, and validates the complete record before returning.
func scanProductCoverAsset(
	scanner productCoverRowScanner,
) (productCoverAsset, error) {
	if scanner == nil {
		return productCoverAsset{}, errProductCoverReadFailed
	}

	var asset productCoverAsset
	var digest []byte
	err := scanner.Scan(
		&asset.ProductID,
		&asset.Version,
		&asset.ContentType,
		&asset.Content,
		&asset.ByteSize,
		&asset.Width,
		&asset.Height,
		&digest,
		&asset.AltText,
		&asset.Caption,
		&asset.CreatedAt,
		&asset.UpdatedAt,
	)
	if err != nil {
		return productCoverAsset{}, err
	}
	if len(digest) != sha256.Size {
		return productCoverAsset{}, errProductCoverReadFailed
	}
	copy(asset.SHA256[:], digest)
	if !isValidProductCoverAsset(asset) {
		return productCoverAsset{}, errProductCoverReadFailed
	}

	return cloneProductCoverAsset(asset), nil
}

// cloneProductCoverAsset isolates mutable byte storage before a repository or
// test double returns an asset across its interface boundary.
func cloneProductCoverAsset(asset productCoverAsset) productCoverAsset {
	asset.Content = append([]byte(nil), asset.Content...)

	return asset
}
