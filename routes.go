package main

import "net/http"

// routes constructs and returns the application's HTTP routing tree.
//
// Go's ServeMux patterns include the HTTP method. A GET pattern accepts GET and
// HEAD, while unrelated methods receive a 405 Method Not Allowed response
// automatically. Returning http.Handler keeps main and tests independent of
// the concrete *http.ServeMux implementation.
func (app *application) routes() http.Handler {
	mux := http.NewServeMux()

	// FileServer reads public assets from the local static directory. The
	// browser requests /static/css/main.css, but StripPrefix removes /static/
	// before FileServer maps the remainder to ./static/css/main.css.
	fileServer := http.FileServer(http.Dir("./static"))

	mux.Handle(
		"GET /static/",
		http.StripPrefix("/static/", fileServer),
	)

	// /{$} is an exact-root pattern. The {$} prevents the homepage handler from
	// acting as a catch-all for unknown paths, which lets ServeMux return a
	// correct 404 response for URLs the application does not define.
	mux.HandleFunc("GET /{$}", app.homeHandler)
	mux.HandleFunc("GET /products", app.productsHandler)
	// The one-segment wildcard creates a real server-rendered URL for each
	// product. ServeMux gives the static /products route higher specificity, and
	// productDetailHandler validates the captured slug before rendering.
	mux.HandleFunc(
		"GET /products/{slug}",
		app.productDetailHandler,
	)
	mux.HandleFunc(
		"GET /interior-design",
		app.interiorDesignHandler,
	)
	// The Interior wildcard follows the same real server-rendered routing
	// contract as Products. The exact listing route above remains the more
	// specific match, while the detail handler validates one captured slug.
	mux.HandleFunc(
		"GET /interior-design/{slug}",
		app.interiorProjectDetailHandler,
	)
	mux.HandleFunc(
		"GET /architecture-design",
		app.architectureDesignHandler,
	)
	// The one-segment Architecture wildcard creates a real URL for each project.
	// ServeMux keeps the exact listing route above more specific, while the
	// handler validates the captured slug before rendering any detail content.
	mux.HandleFunc(
		"GET /architecture-design/{slug}",
		app.architectureProjectDetailHandler,
	)

	// Contact uses separate method-aware handlers at one canonical URL. GET
	// presents the protected form and consumes a one-time success receipt, while
	// POST validates and persists through the injected inquiry repository.
	mux.HandleFunc(
		"GET /contact",
		app.contactHandler,
	)
	mux.HandleFunc(
		"POST /contact",
		app.inquirySubmissionHandler,
	)

	// Admin login remains available without an authenticated session but uses
	// separate method-aware handlers so credentials can never travel in a URL.
	// The private dashboard and logout mutation both pass through requireAdmin,
	// which resolves one active database session before their handlers run.
	mux.HandleFunc(
		"GET /admin/login",
		app.adminLoginPageHandler,
	)
	mux.HandleFunc(
		"POST /admin/login",
		app.adminLoginHandler,
	)
	mux.Handle(
		"GET /admin",
		app.requireAdmin(http.HandlerFunc(app.adminDashboardHandler)),
	)
	mux.Handle(
		"POST /admin/logout",
		app.requireAdmin(http.HandlerFunc(app.adminLogoutHandler)),
	)

	// Inquiry content contains visitor personal data, so every list and detail
	// request passes through authentication and an explicit role allowlist. Both
	// current roles may read; future roles receive no access automatically.
	inquiryReaderRoles := requireAdminRoles(
		adminRoleOwner,
		adminRoleEditor,
	)
	mux.Handle(
		"GET /admin/inquiries",
		app.requireAdmin(
			inquiryReaderRoles(
				http.HandlerFunc(app.adminInquiryListHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/inquiries/{id}",
		app.requireAdmin(
			inquiryReaderRoles(
				http.HandlerFunc(app.adminInquiryDetailHandler),
			),
		),
	)

	// Applying headers outside ServeMux also protects its generated admin 404 and
	// 405 responses while leaving the established public response policy intact.
	return adminSecurityHeaders(mux)
}
