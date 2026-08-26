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

// TestStaticAssetHandlerRevalidatesUnversionedFiles protects the Stage 26
// performance contract: checked-in asset URLs remain safe to update while a
// matching Last-Modified request avoids transferring the file again.
func TestStaticAssetHandlerRevalidatesUnversionedFiles(t *testing.T) {
	handler := newStaticAssetHandler()
	initialRequest := httptest.NewRequest(
		http.MethodGet,
		"/static/css/main.css",
		nil,
	)
	initialRecorder := httptest.NewRecorder()
	handler.ServeHTTP(initialRecorder, initialRequest)

	if initialRecorder.Code != http.StatusOK {
		t.Fatalf(
			"initial status: got %d, want %d",
			initialRecorder.Code,
			http.StatusOK,
		)
	}
	if cacheControl := initialRecorder.Header().Get("Cache-Control"); cacheControl != staticCacheControl {
		t.Errorf(
			"Cache-Control: got %q, want %q",
			cacheControl,
			staticCacheControl,
		)
	}
	lastModified := initialRecorder.Header().Get("Last-Modified")
	if lastModified == "" {
		t.Fatal("initial response omits Last-Modified")
	}

	conditionalRequest := httptest.NewRequest(
		http.MethodGet,
		"/static/css/main.css",
		nil,
	)
	conditionalRequest.Header.Set("If-Modified-Since", lastModified)
	conditionalRecorder := httptest.NewRecorder()
	handler.ServeHTTP(conditionalRecorder, conditionalRequest)

	if conditionalRecorder.Code != http.StatusNotModified {
		t.Errorf(
			"conditional status: got %d, want %d",
			conditionalRecorder.Code,
			http.StatusNotModified,
		)
	}
	if conditionalRecorder.Body.Len() != 0 {
		t.Errorf(
			"conditional body bytes: got %d, want 0",
			conditionalRecorder.Body.Len(),
		)
	}
	if cacheControl := conditionalRecorder.Header().Get("Cache-Control"); cacheControl != staticCacheControl {
		t.Errorf(
			"conditional Cache-Control: got %q, want %q",
			cacheControl,
			staticCacheControl,
		)
	}

	// HEAD retains the representation metadata without transferring CSS bytes.
	headRequest := httptest.NewRequest(
		http.MethodHead,
		"/static/css/main.css",
		nil,
	)
	headRecorder := httptest.NewRecorder()
	handler.ServeHTTP(headRecorder, headRequest)
	if headRecorder.Code != http.StatusOK || headRecorder.Body.Len() != 0 {
		t.Errorf(
			"HEAD response: status=%d body-bytes=%d",
			headRecorder.Code,
			headRecorder.Body.Len(),
		)
	}
	if headRecorder.Header().Get("Last-Modified") != lastModified ||
		headRecorder.Header().Get("Cache-Control") != staticCacheControl ||
		headRecorder.Header().Get("Content-Length") == "" {
		t.Errorf("HEAD metadata: got %#v", headRecorder.Header())
	}

	// A stale validator must receive the current representation rather than 304.
	staleRequest := httptest.NewRequest(
		http.MethodGet,
		"/static/css/main.css",
		nil,
	)
	staleRequest.Header.Set(
		"If-Modified-Since",
		"Thu, 01 Jan 1970 00:00:00 GMT",
	)
	staleRecorder := httptest.NewRecorder()
	handler.ServeHTTP(staleRecorder, staleRequest)
	if staleRecorder.Code != http.StatusOK || staleRecorder.Body.Len() == 0 {
		t.Errorf(
			"stale validator response: status=%d body-bytes=%d",
			staleRecorder.Code,
			staleRecorder.Body.Len(),
		)
	}
}
