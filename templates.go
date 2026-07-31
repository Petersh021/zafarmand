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
	// template set containing that page, the shared base layout, and partials.
	templates map[string]*template.Template
	// products is the ordered temporary source shared by listing and detail handlers.
	//
	// Handlers treat this slice as read-only. A later database-backed repository
	// can replace it without changing the current page view models.
	products []product
	// interiorProjects is the ordered temporary source shared by Interior
	// listing and detail handlers.
	//
	// Keeping it on application follows the same dependency pattern as Products
	// without introducing a generic Project abstraction before Architecture's
	// different interface has been designed.
	interiorProjects []interiorProject
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

// disciplineEntranceData is the small view model needed by one homepage
// discipline link.
//
// It deliberately contains presentation data only. Future Product or Project
// database records can be mapped into this shape without coupling the homepage
// template to persistence fields such as IDs, publication status, or slugs.
type disciplineEntranceData struct {
	// Number is a decorative editorial sequence displayed beside the link.
	Number string
	// Name is the visible discipline name and the link's accessible text.
	Name string
	// Path is the real server-rendered destination used by the link's href.
	Path string
}

// disciplinePageData is the minimal view model shared by the three discipline
// landing pages.
//
// It contains route-level interface information only. Catalogue records belong
// to the product-specific view models below, while project descriptions, images,
// and publication state will enter their own vertical slices when designed.
type disciplinePageData struct {
	// Number is the zero-padded position displayed in the editorial sequence.
	Number string
	// Name is the visible h1 and also supplies the document title in the handler.
	Name string
	// NextName labels the next real discipline destination.
	NextName string
	// NextPath is the registered server-rendered URL for that destination.
	NextPath string
}

// productListingData describes the Products-only catalogue section.
//
// The pointer to this value is optional on pageData because only /products uses
// it. Keeping section copy beside its ordered preview slice establishes the
// same handler-to-template data flow that a later database-backed catalogue can
// use without introducing persistence concerns during this stage.
type productListingData struct {
	// Eyebrow is the short interface label displayed above the section heading.
	Eyebrow string
	// Heading names the catalogue section and labels its semantic section.
	Heading string
	// Introduction explains the current scope without inventing product claims.
	Introduction string
	// EmptyMessage is shown when Items is nil or empty.
	EmptyMessage string
	// Items contains temporary structural previews in their editorial order.
	Items []productPreviewData
}

// productPreviewData is the minimal presentation shape for one temporary
// catalogue slot.
//
// These values are not final Product records. They receive a complete trusted
// Path rather than the source Slug and intentionally omit prices, descriptions,
// database IDs, and media until approved content is introduced.
type productPreviewData struct {
	// Number is the zero-padded editorial position visible in the media field.
	Number string
	// Name is the temporary product heading displayed inside the catalogue card.
	Name string
	// Category identifies the broad product family reserved by this slot.
	Category string
	// Status truthfully communicates that approved catalogue content is pending.
	Status string
	// Path is the real server-rendered detail URL used by the card anchor.
	Path string
}

// productDetailData is the complete view model needed by one structural
// product detail page.
//
// It deliberately contains only facts present in the temporary source. Final
// specifications, descriptive copy, pricing, imagery, and purchasing controls
// remain outside Stage 7.
type productDetailData struct {
	// Number is the catalogue position displayed as editorial context.
	Number string
	// Name is the detail page's one primary heading.
	Name string
	// Category identifies the product family in the visible facts list.
	Category string
	// Status communicates that the page is a temporary catalogue preview.
	Status string
}

// interiorProjectListingData describes the Interior Design portfolio section.
//
// The pointer to this value is optional on pageData because only the
// /interior-design route uses it. Section-level copy stays beside the ordered
// preview slice so a later database result can use the same template contract.
type interiorProjectListingData struct {
	// Eyebrow is the short interface label displayed above the section heading.
	Eyebrow string
	// Heading names the project index and labels the shared work section.
	Heading string
	// Introduction explains the temporary index without inventing project claims.
	Introduction string
	// EmptyMessage is shown when Items is nil or empty.
	EmptyMessage string
	// Items contains structural project previews in their editorial order.
	Items []interiorProjectPreviewData
}

// interiorProjectPreviewData is the minimal presentation shape for one
// temporary Interior Design project slot.
//
// It receives a complete trusted Path rather than exposing the source Slug.
// Locations, years, descriptions, and media remain deferred until approved data
// exists.
type interiorProjectPreviewData struct {
	// Number is the zero-padded sequence visible in the structural media field.
	Number string
	// Title is the temporary study heading displayed by the preview article.
	Title string
	// Typology identifies the broad interior category reserved by the slot.
	Typology string
	// Status truthfully communicates that approved project content is pending.
	Status string
	// Path is the real server-rendered project detail URL used by the card link.
	Path string
}

// interiorProjectDetailData is the complete view model needed by one
// structural Interior Design project detail page.
//
// It deliberately includes only facts present in the temporary source. Final
// location, year, client, description, photography, and gallery information
// remain outside Stage 9.
type interiorProjectDetailData struct {
	// Number is the portfolio sequence displayed as editorial context.
	Number string
	// Title is the detail page's one primary heading.
	Title string
	// Typology identifies the broad interior category in the visible facts list.
	Typology string
	// Status communicates that the page is a temporary portfolio preview.
	Status string
}

// pageData is the common top-level value passed to every page template.
//
// A pointer is used for HomeHero because only the homepage has hero data. A nil
// pointer lets other templates omit that optional section naturally. A slice
// is used for HomeDisciplines because templates can range over any number of
// entries, while a nil or empty slice renders no discipline section. The route-
// specific pointers below follow the same optional-data pattern.
type pageData struct {
	// Title becomes the page-specific portion of the browser document title.
	Title string
	// CurrentPath is the real canonical URL represented by the response.
	CurrentPath string
	// NavigationPath optionally identifies a parent route for active navigation.
	NavigationPath string
	// HomeHero contains homepage-only content, or nil for other pages.
	HomeHero *homeHeroData
	// HomeDisciplines contains the ordered homepage route entrances.
	HomeDisciplines []disciplineEntranceData
	// DisciplinePage contains shared landing data, or nil for non-discipline pages.
	DisciplinePage *disciplinePageData
	// ProductListing contains catalogue data only for the Products route.
	ProductListing *productListingData
	// ProductDetail contains one Products detail view, or nil on other routes.
	ProductDetail *productDetailData
	// InteriorProjectListing contains portfolio data only for Interior Design.
	InteriorProjectListing *interiorProjectListingData
	// InteriorProjectDetail contains one Interior detail view, or nil elsewhere.
	InteriorProjectDetail *interiorProjectDetailData
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
		templates:        templateCache,
		products:         temporaryProducts(),
		interiorProjects: temporaryInteriorProjects(),
	}

	return app, nil
}

// newTemplateCache parses every page template together with the shared base
// layout and shared partials, then indexes the resulting sets by page filename.
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

	// Partials contain reusable named templates that page files may compose.
	// Glob returns paths in lexical order, giving every page a deterministic
	// parse order without maintaining another hard-coded file list.
	partials, err := filepath.Glob(
		"./templates/partials/*.html",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"find partial templates: %w",
			err,
		)
	}

	// Stage 5 uses shared template definitions. An entirely missing partial
	// directory is always a startup configuration error; exact required names
	// are verified after each complete template set is parsed below.
	if len(partials) == 0 {
		return nil, fmt.Errorf(
			"no partial templates found",
		)
	}

	// These names form the contract between the three thin page wrappers and
	// the shared discipline partial. Lookup below protects that exact contract
	// even if another unrelated partial file still exists.
	requiredPartialTemplates := []string{
		"disciplinePageStyles",
		"disciplinePageContent",
	}

	for _, page := range pages {
		// Base returns a platform-safe filename for use as the cache key.
		pageName := filepath.Base(page)

		// Build a fresh slice for each page so appending its path cannot reuse
		// and overwrite a backing array shared with the next iteration. ParseFiles
		// accepts the slice with "...", which expands it into variadic arguments.
		files := make(
			[]string,
			0,
			len(partials)+2,
		)
		files = append(
			files,
			"./templates/base.html",
		)
		files = append(files, partials...)
		files = append(files, page)

		templateSet, err := template.ParseFiles(files...)
		if err != nil {
			return nil, fmt.Errorf(
				"parse template %s: %w",
				pageName,
				err,
			)
		}

		// ParseFiles can successfully parse a set that does not define a name
		// referenced only at execution time. Explicit Lookup checks convert that
		// delayed response failure into an actionable application-startup error.
		for _, templateName := range requiredPartialTemplates {
			if templateSet.Lookup(templateName) == nil {
				return nil, fmt.Errorf(
					"page template %s requires %s",
					pageName,
					templateName,
				)
			}
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
