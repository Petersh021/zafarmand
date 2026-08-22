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
	// The managed homepage hero uses one revisioned public media URL. Its
	// handler returns bytes only for the exact current enabled revision, while
	// the homepage keeps the checked-in image as its explicit bootstrap fallback.
	mux.HandleFunc(
		"GET /homepage/hero/{version}",
		app.homepageHeroHandler,
	)
	mux.HandleFunc("GET /products", app.productsHandler)
	// A cover revision is a separate ETag-validated binary response. The route
	// remains public only while the owning Product is published.
	mux.HandleFunc(
		"GET /products/{slug}/cover/{version}",
		app.productCoverHandler,
	)
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
	// The revisioned cover route is more specific than the one-segment detail
	// route. Its handler independently rechecks that the owning project remains
	// Published before returning any image bytes.
	mux.HandleFunc(
		"GET /interior-design/{slug}/cover/{version}",
		app.interiorProjectCoverHandler,
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
	// Architecture cover URLs carry the current revision and recheck the owning
	// project's Published state before any reviewed bytes leave PostgreSQL.
	mux.HandleFunc(
		"GET /architecture-design/{slug}/cover/{version}",
		app.architectureProjectCoverHandler,
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

	// Both current roles can inspect every Product lifecycle state, while future
	// roles receive no access unless added explicitly to this read allowlist.
	productReaderRoles := requireAdminRoles(
		adminRoleOwner,
		adminRoleEditor,
	)
	mux.Handle(
		"GET /admin/products",
		app.requireAdmin(
			productReaderRoles(
				http.HandlerFunc(app.adminProductListHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/products/{id}",
		app.requireAdmin(
			productReaderRoles(
				http.HandlerFunc(app.adminProductDetailHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/products/{id}/cover/{version}",
		app.requireAdmin(
			productReaderRoles(
				http.HandlerFunc(app.adminProductCoverAssetHandler),
			),
		),
	)

	// Product text and cover changes use a separate mutation allowlist. Owner and
	// editor are named explicitly; static /new wins over the detail wildcard, and
	// every successful POST redirects to a canonical GET.
	productWriterRoles := requireAdminRoles(
		adminRoleOwner,
		adminRoleEditor,
	)
	mux.Handle(
		"GET /admin/products/new",
		app.requireAdmin(
			productWriterRoles(
				http.HandlerFunc(app.adminProductNewHandler),
			),
		),
	)
	mux.Handle(
		"POST /admin/products",
		app.requireAdmin(
			productWriterRoles(
				http.HandlerFunc(app.adminProductCreateHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/products/{id}/edit",
		app.requireAdmin(
			productWriterRoles(
				http.HandlerFunc(app.adminProductEditHandler),
			),
		),
	)
	mux.Handle(
		"POST /admin/products/{id}",
		app.requireAdmin(
			productWriterRoles(
				http.HandlerFunc(app.adminProductUpdateHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/products/{id}/cover",
		app.requireAdmin(
			productWriterRoles(
				http.HandlerFunc(app.adminProductCoverFormHandler),
			),
		),
	)
	mux.Handle(
		"POST /admin/products/{id}/cover",
		app.requireAdmin(
			productWriterRoles(
				http.HandlerFunc(app.adminProductCoverUploadHandler),
			),
		),
	)

	// Interior-project reads deliberately repeat the Product authorization
	// boundary instead of sharing a broad "content" permission implicitly. Both
	// current roles may inspect every lifecycle state and exact protected cover;
	// any future role remains denied until it is reviewed here explicitly.
	interiorProjectReaderRoles := requireAdminRoles(
		adminRoleOwner,
		adminRoleEditor,
	)
	mux.Handle(
		"GET /admin/interior-projects",
		app.requireAdmin(
			interiorProjectReaderRoles(
				http.HandlerFunc(app.adminInteriorProjectListHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/interior-projects/{id}",
		app.requireAdmin(
			interiorProjectReaderRoles(
				http.HandlerFunc(app.adminInteriorProjectDetailHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/interior-projects/{id}/cover/{version}",
		app.requireAdmin(
			interiorProjectReaderRoles(
				http.HandlerFunc(app.adminInteriorProjectCoverAssetHandler),
			),
		),
	)

	// Interior create/update and cover replacement use explicit mutation routes;
	// publication is a validated create/update field. GET only presents a form,
	// while POST mutates then redirects using Post/Redirect/Get.
	interiorProjectWriterRoles := requireAdminRoles(
		adminRoleOwner,
		adminRoleEditor,
	)
	mux.Handle(
		"GET /admin/interior-projects/new",
		app.requireAdmin(
			interiorProjectWriterRoles(
				http.HandlerFunc(app.adminInteriorProjectNewHandler),
			),
		),
	)
	mux.Handle(
		"POST /admin/interior-projects",
		app.requireAdmin(
			interiorProjectWriterRoles(
				http.HandlerFunc(app.adminInteriorProjectCreateHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/interior-projects/{id}/edit",
		app.requireAdmin(
			interiorProjectWriterRoles(
				http.HandlerFunc(app.adminInteriorProjectEditHandler),
			),
		),
	)
	mux.Handle(
		"POST /admin/interior-projects/{id}",
		app.requireAdmin(
			interiorProjectWriterRoles(
				http.HandlerFunc(app.adminInteriorProjectUpdateHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/interior-projects/{id}/cover",
		app.requireAdmin(
			interiorProjectWriterRoles(
				http.HandlerFunc(app.adminInteriorProjectCoverFormHandler),
			),
		),
	)
	mux.Handle(
		"POST /admin/interior-projects/{id}/cover",
		app.requireAdmin(
			interiorProjectWriterRoles(
				http.HandlerFunc(app.adminInteriorProjectCoverUploadHandler),
			),
		),
	)

	// Architecture reads have an explicit allowlist separate from Product and
	// Interior access. Owner and Editor may inspect every lifecycle state and an
	// exact protected cover revision; a future role remains denied until it is
	// deliberately reviewed and named here.
	architectureProjectReaderRoles := requireAdminRoles(
		adminRoleOwner,
		adminRoleEditor,
	)
	mux.Handle(
		"GET /admin/architecture-projects",
		app.requireAdmin(
			architectureProjectReaderRoles(
				http.HandlerFunc(app.adminArchitectureProjectListHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/architecture-projects/{id}",
		app.requireAdmin(
			architectureProjectReaderRoles(
				http.HandlerFunc(app.adminArchitectureProjectDetailHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/architecture-projects/{id}/cover/{version}",
		app.requireAdmin(
			architectureProjectReaderRoles(
				http.HandlerFunc(app.adminArchitectureProjectCoverAssetHandler),
			),
		),
	)

	// Architecture mutations use a distinct Owner/Editor allowlist so write
	// authority never follows implicitly from read access. Each GET only renders
	// a form; each validated POST applies one revision-aware mutation and then
	// redirects to the canonical detail GET using Post/Redirect/Get.
	architectureProjectWriterRoles := requireAdminRoles(
		adminRoleOwner,
		adminRoleEditor,
	)
	mux.Handle(
		"GET /admin/architecture-projects/new",
		app.requireAdmin(
			architectureProjectWriterRoles(
				http.HandlerFunc(app.adminArchitectureProjectNewHandler),
			),
		),
	)
	mux.Handle(
		"POST /admin/architecture-projects",
		app.requireAdmin(
			architectureProjectWriterRoles(
				http.HandlerFunc(app.adminArchitectureProjectCreateHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/architecture-projects/{id}/edit",
		app.requireAdmin(
			architectureProjectWriterRoles(
				http.HandlerFunc(app.adminArchitectureProjectEditHandler),
			),
		),
	)
	mux.Handle(
		"POST /admin/architecture-projects/{id}",
		app.requireAdmin(
			architectureProjectWriterRoles(
				http.HandlerFunc(app.adminArchitectureProjectUpdateHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/architecture-projects/{id}/cover",
		app.requireAdmin(
			architectureProjectWriterRoles(
				http.HandlerFunc(app.adminArchitectureProjectCoverFormHandler),
			),
		),
	)
	mux.Handle(
		"POST /admin/architecture-projects/{id}/cover",
		app.requireAdmin(
			architectureProjectWriterRoles(
				http.HandlerFunc(app.adminArchitectureProjectCoverUploadHandler),
			),
		),
	)

	// Site-content reads have an explicit allowlist independent from discipline
	// records and inquiry data. Both current editorial roles may inspect the
	// Homepage, Contact, and exact protected hero state; future roles remain
	// denied until deliberately added here.
	siteContentReaderRoles := requireAdminRoles(
		adminRoleOwner,
		adminRoleEditor,
	)
	mux.Handle(
		"GET /admin/site-content",
		app.requireAdmin(
			siteContentReaderRoles(
				http.HandlerFunc(app.adminSiteContentOverviewHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/site-content/homepage",
		app.requireAdmin(
			siteContentReaderRoles(
				http.HandlerFunc(app.adminHomepageContentDetailHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/site-content/homepage/hero/{version}",
		app.requireAdmin(
			siteContentReaderRoles(
				http.HandlerFunc(app.adminHomepageHeroAssetHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/site-content/contact",
		app.requireAdmin(
			siteContentReaderRoles(
				http.HandlerFunc(app.adminContactContentDetailHandler),
			),
		),
	)

	// Homepage, hero, and Contact changes repeat the current Owner/Editor policy
	// through a separate mutation allowlist. GET presents an authenticated form;
	// POST validates CSRF and optimistic revision state before a 303 redirect.
	siteContentWriterRoles := requireAdminRoles(
		adminRoleOwner,
		adminRoleEditor,
	)
	mux.Handle(
		"GET /admin/site-content/homepage/edit",
		app.requireAdmin(
			siteContentWriterRoles(
				http.HandlerFunc(app.adminHomepageContentEditHandler),
			),
		),
	)
	mux.Handle(
		"POST /admin/site-content/homepage",
		app.requireAdmin(
			siteContentWriterRoles(
				http.HandlerFunc(app.adminHomepageContentUpdateHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/site-content/homepage/hero",
		app.requireAdmin(
			siteContentWriterRoles(
				http.HandlerFunc(app.adminHomepageHeroFormHandler),
			),
		),
	)
	mux.Handle(
		"POST /admin/site-content/homepage/hero",
		app.requireAdmin(
			siteContentWriterRoles(
				http.HandlerFunc(app.adminHomepageHeroUploadHandler),
			),
		),
	)
	mux.Handle(
		"GET /admin/site-content/contact/edit",
		app.requireAdmin(
			siteContentWriterRoles(
				http.HandlerFunc(app.adminContactContentEditHandler),
			),
		),
	)
	mux.Handle(
		"POST /admin/site-content/contact",
		app.requireAdmin(
			siteContentWriterRoles(
				http.HandlerFunc(app.adminContactContentUpdateHandler),
			),
		),
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

	// Status changes are explicit POST mutations. Repeating the role allowlist
	// here makes write permission a separate decision from read permission, even
	// while both current roles remain allowed in the workflow introduced then.
	inquiryStatusWriterRoles := requireAdminRoles(
		adminRoleOwner,
		adminRoleEditor,
	)
	mux.Handle(
		"POST /admin/inquiries/{id}/status",
		app.requireAdmin(
			inquiryStatusWriterRoles(
				http.HandlerFunc(app.adminInquiryStatusUpdateHandler),
			),
		),
	)

	// Applying policy outside ServeMux also covers its generated responses:
	// private admin 404/405 pages retain the full security header set, and the
	// exact Contact URL remains no-store even when a wrong method yields 405.
	return contactPrivacyHeaders(adminSecurityHeaders(mux))
}
