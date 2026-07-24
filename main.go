package main

import (
	"fmt"
	"log"
	"net/http"
)

func homehandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-type", "text/html; charset=utf-8")
	if _, err := fmt.Fprint(w, `
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Zafarmand Studio</title>
</head>
<body>
	<h1>Zafarmand Studio</h1>
	<p>Products, Interior Design, and Architecture Design</p>
</body>
</html>
`); err != nil {
		log.Printf("could not write response: %v", err)
	}
}

func main() {
	http.HandleFunc("/", homehandler)

	const address = ":8080"

	log.Printf("Zafarmand website is running at http://localhost%s", address)

	err := http.ListenAndServe(address, nil)
	if err != nil {
		log.Fatal(err)
	}
}
