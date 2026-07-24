package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
)

type application struct {
	templates map[string]*template.Template
}

type pageData struct {
	Title       string
	CurrentPath string
}

func newApplication() (*application, error) {
	templateCache, err := newTemplateCache()
	if err != nil {
		return nil, err
	}

	app := &application{
		templates: templateCache,
	}

	return app, nil
}

func newTemplateCache() (
	map[string]*template.Template,
	error,
) {
	cache := make(map[string]*template.Template)

	pages, err := filepath.Glob(
		"./templates/pages/*.html",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"find page templates: %w",
			err,
		)
	}

	if len(pages) == 0 {
		return nil, fmt.Errorf(
			"no page templates found",
		)
	}

	for _, page := range pages {
		pageName := filepath.Base(page)

		templateSet, err := template.ParseFiles(
			"./templates/base.html",
			page,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"parse template %s: %w",
				pageName,
				err,
			)
		}

		cache[pageName] = templateSet
	}

	return cache, nil
}

func (app *application) render(
	w http.ResponseWriter,
	status int,
	pageName string,
	data pageData,
) {
	templateSet, exists := app.templates[pageName]
	if !exists {
		log.Printf(
			"template %q does not exist",
			pageName,
		)

		http.Error(
			w,
			"internal server error",
			http.StatusInternalServerError,
		)

		return
	}

	var buffer bytes.Buffer

	err := templateSet.ExecuteTemplate(
		&buffer,
		"base",
		data,
	)
	if err != nil {
		log.Printf(
			"could not execute template %q: %v",
			pageName,
			err,
		)

		http.Error(
			w,
			"internal server error",
			http.StatusInternalServerError,
		)

		return
	}

	w.Header().Set(
		"Content-Type",
		"text/html; charset=utf-8",
	)

	w.WriteHeader(status)

	if _, err := buffer.WriteTo(w); err != nil {
		log.Printf(
			"could not write response: %v",
			err,
		)
	}
}
