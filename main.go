package main

import (
	"log"
	"net/http"
)

func main() {
	app, err := newApplication()
	if err != nil {
		log.Fatal(err)
	}

	const address = ":8080"

	log.Printf(
		"Zafarmand website is running at http://localhost%s",
		address,
	)

	if err := http.ListenAndServe(address, app.routes()); err != nil {
		log.Fatal(err)
	}
}
