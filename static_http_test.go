package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStaticAssetHandlerServesFilesWithoutDirectoryIndexes verifies the strict
// wrapper preserves normal checked-in assets while root and nested directory
// requests reveal neither filenames nor generated index pages.
func TestStaticAssetHandlerServesFilesWithoutDirectoryIndexes(t *testing.T) {
	handler := newStaticAssetHandler()

	t.Run("ordinary asset", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodGet,
			"/static/css/main.css",
			nil,
		)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		response := recorder.Result()
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf(
				"status: got %d, want %d; body=%q",
				response.StatusCode,
				http.StatusOK,
				recorder.Body.String(),
			)
		}
		if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/css") {
			t.Errorf("Content-Type: got %q, want text/css", contentType)
		}
		if recorder.Body.Len() == 0 {
			t.Error("ordinary asset response is empty")
		}
	})

	for _, path := range []string{
		"/static/",
		"/static/css/",
	} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != http.StatusNotFound {
				t.Errorf(
					"status: got %d, want %d",
					response.StatusCode,
					http.StatusNotFound,
				)
			}
			for _, assetName := range []string{
				"main.css",
				"navigation.css",
				"admin.css",
			} {
				if strings.Contains(recorder.Body.String(), assetName) {
					t.Errorf("directory response lists %q", assetName)
				}
			}
		})
	}
}
