package main

import (
	"context"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// productCoverHandler serves one validated JPEG or PNG revision for a currently
// published Product. Draft, archived, missing, and stale revisions deliberately
// share the same public 404 response.
func (app *application) productCoverHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	// Error responses must not outlive a later publish or archive action in a
	// browser or intermediary cache. A successful validated asset replaces this
	// default with the explicit ETag revalidation policy below.
	w.Header().Set("Cache-Control", "no-store")

	slug := r.PathValue("slug")
	version, validVersion := parseCanonicalPositiveInt64(
		r.PathValue("version"),
	)
	if !isCanonicalProductSlug(slug) || !validVersion {
		http.NotFound(w, r)

		return
	}
	canonicalPath := productCoverPath(slug, version)
	if r.URL.EscapedPath() != canonicalPath {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Product cover request", http.StatusBadRequest)

		return
	}
	if app == nil || app.products == nil {
		log.Print("public product cover dependency unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	readContext, cancel := context.WithTimeout(
		r.Context(),
		productCatalogueReadTimeout,
	)
	asset, err := app.products.FindPublishedCover(
		readContext,
		slug,
		version,
	)
	cancel()
	if errors.Is(err, errProductCoverNotFound) {
		http.NotFound(w, r)

		return
	}
	if err != nil || !isValidProductCoverAsset(asset) ||
		asset.Version != version {
		log.Print("public product cover read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	etag := `"` + hex.EncodeToString(asset.SHA256[:]) + `"`
	header := w.Header()
	// The revision-specific ETag avoids retransmitting unchanged bytes, while
	// mandatory revalidation lets an archive action make the route private on
	// the browser's next request. A long immutable lifetime would defeat that
	// publication boundary with a previously cached response.
	header.Set("Cache-Control", "public, max-age=0, must-revalidate")
	header.Set("Content-Type", asset.ContentType)
	header.Set("Content-Length", strconv.Itoa(asset.ByteSize))
	header.Set("ETag", etag)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	if productCoverETagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)

		return
	}

	if _, err := w.Write(asset.Content); err != nil {
		// The connection may already be gone, so only a fixed diagnostic is useful.
		// Never log the slug, Product identity, image bytes, or repository error.
		log.Print("public product cover response write failed")
	}
}

// reviewedCoverETagMatches applies HTTP's weak comparison used by If-None-Match
// on GET/HEAD. Product and Interior media routes share these protocol mechanics
// without sharing repository identities or publication queries.
func reviewedCoverETagMatches(headerValue string, currentETag string) bool {
	remaining := strings.TrimSpace(headerValue)
	if remaining == "*" {
		return true
	}
	if remaining == "" || currentETag == "" {
		return false
	}

	for remaining != "" {
		if strings.HasPrefix(remaining, "W/") {
			remaining = remaining[2:]
		}
		if len(remaining) < 2 || remaining[0] != '"' {
			return false
		}
		closingQuote := strings.IndexByte(remaining[1:], '"')
		if closingQuote < 0 {
			return false
		}
		closingQuote++
		candidate := remaining[:closingQuote+1]
		afterCandidate := strings.TrimSpace(remaining[closingQuote+1:])
		if afterCandidate != "" && afterCandidate[0] != ',' {
			return false
		}
		if candidate == currentETag {
			return true
		}

		remaining = afterCandidate
		if remaining == "" {
			return false
		}
		if remaining[0] != ',' {
			return false
		}
		remaining = strings.TrimSpace(remaining[1:])
	}

	return false
}

// productCoverETagMatches preserves the Stage 21 Product-facing helper name for
// its existing tests while delegating to the shared conditional-request logic.
func productCoverETagMatches(headerValue string, currentETag string) bool {
	return reviewedCoverETagMatches(headerValue, currentETag)
}
