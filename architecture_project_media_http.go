package main

import (
	"context"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strconv"
)

// architectureProjectCoverHandler serves one validated current cover revision
// only while its owning Architecture project remains Published. Missing
// projects, private lifecycle states, missing covers, and stale revisions share
// one public 404 boundary so the media route cannot reveal editorial state.
func (app *application) architectureProjectCoverHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	// A guessed cover URL must not leave a cacheable 404 that survives a later
	// publish action. A successful read replaces this conservative default only
	// after PostgreSQL rechecks publication and the exact requested revision.
	w.Header().Set("Cache-Control", "no-store")

	slug := r.PathValue("slug")
	version, validVersion := parseCanonicalPositiveInt64(
		r.PathValue("version"),
	)
	if !isCanonicalArchitectureProjectSlug(slug) || !validVersion {
		http.NotFound(w, r)

		return
	}
	canonicalPath := architectureProjectCoverPath(slug, version)
	if r.URL.EscapedPath() != canonicalPath {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(
			w,
			"invalid Architecture project cover request",
			http.StatusBadRequest,
		)

		return
	}
	if app == nil || app.architectureProjects == nil {
		log.Print("public architecture project cover dependency unavailable")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	readContext, cancel := context.WithTimeout(
		r.Context(),
		architectureProjectCatalogueReadTimeout,
	)
	asset, err := app.architectureProjects.FindPublishedCover(
		readContext,
		slug,
		version,
	)
	cancel()
	if errors.Is(err, errArchitectureProjectCoverNotFound) {
		http.NotFound(w, r)

		return
	}
	if err != nil ||
		!isValidArchitectureProjectCoverAsset(asset) ||
		asset.Version != version {
		log.Print("public architecture project cover read failed")
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
		// A disconnected client cannot receive a recovery response. The fixed log
		// excludes slug, image bytes, project data, and repository diagnostics.
		log.Print("public architecture project cover response write failed")
	}
}
