// Package main assembles and runs the Zafarmand web application.
//
// The executable is intentionally small: routing, request handling, and
// template work live in separate files so each concern can be learned and
// changed independently.
package main

import (
	"log"
	"net/http"
)

// main is the application's composition root and process entry point.
//
// It builds the shared application dependencies, registers the HTTP routes,
// and then starts a blocking web server on port 8080. Startup and server
// failures are fatal because the website cannot serve requests without either
// a valid application or a listening server.
func main() {
	app, err := newApplication()
	if err != nil {
		log.Fatal(err)
	}

	// Keeping the address in one named constant makes the server configuration
	// easy to find while development still uses a fixed local port.
	const address = ":8080"

	log.Printf(
		"Zafarmand website is running at http://localhost%s",
		address,
	)

	// ListenAndServe blocks while the process is running. app.routes() supplies
	// the http.Handler that decides which code handles each incoming request.
	if err := http.ListenAndServe(address, app.routes()); err != nil {
		log.Fatal(err)
	}
}
