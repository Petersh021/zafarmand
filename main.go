package main

import (
	"fmt"
	"log"
	"net/http"
)

func renderPage(
	w http.ResponseWriter,
	title string,
	heading string,
	description string,
) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if _, err := fmt.Fprintf(w, `
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">

	<title>%s | Zafarmand Studio</title>
</head>

<body>
	<header>
		<a href="/">Zafarmand Studio</a>

		<nav>
			<a href="/">Home</a>
			<a href="/products">Products</a>
			<a href="/interior-design">Interior Design</a>
			<a href="/architecture-design">Architecture Design</a>
		</nav>
	</header>

	<main>
		<h1>%s</h1>
		<p>%s</p>
	</main>
</body>
</html>
`, title, heading, description); err != nil {
		log.Printf("could not write response: %v", err)
	}
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	renderPage(
		w,
		"Home",
		"Zafarmand Studio",
		"Products, Interior Design, and Architecture Design.",
	)
}

func productsHandler(w http.ResponseWriter, r *http.Request) {
	renderPage(
		w,
		"Products",
		"Products",
		"Explore furniture, lighting, objects, and custom-designed products.",
	)
}

func interiorDesignHandler(w http.ResponseWriter, r *http.Request) {
	renderPage(
		w,
		"Interior Design",
		"Interior Design",
		"Explore residential, commercial, and hospitality interior projects.",
	)
}

func architectureDesignHandler(w http.ResponseWriter, r *http.Request) {
	renderPage(
		w,
		"Architecture Design",
		"Architecture Design",
		"Explore built, conceptual, and in-progress architectural projects.",
	)
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", homeHandler)
	mux.HandleFunc("/products", productsHandler)
	mux.HandleFunc("/interior-design", interiorDesignHandler)
	mux.HandleFunc("/architecture-design", architectureDesignHandler)

	const address = ":8080"

	log.Printf("Zafarmand website is running at http://localhost%s", address)

	if err := http.ListenAndServe(address, mux); err != nil {
		log.Fatal(err)
	}
}
