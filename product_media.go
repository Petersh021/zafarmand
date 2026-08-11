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
	// productCoverMaximumBytes prevents one upload or database row from consuming
	// unbounded request, memory, or response resources.
	productCoverMaximumBytes = 8 * 1024 * 1024
	// productCoverMaximumDimension rejects implausibly wide or tall decoded images.
	productCoverMaximumDimension = 10000
	// productCoverMaximumPixels bounds decoded memory independently of compressed
	// file size. A 6000 by 4000 photograph remains within this limit.
	productCoverMaximumPixels = 25_000_000
	// productCoverAltTextMaximumLength keeps meaningful alternative text concise.
	productCoverAltTextMaximumLength = 300
	// productCoverCaptionMaximumLength bounds optional visible cover copy.
	productCoverCaptionMaximumLength = 500
	// productCoverJPEGContentType is the trusted response type derived from a
	// successfully decoded JPEG rather than from an upload header.
	productCoverJPEGContentType = "image/jpeg"
	// productCoverPNGContentType is the equivalent trusted PNG response type.
	productCoverPNGContentType = "image/png"
	// productCoverJPEGQuality is the explicit quality used when uploaded pixels
	// are re-encoded without ancillary camera or location metadata.
	productCoverJPEGQuality = 90
)

// Product-cover errors are fixed, credential-free categories. They never wrap
// image bytes, administrator text, SQL, or driver diagnostics.
var (
	// errProductCoverNotFound represents a missing cover, a hidden Product, or a
	// stale cover revision without revealing which condition occurred publicly.
	errProductCoverNotFound = errors.New("product cover not found")
	// errProductCoverReadFailed collapses database and stored-media contract
	// failures into one safe service category.
	errProductCoverReadFailed = errors.New("product cover read failed")
	// errProductCoverImageInvalid identifies bytes that are empty, unsupported,
	// corrupt, oversized, or unsafe to decode within the Stage 21 limits.
	errProductCoverImageInvalid = errors.New("product cover image is invalid")
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

// inspectedProductCover contains trusted facts derived from encoded bytes. The
// administrator writer persists only this output, never browser media headers.
type inspectedProductCover struct {
	// ContentType is derived from the decoder format.
	ContentType string
	// Width is the decoded pixel width.
	Width int
	// Height is the decoded pixel height.
	Height int
	// SHA256 is calculated over the complete encoded file.
	SHA256 [sha256.Size]byte
}

// productCoverRowScanner is the smallest database/sql surface needed by both
// public and protected cover reads.
type productCoverRowScanner interface {
	// Scan copies the fixed twelve-column cover projection into destinations.
	Scan(...any) error
}

// isValidOptionalProductText accepts an empty value or one already-trimmed,
// valid UTF-8 string within the supplied Unicode-character limit. NUL is
// rejected because PostgreSQL text cannot store it; intended newlines and tabs
// remain available to long-form editorial copy.
func isValidOptionalProductText(value string, maximumLength int) bool {
	return maximumLength > 0 && utf8.ValidString(value) &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) == value &&
		utf8.RuneCountInString(value) <= maximumLength
}

// isValidProductCoverMetadata verifies every binary-free cover invariant before
// a path or image element is rendered.
func isValidProductCoverMetadata(metadata productCoverMetadata) bool {
	return metadata.Version > 0 &&
		isValidProductCoverDimensions(metadata.Width, metadata.Height) &&
		isValidRequiredProductCoverText(
			metadata.AltText,
			productCoverAltTextMaximumLength,
		) &&
		isValidOptionalProductCoverText(
			metadata.Caption,
			productCoverCaptionMaximumLength,
		)
}

// isValidRequiredProductCoverText applies UTF-8, trimming, and nonempty rules
// to required administrator-authored media text.
func isValidRequiredProductCoverText(value string, maximumLength int) bool {
	return value != "" && isValidOptionalProductCoverText(value, maximumLength)
}

// isValidOptionalProductCoverText adds a single-line control-character rule to
// the shared optional-text boundary used by Product editorial fields.
func isValidOptionalProductCoverText(value string, maximumLength int) bool {
	if !isValidOptionalProductText(value, maximumLength) {
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

// isValidProductCoverDimensions enforces both per-axis and total decoded-pixel
// limits without allowing integer multiplication to overflow.
func isValidProductCoverDimensions(width int, height int) bool {
	if width <= 0 || height <= 0 ||
		width > productCoverMaximumDimension ||
		height > productCoverMaximumDimension {
		return false
	}

	return int64(width)*int64(height) <= productCoverMaximumPixels
}

// inspectProductCover validates a complete JPEG or PNG and returns only facts
// derived by the standard-library decoder. fullDecode is true for uploads so a
// truncated file cannot be persisted, and false for the cheaper read recheck.
func inspectProductCover(
	content []byte,
	fullDecode bool,
) (inspectedProductCover, error) {
	if len(content) == 0 || len(content) > productCoverMaximumBytes {
		return inspectedProductCover{}, errProductCoverImageInvalid
	}

	configuration, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || !isValidProductCoverDimensions(
		configuration.Width,
		configuration.Height,
	) {
		return inspectedProductCover{}, errProductCoverImageInvalid
	}

	contentType := ""
	switch format {
	case "jpeg":
		contentType = productCoverJPEGContentType
	case "png":
		contentType = productCoverPNGContentType
	default:
		return inspectedProductCover{}, errProductCoverImageInvalid
	}

	if fullDecode {
		decoded, decodedFormat, decodeErr := image.Decode(bytes.NewReader(content))
		if decodeErr != nil || decodedFormat != format {
			return inspectedProductCover{}, errProductCoverImageInvalid
		}
		bounds := decoded.Bounds()
		if bounds.Dx() != configuration.Width ||
			bounds.Dy() != configuration.Height {
			return inspectedProductCover{}, errProductCoverImageInvalid
		}
	}

	return inspectedProductCover{
		ContentType: contentType,
		Width:       configuration.Width,
		Height:      configuration.Height,
		SHA256:      sha256.Sum256(content),
	}, nil
}

// normalizeProductCover decodes one complete supported upload and re-encodes
// only its pixels. Re-encoding strips EXIF, XMP, PNG text, hidden thumbnails,
// and other ancillary metadata before public persistence. JPEG orientation must
// therefore already be baked into the selected pixels by the administrator.
func normalizeProductCover(
	content []byte,
) ([]byte, inspectedProductCover, error) {
	if len(content) == 0 || len(content) > productCoverMaximumBytes {
		return nil, inspectedProductCover{}, errProductCoverImageInvalid
	}

	configuration, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || !isValidProductCoverDimensions(
		configuration.Width,
		configuration.Height,
	) {
		return nil, inspectedProductCover{}, errProductCoverImageInvalid
	}
	if format != "jpeg" && format != "png" {
		return nil, inspectedProductCover{}, errProductCoverImageInvalid
	}

	decoded, decodedFormat, err := image.Decode(bytes.NewReader(content))
	if err != nil || decodedFormat != format {
		return nil, inspectedProductCover{}, errProductCoverImageInvalid
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != configuration.Width || bounds.Dy() != configuration.Height {
		return nil, inspectedProductCover{}, errProductCoverImageInvalid
	}

	var normalized bytes.Buffer
	contentType := productCoverPNGContentType
	if format == "jpeg" {
		contentType = productCoverJPEGContentType
		err = jpeg.Encode(
			&normalized,
			decoded,
			&jpeg.Options{Quality: productCoverJPEGQuality},
		)
	} else {
		encoder := png.Encoder{CompressionLevel: png.DefaultCompression}
		err = encoder.Encode(&normalized, decoded)
	}
	if err != nil || normalized.Len() == 0 ||
		normalized.Len() > productCoverMaximumBytes {
		return nil, inspectedProductCover{}, errProductCoverImageInvalid
	}

	normalizedContent := append([]byte(nil), normalized.Bytes()...)
	return normalizedContent, inspectedProductCover{
		ContentType: contentType,
		Width:       configuration.Width,
		Height:      configuration.Height,
		SHA256:      sha256.Sum256(normalizedContent),
	}, nil
}

// isValidProductCoverAsset rechecks database metadata, encoded image
// configuration, digest, timestamps, and reviewed text before a response. Full
// pixel decoding and metadata stripping occur once at the upload boundary.
func isValidProductCoverAsset(asset productCoverAsset) bool {
	if asset.ProductID <= 0 || asset.Version <= 0 ||
		asset.ByteSize != len(asset.Content) ||
		asset.CreatedAt.IsZero() || asset.UpdatedAt.IsZero() ||
		asset.UpdatedAt.Before(asset.CreatedAt) ||
		!isValidRequiredProductCoverText(
			asset.AltText,
			productCoverAltTextMaximumLength,
		) ||
		!isValidOptionalProductCoverText(
			asset.Caption,
			productCoverCaptionMaximumLength,
		) {
		return false
	}

	inspection, err := inspectProductCover(asset.Content, false)
	if err != nil {
		return false
	}

	return asset.ContentType == inspection.ContentType &&
		asset.Width == inspection.Width &&
		asset.Height == inspection.Height &&
		asset.SHA256 == inspection.SHA256
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
