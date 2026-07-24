package main

import "net/http"

func (app *application) routes() http.Handler {
	mux := http.NewServeMux()

	fileServer := http.FileServer(http.Dir("./static"))

	mux.Handle(
		"/static/",
		http.StripPrefix("/static/", fileServer),
	)

	mux.HandleFunc("/", app.homeHandler)
	mux.HandleFunc("/products", app.productsHandler)
	mux.HandleFunc(
		"/interior-design",
		app.interiorDesignHandler,
	)
	mux.HandleFunc(
		"/architecture-design",
		app.architectureDesignHandler,
	)

	return mux
}
