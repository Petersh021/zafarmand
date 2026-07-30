package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
)

// application holds dependencies shared by all HTTP handlers.
//
// Keeping these values on an application instance avoids mutable global state
// and makes it straightforward for tests to create an isolated application.
type application struct {
	// templates maps a page filename, such as "home.html", to the parsed
	// template set containing that page and the shared base layout.
	templates map[string]*template.Template
}

// homeHeroData describes the content and media needed by the homepage hero.
//
// This view model is intentionally about template needs rather than database
// storage. A future persistence model can be translated into this stable shape.
type homeHeroData struct {
	// StudioName is the primary identity displayed as the hero heading.
	StudioName string
	// Descriptor briefly states the studio's role beneath its name.
	Descriptor string
	// ImageURL is the browser-accessible path to the hero image.
	ImageURL string
	// ImageAlt describes meaningful image content for visitors who cannot see it.
	ImageAlt string
	// ImageWidth is the source image width used to reserve layout space.
	ImageWidth int
	// ImageHeight is the source image height used to reserve layout space.
	ImageHeight int
}

// pageData is the common top-level value passed to every page template.
//
// A pointer is used for HomeHero because only the homepage has hero data. A nil
// pointer lets other templates omit that optional section naturally with
// html/template's conditional actions.
type pageData struct {
	// Title becomes the page-specific portion of the browser document title.
	Title string
	// CurrentPath identifies the active navigation destination.
	CurrentPath string
	// HomeHero contains homepage-only content, or nil for other pages.
	HomeHero *homeHeroData
}

// newApplication creates a ready-to-serve application and its shared template
// cache.
//
// It returns an error instead of terminating the process so the caller decides
// how initialization failures should be handled. This also makes construction
// reusable from both main and tests.
func newApplication() (*application, error) {
	templateCache, err := newTemplateCache()
	if err != nil {
		return nil, err
	}

	app := &application{
		templates: templateCache,
	}

	return app, nil
}

// newTemplateCache parses every page template together with the shared base
// layout and indexes the resulting sets by page filename.
//
// Parsing pages into separate sets prevents identically named blocks such as
// "content" from overwriting one another. The returned error wraps filesystem
// and parsing failures with context while preserving the original cause.
func newTemplateCache() (
	map[string]*template.Template,
	error,
) {
	cache := make(map[string]*template.Template)

	// Glob discovers page files without requiring a second hard-coded list that
	// could drift out of sync as pages are added.
	pages, err := filepath.Glob(
		"./templates/pages/*.html",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"find page templates: %w",
			err,
		)
	}

	// An empty match is not an error for filepath.Glob, but it would leave the
	// application unable to render anything, so treat it as startup failure.
	if len(pages) == 0 {
		return nil, fmt.Errorf(
			"no page templates found",
		)
	}

	for _, page := range pages {
		// Base returns a platform-safe filename for use as the cache key.
		pageName := filepath.Base(page)

		templateSet, err := template.ParseFiles(
			"./templates/base.html",
			page,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"parse template %s: %w",
				pageName,
				err,
			)
		}

		cache[pageName] = templateSet
	}

	return cache, nil
}

// render executes one cached page through the shared "base" template and writes
// the completed HTML response.
//
// status is the desired HTTP status code, pageName selects the cached template
// set, and data is the view model exposed to template actions. Rendering into a
// buffer first is important: if template execution fails, the handler can still
// send a clean 500 response instead of a partially written HTML document.
func (app *application) render(
	w http.ResponseWriter,
	status int,
	pageName string,
	data pageData,
) {
	templateSet, exists := app.templates[pageName]
	if !exists {
		// Missing templates are server configuration errors. Log the detailed
		// name for developers while returning a generic message to visitors.
		log.Printf(
			"template %q does not exist",
			pageName,
		)

		http.Error(
			w,
			"internal server error",
			http.StatusInternalServerError,
		)

		return
	}

	var buffer bytes.Buffer

	// ExecuteTemplate starts at the named base layout; the selected page's
	// definitions fill the layout's content and optional style blocks.
	err := templateSet.ExecuteTemplate(
		&buffer,
		"base",
		data,
	)
	if err != nil {
		log.Printf(
			"could not execute template %q: %v",
			pageName,
			err,
		)

		http.Error(
			w,
			"internal server error",
			http.StatusInternalServerError,
		)

		return
	}

	// Set headers before WriteHeader because the first response write commits
	// the status and headers to the client.
	w.Header().Set(
		"Content-Type",
		"text/html; charset=utf-8",
	)

	w.WriteHeader(status)

	// At this point the status may already have reached the client, so a write
	// failure can only be logged; a second HTTP error response would be invalid.
	if _, err := buffer.WriteTo(w); err != nil {
		log.Printf(
			"could not write response: %v",
			err,
		)
	}
}
