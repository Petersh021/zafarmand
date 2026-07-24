package main

import "net/http"

func (app *application) homeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	app.render(
		w,
		http.StatusOK,
		"home.html",
		pageData{
			Title:       "Home",
			CurrentPath: "/",
		},
	)
}

func (app *application) productsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.render(
		w,
		http.StatusOK,
		"products.html",
		pageData{
			Title:       "Products",
			CurrentPath: "/products",
		},
	)
}

func (app *application) interiorDesignHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.render(
		w,
		http.StatusOK,
		"interior-design.html",
		pageData{
			Title:       "Interior Design",
			CurrentPath: "/interior-design",
		},
	)
}

func (app *application) architectureDesignHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	app.render(
		w,
		http.StatusOK,
		"architecture-design.html",
		pageData{
			Title:       "Architecture Design",
			CurrentPath: "/architecture-design",
		},
	)
}
