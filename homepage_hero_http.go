package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
)

// homepageHeroPath returns the application-owned exact public path for one
// positive managed hero revision.
func homepageHeroPath(version int64) string {
	return "/homepage/hero/" + strconv.FormatInt(version, 10)
}

// homepageHeroHandler serves one exact current managed hero only while the
// singleton's publication switch remains enabled. Disabled, missing, and stale
// revisions share one public 404 response.
func (app *application) homepageHeroHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	// A guessed path must not leave a cacheable response that outlives a later
	// enable, disable, or replacement action. Success replaces this default only
	// after PostgreSQL rechecks the current publication boundary.
	w.Header().Set("Cache-Control", "no-store")

	version, validVersion := parseCanonicalPositiveInt64(
		r.PathValue("version"),
	)
	if !validVersion || r.URL.EscapedPath() != homepageHeroPath(version) {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(
			w,
			"invalid Homepage hero request",
			http.StatusBadRequest,
		)

		return
	}
	if app == nil || app.siteContent == nil {
		log.Print("public Homepage hero dependency unavailable")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	readContext, cancel := context.WithTimeout(
		r.Context(),
		siteContentReadTimeout,
	)
	defer cancel()
	metadata, err := app.siteContent.FindHomepageHeroMetadata(
		readContext,
		version,
	)
	if errors.Is(err, errHomepageHeroNotFound) {
		http.NotFound(w, r)

		return
	}
	if err != nil || !isValidReviewedCoverAssetMetadata(metadata) ||
		metadata.OwnerID != siteContentSingletonID ||
		metadata.Version != version {
		// The fixed diagnostic excludes managed copy, image bytes, digest, revision,
		// database errors, and any administrator-authored value.
		log.Print("public Homepage hero read failed")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	etag := reviewedCoverResponseETag(metadata)
	// Revalidation avoids retransmitting unchanged normalized bytes while making
	// a disable or replacement effective on the next browser request.
	if reviewedCoverETagMatches(r.Header.Get("If-None-Match"), etag) {
		setReviewedCoverResponseHeaders(w.Header(), metadata, etag)
		w.WriteHeader(http.StatusNotModified)

		return
	}
	if r.Method == http.MethodHead {
		setReviewedCoverResponseHeaders(w.Header(), metadata, etag)

		return
	}

	asset, err := app.siteContent.FindHomepageHero(readContext, version)
	if errors.Is(err, errHomepageHeroNotFound) {
		// A disable or replacement between phases must remain indistinguishable
		// from every other unavailable managed hero revision.
		http.NotFound(w, r)

		return
	}
	if err != nil || !isValidHomepageHeroAsset(asset) ||
		asset.responseMetadata() != metadata {
		log.Print("public Homepage hero read failed")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	setReviewedCoverResponseHeaders(w.Header(), metadata, etag)

	if _, err := w.Write(asset.Content); err != nil {
		// A disconnected client cannot receive a replacement response. Record only
		// the fixed event category without media or repository details.
		log.Print("public Homepage hero response write failed")
	}
}
