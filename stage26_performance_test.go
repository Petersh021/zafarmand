package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Stage 26 performance budgets are deliberately expressed in uncompressed
// source bytes. Production Brotli or gzip at the HTTPS edge improves transfer
// size further, but release safety must not depend on compression hiding an
// unexpectedly large checked-in asset or server-rendered document.
const (
	// stage26KiB keeps every threshold visibly based on binary kibibytes.
	stage26KiB int64 = 1024
	// stage26PublicAssetBudget caps the largest supported public route bundle.
	stage26PublicAssetBudget = 80 * stage26KiB
	// stage26AdminAssetBudget caps the protected interface's single stylesheet.
	stage26AdminAssetBudget = 48 * stage26KiB
	// stage26FallbackHeroBudget bounds the checked-in bootstrap photograph.
	stage26FallbackHeroBudget = 256 * stage26KiB
	// stage26PublicDocumentBudget guards representative server-rendered HTML.
	stage26PublicDocumentBudget = 96 * stage26KiB
)

// TestStage26CheckedInAssetBudgets caps the CSS, JavaScript, and bootstrap hero
// shipped with one route. Managed PostgreSQL media has a separate upload and
// conditional-delivery contract tested at its repository and HTTP boundaries.
func TestStage26CheckedInAssetBudgets(t *testing.T) {
	publicSharedAssets := []string{
		"static/css/main.css",
		"static/css/navigation.css",
		// The small always-loaded fallback preserves navigation until the drawer
		// controller proves it initialized, so it belongs in every route bundle.
		"static/css/no-script.css",
		"static/js/main.js",
	}
	// Route bundles mirror the stylesheet links emitted by the real templates.
	// Catalogue pages compose discipline.css with their discipline-specific
	// rules, while detail and standalone pages load only their own sheet.
	publicRouteBundles := []struct {
		name   string
		assets []string
	}{
		{name: "home", assets: []string{"static/css/home.css"}},
		{
			name: "products",
			assets: []string{
				"static/css/discipline.css",
				"static/css/products.css",
			},
		},
		{
			name:   "product detail",
			assets: []string{"static/css/product-detail.css"},
		},
		{
			name: "interior design",
			assets: []string{
				"static/css/discipline.css",
				"static/css/interior-design.css",
			},
		},
		{
			name:   "interior detail",
			assets: []string{"static/css/interior-project-detail.css"},
		},
		{
			name: "architecture design",
			assets: []string{
				"static/css/discipline.css",
				"static/css/architecture-design.css",
			},
		},
		{
			name:   "architecture detail",
			assets: []string{"static/css/architecture-project-detail.css"},
		},
		{name: "contact", assets: []string{"static/css/contact.css"}},
	}
	sharedBytes := stage26FileBytes(t, publicSharedAssets)
	for _, bundle := range publicRouteBundles {
		t.Run(bundle.name, func(t *testing.T) {
			bundleBytes := sharedBytes + stage26FileBytes(
				t,
				bundle.assets,
			)
			if bundleBytes > stage26PublicAssetBudget {
				t.Errorf(
					"public asset bundle bytes: got %d, budget %d",
					bundleBytes,
					stage26PublicAssetBudget,
				)
			}
		})
	}

	adminBytes := stage26FileBytes(
		t,
		[]string{"static/css/admin.css"},
	)
	if adminBytes > stage26AdminAssetBudget {
		t.Errorf(
			"admin stylesheet bytes: got %d, budget %d",
			adminBytes,
			stage26AdminAssetBudget,
		)
	}

	heroBytes := stage26FileBytes(
		t,
		[]string{"static/images/home-hero-placeholder.jpg"},
	)
	if heroBytes > stage26FallbackHeroBudget {
		t.Errorf(
			"fallback hero bytes: got %d, budget %d",
			heroBytes,
			stage26FallbackHeroBudget,
		)
	}
}

// TestStage26PublicDocumentBudget renders every public page through the real
// router and catches accidental template or inline-data growth before a release.
func TestStage26PublicDocumentBudget(t *testing.T) {
	handler := newTestApplication(t).routes()
	paths := []string{
		"/",
		"/products",
		"/products/furniture-study-01",
		"/interior-design",
		"/interior-design/interior-study-01",
		"/architecture-design",
		"/architecture-design/architecture-study-01",
		"/contact",
		"/admin/login",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"status: got %d, want %d",
					recorder.Code,
					http.StatusOK,
				)
			}
			if bodyBytes := int64(recorder.Body.Len()); bodyBytes > stage26PublicDocumentBudget {
				t.Errorf(
					"document bytes: got %d, budget %d",
					bodyBytes,
					stage26PublicDocumentBudget,
				)
			}
		})
	}
}

// stage26FileBytes returns the combined regular-file size for one reviewed
// bundle and fails the calling test if an expected deployment asset is absent.
func stage26FileBytes(t *testing.T, paths []string) int64 {
	t.Helper()

	var total int64
	for _, path := range paths {
		information, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat performance asset %q: %v", path, err)
		}
		if !information.Mode().IsRegular() {
			t.Fatalf("performance asset %q is not a regular file", path)
		}
		total += information.Size()
	}

	return total
}
