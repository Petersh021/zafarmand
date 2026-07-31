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

	return mux
}
