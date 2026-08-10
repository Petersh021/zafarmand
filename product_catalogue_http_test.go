package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestProductsRouteRendersDatabaseEmptyState proves that zero published rows is
// a successful and truthful catalogue state. It must not resurrect the old
// in-memory placeholder products in production behavior.
func TestProductsRouteRendersDatabaseEmptyState(t *testing.T) {
	reader := newRecordingProductCatalogueReader()
	reader.setProducts(nil)
	app := newTestApplicationWithProductCatalogueReader(t, reader)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/products", nil)

	app.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code: got %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(
		body,
		"Product entries are being prepared for publication.",
	) {
		t.Error("empty catalogue does not explain its publication state")
	}
	if strings.Contains(body, `class="product-preview"`) {
		t.Error("empty catalogue renders a fictional Product preview")
	}

	assertProductReadDeadline(t, reader.listCallSnapshot())
}

// TestProductsRouteFailsClosedOnReaderProblems verifies both dependency errors
// and malformed substituted results. Neither condition may leak implementation
// text or render partially trusted catalogue data.
func TestProductsRouteFailsClosedOnReaderProblems(t *testing.T) {
	privateDetail := "private Product database diagnostic"
	tests := []struct {
		// name identifies the dependency-contract failure.
		name string
		// arrange configures the reader for that failure.
		arrange func(*recordingProductCatalogueReader)
	}{
		{
			name: "database failure",
			arrange: func(reader *recordingProductCatalogueReader) {
				reader.setErrors(errors.New(privateDetail), nil)
			},
		},
		{
			name: "nonconsecutive catalogue numbers",
			arrange: func(reader *recordingProductCatalogueReader) {
				reader.setProducts([]catalogueProduct{
					{
						ID:              7,
						CatalogueNumber: 2,
						Slug:            "stored-product",
						Name:            "Stored Product",
						Category:        "Objects",
					},
				})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingProductCatalogueReader()
			test.arrange(reader)
			app := newTestApplicationWithProductCatalogueReader(t, reader)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/products", nil)

			app.routes().ServeHTTP(recorder, request)

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf(
					"status code: got %d, want 503",
					recorder.Code,
				)
			}
			if recorder.Body.String() != "service temporarily unavailable\n" {
				t.Errorf("unexpected generic response: %q", recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), privateDetail) {
				t.Error("response exposes Product dependency diagnostic")
			}

			assertProductReadDeadline(t, reader.listCallSnapshot())
		})
	}
}

// TestProductDetailHandlerValidatesBeforeReading verifies malformed path values
// receive a normal 404 without spending a database query or revealing whether
// a similarly spelled unpublished row exists.
func TestProductDetailHandlerValidatesBeforeReading(t *testing.T) {
	tests := []string{
		"",
		"Uppercase-product",
		"leading--separator",
		"trailing-",
		strings.Repeat("a", productSlugMaximumLength+1),
	}

	for _, slug := range tests {
		t.Run(slug, func(t *testing.T) {
			reader := newRecordingProductCatalogueReader()
			app := newTestApplicationWithProductCatalogueReader(t, reader)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodGet,
				"/products/placeholder",
				nil,
			)
			request.SetPathValue("slug", slug)

			app.productDetailHandler(recorder, request)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf(
					"status code: got %d, want 404",
					recorder.Code,
				)
			}
			if calls := reader.findCallSnapshot(); len(calls) != 0 {
				t.Errorf("invalid slug reached reader %d time(s)", len(calls))
			}
		})
	}
}

// TestProductDetailRouteMapsRepositoryOutcomes verifies the public distinction
// is deliberately narrow: published records render, missing or unpublished
// records are 404, and operational failures become a redacted 503.
func TestProductDetailRouteMapsRepositoryOutcomes(t *testing.T) {
	privateDetail := "unsafe product driver detail"
	tests := []struct {
		// name identifies one safe repository category.
		name string
		// slug is the canonical path value supplied to the reader.
		slug string
		// findErr configures an optional repository outcome.
		findErr error
		// expectedStatus is the public HTTP mapping.
		expectedStatus int
	}{
		{
			name:           "published record",
			slug:           "furniture-study-01",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "not found or unpublished",
			slug:           "unpublished-product",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "database failure",
			slug:           "furniture-study-01",
			findErr:        errors.New(privateDetail),
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name:           "reader contract failure",
			slug:           "furniture-study-01",
			findErr:        errProductCatalogueInvalidQuery,
			expectedStatus: http.StatusServiceUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := newRecordingProductCatalogueReader()
			reader.setErrors(nil, test.findErr)
			app := newTestApplicationWithProductCatalogueReader(t, reader)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodGet,
				"/products/"+test.slug,
				nil,
			)

			app.routes().ServeHTTP(recorder, request)

			if recorder.Code != test.expectedStatus {
				t.Fatalf(
					"status code: got %d, want %d",
					recorder.Code,
					test.expectedStatus,
				)
			}
			if strings.Contains(recorder.Body.String(), privateDetail) {
				t.Error("detail response exposes Product dependency diagnostic")
			}
			if test.expectedStatus == http.StatusServiceUnavailable &&
				recorder.Body.String() != "service temporarily unavailable\n" {
				t.Errorf("unexpected generic response: %q", recorder.Body.String())
			}

			calls := reader.findCallSnapshot()
			if len(calls) != 1 {
				t.Fatalf("detail reader call count: got %d, want 1", len(calls))
			}
			if calls[0].Slug != test.slug {
				t.Errorf("detail reader slug: got %q, want %q", calls[0].Slug, test.slug)
			}
			assertProductFindDeadline(t, calls)
		})
	}
}

// TestProductHandlersRejectMissingRuntimeDependency proves the request-time
// guard protects manually assembled applications even though newApplication
// already rejects this configuration at startup.
func TestProductHandlersRejectMissingRuntimeDependency(t *testing.T) {
	app := newTestApplication(t)
	app.products = nil

	for _, path := range []string{
		"/products",
		"/products/furniture-study-01",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)

			app.routes().ServeHTTP(recorder, request)

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf(
					"status code: got %d, want 503",
					recorder.Code,
				)
			}
		})
	}
}

// assertProductReadDeadline checks the one-call list contract without relying
// on exact wall-clock scheduling.
func assertProductReadDeadline(
	t *testing.T,
	calls []recordingProductListCall,
) {
	t.Helper()

	if len(calls) != 1 {
		t.Fatalf("Product list call count: got %d, want 1", len(calls))
	}
	if !calls[0].HasDeadline || !calls[0].Deadline.After(time.Now()) {
		t.Error("Product list call does not have a future deadline")
	}
}

// assertProductFindDeadline checks the one-call detail contract without tying
// the test to a precise scheduler delay.
func assertProductFindDeadline(
	t *testing.T,
	calls []recordingProductFindCall,
) {
	t.Helper()

	if len(calls) != 1 {
		t.Fatalf("Product detail call count: got %d, want 1", len(calls))
	}
	if !calls[0].HasDeadline || !calls[0].Deadline.After(time.Now()) {
		t.Error("Product detail call does not have a future deadline")
	}
}
