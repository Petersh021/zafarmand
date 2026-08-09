package main

import (
	"context"
	"errors"
	"log"
	"mime"
	"net/http"
	"time"
)

const (
	// inquiryPersistenceTimeout bounds one Contact database write independently
	// from the visitor connection and the process-wide server lifetime.
	inquiryPersistenceTimeout = 5 * time.Second
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
// Contact responses are never cached because a validation or storage-failure
// response may contain reflected personal values. The handler establishes the
// reusable CSRF pair, creates a separate per-form idempotency token, and
// consumes a valid one-time success receipt after Post/Redirect/Get.
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

	submissionToken, err := newInquirySubmissionToken()
	if err != nil {
		// As with CSRF generation, a missing cryptographic token is an internal
		// failure. Rendering a form without a safe database identity would allow a
		// retry to create duplicate inquiries.
		log.Printf(
			"could not create inquiry submission token: %v",
			err,
		)
		http.Error(
			w,
			"internal server error",
			http.StatusInternalServerError,
		)
		return
	}

	submissionState := inquirySubmissionStateForm
	if r.Method == http.MethodGet &&
		app.inquirySuccess.consume(w, r) {
		// HEAD shares Go's GET route pattern but must not consume a browser's
		// one-time confirmation before its following document navigation.
		submissionState = inquirySubmissionStateSucceeded
	}

	app.renderContactPage(
		w,
		http.StatusOK,
		newContactPageData(
			csrfToken,
			submissionToken,
			inquiryFormData{},
			inquiryFormErrors{},
			submissionState,
		),
	)
}

// inquirySubmissionHandler validates POST /contact, persists one idempotent
// inquiry, and redirects only after the repository confirms durable success.
//
// It preserves the Stage 12 protocol and validation boundaries. Invalid input
// never reaches PostgreSQL, and storage errors render generic copy without
// logging the visitor's name, email address, message, or a raw driver error.
func (app *application) inquirySubmissionHandler(
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

	// The submission token is generated independently for one form render. Treat
	// a missing or malformed value as an ambiguous request rather than inventing
	// a replacement after the visitor may already be retrying an earlier POST.
	submissionKey, validSubmissionToken := decodeInquirySubmissionToken(
		r.PostForm.Get(inquirySubmissionFieldName),
	)
	if !validSubmissionToken {
		http.Error(
			w,
			"bad request",
			http.StatusBadRequest,
		)
		return
	}
	submissionToken := r.PostForm.Get(
		inquirySubmissionFieldName,
	)

	form := normalizeInquiryForm(r.PostForm)
	formErrors := validateInquiryForm(form)
	if hasInquiryFormErrors(formErrors) {
		app.renderContactPage(
			w,
			http.StatusUnprocessableEntity,
			newContactPageData(
				csrfToken,
				submissionToken,
				form,
				formErrors,
				inquirySubmissionStateForm,
			),
		)
		return
	}

	// A short deadline prevents a temporarily unavailable database from holding
	// the request indefinitely. The request remains the parent, so a client
	// disconnect or forced connection closure can cancel the operation sooner;
	// graceful Shutdown itself waits for active handlers instead of canceling them.
	createContext, cancel := context.WithTimeout(
		r.Context(),
		inquiryPersistenceTimeout,
	)
	defer cancel()

	createResult, err := app.inquiries.Create(
		createContext,
		inquirySubmission{
			SubmissionKey: submissionKey,
			Name:          form.Name,
			Email:         form.Email,
			Discipline:    form.Discipline,
			Message:       form.Message,
		},
	)
	if errors.Is(err, errInquirySubmissionConflict) {
		// A conflicting key can never succeed with the same payload retry. Replace
		// only the opaque form key, retain the normalized fields for review, and
		// explain that the next POST will be a new inquiry rather than presenting a
		// misleading transient-failure message.
		freshSubmissionToken, tokenErr := newInquirySubmissionToken()
		if tokenErr != nil {
			log.Print("could not renew conflicting inquiry submission token")
			http.Error(
				w,
				"internal server error",
				http.StatusInternalServerError,
			)
			return
		}

		log.Print("Contact inquiry submission key conflicted")
		app.renderContactPage(
			w,
			http.StatusConflict,
			newContactPageData(
				csrfToken,
				freshSubmissionToken,
				form,
				formErrors,
				inquirySubmissionStateConflict,
			),
		)
		return
	}
	if err != nil ||
		(createResult != inquiryCreateResultCreated &&
			createResult != inquiryCreateResultReplay) {
		// The log intentionally records only a stable event category. Repository
		// errors and visitor values are omitted because PostgreSQL errors can include
		// rejected personal data in their detail text.
		log.Print("could not save Contact inquiry")
		app.renderContactPage(
			w,
			http.StatusServiceUnavailable,
			newContactPageData(
				csrfToken,
				submissionToken,
				form,
				formErrors,
				inquirySubmissionStateFailed,
			),
		)
		return
	}

	// The signed receipt carries a fresh server-generated nonce rather than the
	// untrusted hidden submission token. It lets the redirected GET make a
	// truthful one-time success claim without putting PII or a forgeable query
	// flag in the URL or cookie.
	if err := app.inquirySuccess.issue(
		w,
		r,
	); err != nil {
		log.Print("could not create Contact inquiry success receipt")
		http.Error(
			w,
			"inquiry saved, but confirmation unavailable",
			http.StatusInternalServerError,
		)
		return
	}

	http.Redirect(
		w,
		r,
		"/contact#contact-form-response",
		http.StatusSeeOther,
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
