package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"reflect"
	"strings"
	"testing"
	"time"
)

// testProductCoverPNG returns a small deterministic image whose real decoder
// facts can be used across repository and HTTP tests.
func testProductCoverPNG(t *testing.T) []byte {
	t.Helper()

	canvas := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			canvas.SetRGBA(
				x,
				y,
				color.RGBA{
					R: uint8(30 + x*20),
					G: uint8(40 + y*30),
					B: 90,
					A: 255,
				},
			)
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatalf("encode deterministic cover PNG: %v", err)
	}

	return encoded.Bytes()
}

// testProductCoverJPEG returns a second supported format for decoder tests.
func testProductCoverJPEG(t *testing.T) []byte {
	t.Helper()

	canvas := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			canvas.Set(x, y, color.RGBA{R: 120, G: uint8(70 + x*10), B: 35, A: 255})
		}
	}

	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode deterministic cover JPEG: %v", err)
	}

	return encoded.Bytes()
}

// validTestProductCoverAsset creates one complete internally consistent media
// record with timestamps and a copied byte slice.
func validTestProductCoverAsset(
	t *testing.T,
	productID int64,
	version int64,
) productCoverAsset {
	t.Helper()

	content := testProductCoverPNG(t)
	inspection, err := inspectProductCover(content, true)
	if err != nil {
		t.Fatalf("inspect deterministic cover: %v", err)
	}
	createdAt := time.Date(2034, time.March, 4, 5, 6, 7, 0, time.UTC)

	return productCoverAsset{
		ProductID:   productID,
		Version:     version,
		ContentType: inspection.ContentType,
		Content:     append([]byte(nil), content...),
		ByteSize:    len(content),
		Width:       inspection.Width,
		Height:      inspection.Height,
		SHA256:      inspection.SHA256,
		AltText:     "A compact geometric Product study on a neutral surface",
		Caption:     "Synthetic Stage 21 cover fixture.",
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt.Add(time.Minute),
	}
}

// productCoverRowStub supplies one fixed twelve-column binary projection to
// either public or protected repository tests.
type productCoverRowStub struct {
	// asset is copied into destinations on success.
	asset productCoverAsset
	// scanError simulates no rows or a dependency failure.
	scanError error
}

// Scan implements the shared productCoverRowScanner contract.
func (row *productCoverRowStub) Scan(destinations ...any) error {
	if row.scanError != nil {
		return row.scanError
	}
	if len(destinations) != 12 {
		return errors.New("cover row expected twelve destinations")
	}

	productID, productIDOK := destinations[0].(*int64)
	version, versionOK := destinations[1].(*int64)
	contentType, contentTypeOK := destinations[2].(*string)
	content, contentOK := destinations[3].(*[]byte)
	byteSize, byteSizeOK := destinations[4].(*int)
	width, widthOK := destinations[5].(*int)
	height, heightOK := destinations[6].(*int)
	digest, digestOK := destinations[7].(*[]byte)
	altText, altTextOK := destinations[8].(*string)
	caption, captionOK := destinations[9].(*string)
	createdAt, createdAtOK := destinations[10].(*time.Time)
	updatedAt, updatedAtOK := destinations[11].(*time.Time)
	if !productIDOK || !versionOK || !contentTypeOK || !contentOK ||
		!byteSizeOK || !widthOK || !heightOK || !digestOK || !altTextOK ||
		!captionOK || !createdAtOK || !updatedAtOK {
		return errors.New("cover row received unexpected destinations")
	}

	*productID = row.asset.ProductID
	*version = row.asset.Version
	*contentType = row.asset.ContentType
	*content = append([]byte(nil), row.asset.Content...)
	*byteSize = row.asset.ByteSize
	*width = row.asset.Width
	*height = row.asset.Height
	*digest = append([]byte(nil), row.asset.SHA256[:]...)
	*altText = row.asset.AltText
	*caption = row.asset.Caption
	*createdAt = row.asset.CreatedAt
	*updatedAt = row.asset.UpdatedAt

	return nil
}

// TestInspectProductCoverAcceptsSupportedImages verifies trusted media type,
// decoded dimensions, digest, and both full and metadata-only read paths.
func TestInspectProductCoverAcceptsSupportedImages(t *testing.T) {
	tests := []struct {
		// name identifies the supported codec.
		name string
		// content provides its deterministic encoded bytes.
		content []byte
		// contentType is the derived response type.
		contentType string
		// width and height are decoder-owned facts.
		width  int
		height int
	}{
		{name: "PNG", content: testProductCoverPNG(t), contentType: productCoverPNGContentType, width: 4, height: 3},
		{name: "JPEG", content: testProductCoverJPEG(t), contentType: productCoverJPEGContentType, width: 3, height: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			full, err := inspectProductCover(test.content, true)
			if err != nil {
				t.Fatalf("inspect complete image: %v", err)
			}
			metadataOnly, err := inspectProductCover(test.content, false)
			if err != nil {
				t.Fatalf("inspect image configuration: %v", err)
			}
			if full != metadataOnly || full.ContentType != test.contentType ||
				full.Width != test.width || full.Height != test.height ||
				full.SHA256 != sha256.Sum256(test.content) {
				t.Errorf("inspection: full=%#v metadata=%#v", full, metadataOnly)
			}
		})
	}
}

// TestNormalizeProductCoverStripsJPEGMetadata inserts a synthetic APP1 payload
// into an otherwise valid JPEG and proves public persistence receives only a
// newly encoded pixel representation.
func TestNormalizeProductCoverStripsJPEGMetadata(t *testing.T) {
	original := testProductCoverJPEG(t)
	metadata := []byte("Exif\x00\x00synthetic-gps-and-camera-metadata")
	segment := make([]byte, 4+len(metadata))
	segment[0], segment[1] = 0xff, 0xe1
	binary.BigEndian.PutUint16(segment[2:4], uint16(len(metadata)+2))
	copy(segment[4:], metadata)
	withMetadata := append([]byte(nil), original[:2]...)
	withMetadata = append(withMetadata, segment...)
	withMetadata = append(withMetadata, original[2:]...)

	normalized, inspection, err := normalizeProductCover(withMetadata)
	if err != nil {
		t.Fatalf("normalize metadata-bearing JPEG: %v", err)
	}
	if inspection.ContentType != productCoverJPEGContentType ||
		inspection.Width != 3 || inspection.Height != 2 ||
		inspection.SHA256 != sha256.Sum256(normalized) {
		t.Errorf("normalized JPEG inspection: %#v", inspection)
	}
	if bytes.Contains(normalized, metadata) || bytes.Equal(normalized, withMetadata) {
		t.Error("normalized JPEG retained original ancillary metadata or bytes")
	}
	if _, _, err := image.Decode(bytes.NewReader(normalized)); err != nil {
		t.Fatalf("decode normalized JPEG pixels: %v", err)
	}
}

// TestInspectProductCoverRejectsUnsafeBytes covers empty, unsupported, corrupt,
// truncated, and over-limit input without persisting any data.
func TestInspectProductCoverRejectsUnsafeBytes(t *testing.T) {
	validPNG := testProductCoverPNG(t)
	tests := []struct {
		// name identifies the rejected boundary.
		name string
		// content is the exact untrusted byte sequence.
		content []byte
	}{
		{name: "empty"},
		{name: "unsupported", content: []byte("GIF89a-not-a-supported-cover")},
		{name: "corrupt", content: []byte("not an image")},
		{name: "truncated", content: append([]byte(nil), validPNG[:len(validPNG)-8]...)},
		{name: "over byte limit", content: make([]byte, productCoverMaximumBytes+1)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := inspectProductCover(test.content, true); !errors.Is(
				err,
				errProductCoverImageInvalid,
			) {
				t.Errorf("error: got %v, want invalid-image category", err)
			}
		})
	}
}

// TestProductCoverDimensionLimits locks the independent per-axis and total
// decoded-pixel boundaries without allocating enormous image buffers.
func TestProductCoverDimensionLimits(t *testing.T) {
	tests := []struct {
		// name identifies the exact edge under test.
		name string
		// width and height are decoder-derived pixel dimensions.
		width  int
		height int
		// want records whether the pair is safe.
		want bool
	}{
		{name: "maximum axis", width: productCoverMaximumDimension, height: 1, want: true},
		{name: "axis overflow", width: productCoverMaximumDimension + 1, height: 1},
		{name: "maximum pixels", width: 5000, height: 5000, want: true},
		{name: "pixel overflow", width: 5001, height: 5000},
		{name: "zero width", height: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isValidProductCoverDimensions(test.width, test.height); got != test.want {
				t.Errorf("dimensions %dx%d: got %t, want %t", test.width, test.height, got, test.want)
			}
		})
	}
}

// TestProductCoverReviewedTextLimits verifies required alt text and optional
// caption boundaries, including exact limits and single-line control rejection.
func TestProductCoverReviewedTextLimits(t *testing.T) {
	if isValidRequiredProductCoverText("", productCoverAltTextMaximumLength) {
		t.Error("empty required alt text was accepted")
	}
	if !isValidRequiredProductCoverText(
		strings.Repeat("a", productCoverAltTextMaximumLength),
		productCoverAltTextMaximumLength,
	) {
		t.Error("maximum-length alt text was rejected")
	}
	if isValidRequiredProductCoverText(
		strings.Repeat("a", productCoverAltTextMaximumLength+1),
		productCoverAltTextMaximumLength,
	) {
		t.Error("overlong alt text was accepted")
	}
	if isValidRequiredProductCoverText(
		"two\nlines",
		productCoverAltTextMaximumLength,
	) {
		t.Error("control character in required alt text was accepted")
	}
	if isValidOptionalProductCoverText(
		"two\nlines",
		productCoverCaptionMaximumLength,
	) {
		t.Error("control character in reviewed media text was accepted")
	}
	if isValidOptionalProductCoverText(
		"two\u2028lines",
		productCoverCaptionMaximumLength,
	) {
		t.Error("Unicode line separator in reviewed media text was accepted")
	}
	if !isValidOptionalProductCoverText(
		strings.Repeat("c", productCoverCaptionMaximumLength),
		productCoverCaptionMaximumLength,
	) {
		t.Error("maximum-length caption was rejected")
	}
	if isValidOptionalProductCoverText(
		strings.Repeat("c", productCoverCaptionMaximumLength+1),
		productCoverCaptionMaximumLength,
	) {
		t.Error("overlong caption was accepted")
	}
}

// TestProductCoverETagMatching covers the normal strong validator plus weak,
// list, wildcard, nonmatching, and malformed conditional-request forms.
func TestProductCoverETagMatching(t *testing.T) {
	const current = `"0123456789abcdef"`
	tests := []struct {
		// header is the untrusted If-None-Match field value.
		header string
		// want records whether HTTP weak comparison finds current.
		want bool
	}{
		{header: current, want: true},
		{header: `W/"0123456789abcdef"`, want: true},
		{header: `"different", W/"0123456789abcdef"`, want: true},
		{header: `*`, want: true},
		{header: `"different"`},
		{header: `W/not-quoted`},
		{header: `"unterminated`},
		{header: current + `garbage`},
	}

	for _, test := range tests {
		if got := productCoverETagMatches(test.header, current); got != test.want {
			t.Errorf("If-None-Match %q: got %t, want %t", test.header, got, test.want)
		}
	}
}

// TestProductCoverValidationRejectsMetadataDrift proves that a valid asset and
// metadata pass while altered bytes, dimensions, text, or digest fail closed.
func TestProductCoverValidationRejectsMetadataDrift(t *testing.T) {
	valid := validTestProductCoverAsset(t, 7, 2)
	if !isValidProductCoverAsset(valid) {
		t.Fatal("valid deterministic cover asset was rejected")
	}
	if !isValidProductCoverMetadata(productCoverMetadata{
		Version: valid.Version,
		Width:   valid.Width,
		Height:  valid.Height,
		AltText: valid.AltText,
		Caption: valid.Caption,
	}) {
		t.Fatal("valid deterministic cover metadata was rejected")
	}

	tests := []struct {
		// name identifies the single corrupted invariant.
		name string
		// mutate changes one copy of the valid asset.
		mutate func(*productCoverAsset)
	}{
		{name: "zero identity", mutate: func(asset *productCoverAsset) { asset.ProductID = 0 }},
		{name: "wrong byte count", mutate: func(asset *productCoverAsset) { asset.ByteSize++ }},
		{name: "changed bytes", mutate: func(asset *productCoverAsset) { asset.Content[0] ^= 0xff }},
		{name: "wrong dimensions", mutate: func(asset *productCoverAsset) { asset.Width++ }},
		{name: "untrimmed alt text", mutate: func(asset *productCoverAsset) { asset.AltText = " bad " }},
		{name: "control caption", mutate: func(asset *productCoverAsset) { asset.Caption = "bad\ncaption" }},
		{name: "timestamp regression", mutate: func(asset *productCoverAsset) { asset.UpdatedAt = asset.CreatedAt.Add(-time.Second) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			asset := cloneProductCoverAsset(valid)
			test.mutate(&asset)
			if isValidProductCoverAsset(asset) {
				t.Error("corrupted cover asset was accepted")
			}
		})
	}
}

// TestScanProductCoverAssetCopiesMutableBytes verifies exact mapping and proves
// callers cannot mutate a scanner-owned byte slice through the result.
func TestScanProductCoverAssetCopiesMutableBytes(t *testing.T) {
	expected := validTestProductCoverAsset(t, 11, 3)
	row := &productCoverRowStub{asset: expected}
	actual, err := scanProductCoverAsset(row)
	if err != nil {
		t.Fatalf("scan valid cover: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("scanned cover: got %#v, want %#v", actual, expected)
	}
	actual.Content[0] ^= 0xff
	if bytes.Equal(actual.Content, expected.Content) {
		t.Error("scanned cover shares mutable content storage")
	}
}
