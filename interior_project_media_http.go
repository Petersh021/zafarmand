package main

import (
	"context"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strconv"
)

// interiorProjectCoverHandler serves one validated current cover revision only
// while its owning Interior project remains Published. Missing projects,
// private lifecycle states, missing covers, and stale revisions deliberately
// share one public 404 boundary.
func (app *application) interiorProjectCoverHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	// A guessed cover URL must not leave a cacheable 404 that survives a later
	// publish action. Successful responses replace this conservative default
	// after the repository has rechecked publication and the exact revision.
	w.Header().Set("Cache-Control", "no-store")

	slug := r.PathValue("slug")
	version, validVersion := parseCanonicalPositiveInt64(
		r.PathValue("version"),
	)
	if !isCanonicalInteriorProjectSlug(slug) || !validVersion {
		http.NotFound(w, r)

		return
	}
	canonicalPath := interiorProjectCoverPath(slug, version)
	if r.URL.EscapedPath() != canonicalPath {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(
			w,
			"invalid Interior project cover request",
			http.StatusBadRequest,
		)

		return
	}
	if app == nil || app.interiorProjects == nil {
		log.Print("public interior project cover dependency unavailable")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	readContext, cancel := context.WithTimeout(
		r.Context(),
		interiorProjectCatalogueReadTimeout,
	)
	asset, err := app.interiorProjects.FindPublishedCover(
		readContext,
		slug,
		version,
	)
	cancel()
	if errors.Is(err, errInteriorProjectCoverNotFound) {
		http.NotFound(w, r)

		return
	}
	if err != nil ||
		!isValidInteriorProjectCoverAsset(asset) ||
		asset.Version != version {
		log.Print("public interior project cover read failed")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	etag := `"` + hex.EncodeToString(asset.SHA256[:]) + `"`
	header := w.Header()
	// Revalidation avoids retransmitting unchanged bytes while ensuring an
	// Archive action hides an image on the browser's next request. A long
	// immutable cache lifetime would violate that publication boundary.
	header.Set("Cache-Control", "public, max-age=0, must-revalidate")
	header.Set("Content-Type", asset.ContentType)
	header.Set("Content-Length", strconv.Itoa(asset.ByteSize))
	header.Set("ETag", etag)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	if reviewedCoverETagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)

		return
	}

	if _, err := w.Write(asset.Content); err != nil {
		// A disconnected client cannot receive a recovery response. Keep the log
		// fixed so it never contains slug, identity, image bytes, or driver text.
		log.Print("public interior project cover response write failed")
	}
}
