package main

import (
	"errors"
	"log"
	"mime"
	"net/http"
)

// homeHandler renders the public homepage for a request already matched to
// GET / by the router.
//
// The request value is deliberately named "_" because this handler currently
// needs no headers, query values, or form data from it. The pageData value is
// the boundary between Go and the HTML template: CurrentPath controls active
// navigation state, HomeHero supplies temporary structured hero content, and
// HomeDisciplines supplies an ordered collection for the template to range
// over. These view models can later be populated from a database without
// changing their HTML contract.
func (app *application) homeHandler(
	w http.ResponseWriter,
	_ *http.Request,
) {
	app.render(
		w,
		http.StatusOK,
		"home.html",
		pageData{
			Title:       "Home",
			CurrentPath: "/",
			HomeHero: &homeHeroData{
				StudioName: "Zafarmand",
				Descriptor: "Design Studio",
				ImageURL: "/static/images/" +
					"home-hero-placeholder.jpg",
				ImageAlt: "Warm minimalist living room " +
					"with stone walls, sculptural seating, " +
					"and a wooden chair",
				ImageWidth:  1536,
				ImageHeight: 1024,
			},
			// The order matches the desktop header and drawer navigation.
			// These are structural route entrances, not database records.
			HomeDisciplines: []disciplineEntranceData{
				{
					Number: "01",
					Name:   "Interior Design",
					Path:   "/interior-design",
				},
				{
					Number: "02",
					Name:   "Architecture Design",
					Path:   "/architecture-design",
				},
				{
					Number: "03",
					Name:   "Products",
					Path:   "/products",
				},
			},
		},
	)
}

// productsHandler renders the Products landing page with an HTTP 200 response.
//
// CurrentPath matches the route exactly so shared navigation templates can
// mark Products as the current page for sighted users and assistive technology.
// DisciplinePage contains truthful route-level presentation data, while
// ProductListing maps the application's shared temporary product source into
// catalogue previews. Each preview now includes a real Stage 7 detail path,
// while prices, final descriptions, media, and database state remain deferred.
func (app *application) productsHandler(
	w http.ResponseWriter,
	_ *http.Request,
) {
	disciplinePage := &disciplinePageData{
		Number:   "03",
		Name:     "Products",
		NextName: "Interior Design",
		NextPath: "/interior-design",
	}

	// The listing view keeps section copy beside a mapped preview slice. Both the
	// catalogue and detail handler read app.products, so card URLs and lookup
	// records cannot drift into two independent hard-coded collections.
	productListing := &productListingData{
		Eyebrow: "Zafarmand objects",
		Heading: "Product catalogue",
		Introduction: "An evolving index of furniture, lighting, " +
			"objects, and material studies.",
		EmptyMessage: "Product entries are being prepared for publication.",
		Items:        productPreviews(app.products),
	}

	app.render(
		w,
		http.StatusOK,
		"products.html",
		pageData{
			Title:          disciplinePage.Name,
			CurrentPath:    "/products",
			DisciplinePage: disciplinePage,
			ProductListing: productListing,
		},
	)
}

// productDetailHandler renders one temporary product detail selected by the
// slug captured in the GET /products/{slug} route.
//
// Request.PathValue reads the decoded wildcard supplied by Go's ServeMux. An
// exact whitelist lookup prevents arbitrary visitor input from becoming page
// content or a template name. Unknown slugs receive a normal HTTP 404 before
// any detail template is executed.
func (app *application) productDetailHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	slug := r.PathValue("slug")
	product, exists := findProductBySlug(
		app.products,
		slug,
	)
	if !exists {
		http.NotFound(w, r)
		return
	}

	productDetail := newProductDetailData(product)
	currentPath := productDetailPath(product.Slug)

	app.render(
		w,
		http.StatusOK,
		"product-detail.html",
		pageData{
			Title:          productDetail.Name,
			CurrentPath:    currentPath,
			NavigationPath: "/products",
			ProductDetail:  &productDetail,
		},
	)
}

// interiorDesignHandler renders the Interior Design landing page.
//
// The handler supplies both the shared discipline shell and the Interior-only
// listing. Stage 9 maps each application-owned source record to a preview with a
// real detail path, while final descriptions, images, database state, and admin
// controls remain deferred.
func (app *application) interiorDesignHandler(
	w http.ResponseWriter,
	_ *http.Request,
) {
	disciplinePage := &disciplinePageData{
		Number:   "01",
		Name:     "Interior Design",
		NextName: "Architecture Design",
		NextPath: "/architecture-design",
	}

	// Section copy and ordered preview data travel to the template as one value.
	// The listing and detail handler share app.interiorProjects, which prevents
	// card destinations and route lookup records from drifting apart.
	interiorProjectListing := &interiorProjectListingData{
		Eyebrow: "Zafarmand interiors",
		Heading: "Interior project index",
		Introduction: "A developing index of residential, hospitality, " +
			"workplace, and cultural interior studies.",
		EmptyMessage: "Interior project entries are being prepared " +
			"for publication.",
		Items: interiorProjectPreviews(app.interiorProjects),
	}

	app.render(
		w,
		http.StatusOK,
		"interior-design.html",
		pageData{
			Title:                  disciplinePage.Name,
			CurrentPath:            "/interior-design",
			DisciplinePage:         disciplinePage,
			InteriorProjectListing: interiorProjectListing,
		},
	)
}

// interiorProjectDetailHandler renders one temporary Interior Design project
// selected by the slug captured in GET /interior-design/{slug}.
//
// PathValue returns the wildcard already decoded by Go's ServeMux. Looking it
// up in the application-owned source keeps visitor input away from template
// names and page content. An unknown or differently cased slug receives a
// normal 404 response before the detail template is executed.
func (app *application) interiorProjectDetailHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	slug := r.PathValue("slug")
	project, exists := findInteriorProjectBySlug(
		app.interiorProjects,
		slug,
	)
	if !exists {
		http.NotFound(w, r)
		return
	}

	projectDetail := newInteriorProjectDetailData(project)
	currentPath := interiorProjectDetailPath(project.Slug)

	app.render(
		w,
		http.StatusOK,
		"interior-project-detail.html",
		pageData{
			Title:                 projectDetail.Title,
			CurrentPath:           currentPath,
			NavigationPath:        "/interior-design",
			InteriorProjectDetail: &projectDetail,
		},
	)
}

// architectureDesignHandler renders the Architecture Design landing page.
//
// The handler combines the shared discipline shell with the Architecture-only
// listing. Stage 11 maps each application-owned source record to a preview with
// a real detail path, while final content, media, database state, and admin
// controls remain deferred.
func (app *application) architectureDesignHandler(
	w http.ResponseWriter,
	_ *http.Request,
) {
	disciplinePage := &disciplinePageData{
		Number:   "02",
		Name:     "Architecture Design",
		NextName: "Products",
		NextPath: "/products",
	}

	// Section copy and ordered previews travel as one Architecture-specific
	// value. The listing and detail handler share app.architectureProjects, so
	// card destinations and accepted lookup records cannot drift apart.
	architectureProjectListing := &architectureProjectListingData{
		Eyebrow: "Zafarmand architecture",
		Heading: "Architecture project index",
		Introduction: "A developing index of residential, commercial, " +
			"cultural, and civic architecture studies.",
		EmptyMessage: "Architecture project entries are being prepared " +
			"for publication.",
		Items: architectureProjectPreviews(app.architectureProjects),
	}

	app.render(
		w,
		http.StatusOK,
		"architecture-design.html",
		pageData{
			Title:                      disciplinePage.Name,
			CurrentPath:                "/architecture-design",
			DisciplinePage:             disciplinePage,
			ArchitectureProjectListing: architectureProjectListing,
		},
	)
}

// architectureProjectDetailHandler renders one temporary Architecture Design
// project selected by the slug captured in GET /architecture-design/{slug}.
//
// PathValue returns the wildcard decoded by Go's ServeMux. Looking it up in the
// application-owned source prevents visitor input from becoming template names
// or arbitrary page content. Unknown or differently cased slugs receive a
// normal 404 before the detail template is executed.
func (app *application) architectureProjectDetailHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	slug := r.PathValue("slug")
	project, exists := findArchitectureProjectBySlug(
		app.architectureProjects,
		slug,
	)
	if !exists {
		http.NotFound(w, r)
		return
	}

	projectDetail := newArchitectureProjectDetailData(project)
	currentPath := architectureProjectDetailPath(project.Slug)

	app.render(
		w,
		http.StatusOK,
		"architecture-project-detail.html",
		pageData{
			Title:                     projectDetail.Title,
			CurrentPath:               currentPath,
			NavigationPath:            "/architecture-design",
			ArchitectureProjectDetail: &projectDetail,
		},
	)
}

// contactHandler renders the initial Contact form for GET /contact.
//
// Contact responses are never cached because a later validation response may
// contain reflected personal values. The handler also establishes the random
// double-submit token that the form sends back with its protected cookie.
func (app *application) contactHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set(
		"Cache-Control",
		"no-store",
	)

	csrfToken, err := ensureInquiryCSRFToken(w, r)
	if err != nil {
		// Random-source failure is an internal condition. The response deliberately
		// excludes implementation details and never renders a form without CSRF
		// protection.
		log.Printf(
			"could not create inquiry CSRF token: %v",
			err,
		)
		http.Error(
			w,
			"internal server error",
			http.StatusInternalServerError,
		)
		return
	}

	app.renderContactPage(
		w,
		http.StatusOK,
		newContactPageData(
			csrfToken,
			inquiryFormData{},
			inquiryFormErrors{},
			nil,
		),
	)
}

// inquiryPreviewHandler validates POST /contact and renders either correctable
// field errors or a truthful, non-persistent preview.
//
// This Stage 12 endpoint does not save, send, enqueue, or log an inquiry. Its
// narrow job is to establish secure request and validation behavior before a
// later PostgreSQL-backed submission workflow is designed.
func (app *application) inquiryPreviewHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	// Start with the privacy-sensitive response policy so it also applies to
	// early 4xx exits produced before template rendering.
	w.Header().Set(
		"Cache-Control",
		"no-store",
	)

	// MaxBytesReader stops oversized bodies while ParseForm is reading them. It
	// is installed before any body parsing so the limit is one reliable boundary.
	r.Body = http.MaxBytesReader(
		w,
		r.Body,
		inquiryRequestBodyLimit,
	)

	mediaType, _, err := mime.ParseMediaType(
		r.Header.Get("Content-Type"),
	)
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		http.Error(
			w,
			"unsupported media type",
			http.StatusUnsupportedMediaType,
		)
		return
	}

	if err := r.ParseForm(); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			http.Error(
				w,
				"request body too large",
				http.StatusRequestEntityTooLarge,
			)
			return
		}

		// Malformed percent encoding and other URL-form parsing failures are
		// client errors; no submitted values are reflected in this response.
		http.Error(
			w,
			"bad request",
			http.StatusBadRequest,
		)
		return
	}

	// Reject ambiguous repeated security and form fields before selecting any
	// values. Missing visitor fields continue to normal 422 validation, while a
	// missing CSRF field is handled by the explicit 403 boundary below.
	if inquiryFormHasDuplicateValues(r.PostForm) {
		http.Error(
			w,
			"bad request",
			http.StatusBadRequest,
		)
		return
	}

	csrfToken, validCSRF := validateInquiryCSRFToken(
		r,
		r.PostForm.Get(inquiryCSRFFieldName),
	)
	if !validCSRF {
		http.Error(
			w,
			"forbidden",
			http.StatusForbidden,
		)
		return
	}

	form := normalizeInquiryForm(r.PostForm)
	formErrors := validateInquiryForm(form)
	if hasInquiryFormErrors(formErrors) {
		app.renderContactPage(
			w,
			http.StatusUnprocessableEntity,
			newContactPageData(
				csrfToken,
				form,
				formErrors,
				nil,
			),
		)
		return
	}

	preview := newInquiryPreview(form)
	app.renderContactPage(
		w,
		http.StatusOK,
		newContactPageData(
			csrfToken,
			form,
			formErrors,
			&preview,
		),
	)
}

// renderContactPage supplies the route-level metadata shared by all Contact
// states and delegates buffered HTML execution to the application's renderer.
func (app *application) renderContactPage(
	w http.ResponseWriter,
	status int,
	contact *contactPageData,
) {
	app.render(
		w,
		status,
		"contact.html",
		pageData{
			Title:       "Contact",
			CurrentPath: "/contact",
			Contact:     contact,
		},
	)
}
