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
	// stage26InteriorHeroBudget keeps the route-owned photographic LCP below the
	// same conservative transfer ceiling as the Homepage fallback.
	stage26InteriorHeroBudget = 256 * stage26KiB
	// stage26InteriorPreviewBudget caps each lazy-loaded reference card image.
	stage26InteriorPreviewBudget = 192 * stage26KiB
	// stage26ArchitectureHeroBudget bounds the route-owned Architecture LCP.
	stage26ArchitectureHeroBudget = 256 * stage26KiB
	// stage26ArchitecturePreviewBudget caps each below-fold concept image.
	stage26ArchitecturePreviewBudget = 192 * stage26KiB
	// stage26ProductsHeroBudget bounds the route-owned Products LCP.
	stage26ProductsHeroBudget = 256 * stage26KiB
	// stage26ProductsPreviewBudget caps each collection or Product concept still.
	stage26ProductsPreviewBudget = 192 * stage26KiB
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
	// Each photographic discipline landing owns one route sheet, while detail
	// and standalone pages continue to load only their focused presentation.
	publicRouteBundles := []struct {
		name   string
		assets []string
	}{
		{name: "home", assets: []string{"static/css/home.css"}},
		{
			name: "products",
			assets: []string{
				"static/css/products.css",
				"static/css/reference-menu.css",
			},
		},
		{
			name: "product detail",
			assets: []string{
				"static/css/product-detail.css",
				"static/css/reference-menu.css",
			},
		},
		{
			name:   "interior design",
			assets: []string{"static/css/interior-design.css"},
		},
		{
			name: "interior detail",
			assets: []string{
				"static/css/interior-project-detail.css",
				"static/css/reference-menu.css",
			},
		},
		{
			name: "architecture design",
			assets: []string{
				"static/css/architecture-design.css",
				"static/css/reference-menu.css",
			},
		},
		{
			name: "architecture detail",
			assets: []string{
				"static/css/architecture-project-detail.css",
				"static/css/reference-menu.css",
			},
		},
		{
			name: "contact",
			assets: []string{
				"static/css/contact.css",
				"static/css/reference-menu.css",
			},
		},
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

	// The dedicated Interior hero is eager, while its four concept previews are
	// lazy. Individual limits prevent one replacement image from quietly
	// dominating the initial page or below-fold transfer.
	interiorHeroBytes := stage26FileBytes(
		t,
		[]string{"static/images/interior-design/interior-hero.jpg"},
	)
	if interiorHeroBytes > stage26InteriorHeroBudget {
		t.Errorf(
			"Interior hero bytes: got %d, budget %d",
			interiorHeroBytes,
			stage26InteriorHeroBudget,
		)
	}

	interiorPreviews := []string{
		"static/images/interior-design/hillside-residence.jpg",
		"static/images/interior-design/karimi-apartment.jpg",
		"static/images/interior-design/noor-office.jpg",
		"static/images/interior-design/atrium-lobby.jpg",
	}
	for _, path := range interiorPreviews {
		information, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat Interior preview %q: %v", path, err)
		}
		if !information.Mode().IsRegular() {
			t.Fatalf("Interior preview %q is not a regular file", path)
		}
		if information.Size() > stage26InteriorPreviewBudget {
			t.Errorf(
				"Interior preview %q bytes: got %d, budget %d",
				path,
				information.Size(),
				stage26InteriorPreviewBudget,
			)
		}
	}

	// Architecture follows the same loading contract: one eager hero and four
	// individually bounded lazy concept images below the first viewport.
	architectureHeroBytes := stage26FileBytes(
		t,
		[]string{"static/images/architecture-design/architecture-hero.jpg"},
	)
	if architectureHeroBytes > stage26ArchitectureHeroBudget {
		t.Errorf(
			"Architecture hero bytes: got %d, budget %d",
			architectureHeroBytes,
			stage26ArchitectureHeroBudget,
		)
	}

	architecturePreviews := []string{
		"static/images/architecture-design/mountain-house.jpg",
		"static/images/architecture-design/terra-office-building.jpg",
		"static/images/architecture-design/silk-museum.jpg",
		"static/images/architecture-design/coastal-retreat.jpg",
	}
	for _, path := range architecturePreviews {
		information, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat Architecture preview %q: %v", path, err)
		}
		if !information.Mode().IsRegular() {
			t.Fatalf("Architecture preview %q is not a regular file", path)
		}
		if information.Size() > stage26ArchitecturePreviewBudget {
			t.Errorf(
				"Architecture preview %q bytes: got %d, budget %d",
				path,
				information.Size(),
				stage26ArchitecturePreviewBudget,
			)
		}
	}

	// Products follows the same loading contract: one eager LCP and nine
	// individually bounded, lazy below-fold collection/Product concept images.
	productsHeroBytes := stage26FileBytes(
		t,
		[]string{"static/images/products/products-hero.jpg"},
	)
	if productsHeroBytes > stage26ProductsHeroBudget {
		t.Errorf(
			"Products hero bytes: got %d, budget %d",
			productsHeroBytes,
			stage26ProductsHeroBudget,
		)
	}

	productsPreviews := []string{
		"static/images/products/collection-furniture.jpg",
		"static/images/products/collection-lighting.jpg",
		"static/images/products/collection-accessories.jpg",
		"static/images/products/collection-materials.jpg",
		"static/images/products/pivot-lounge-chair.jpg",
		"static/images/products/noir-pendant-lamp.jpg",
		"static/images/products/travertine-coffee-table.jpg",
		"static/images/products/bronze-bowl.jpg",
		"static/images/products/terra-vase.jpg",
	}
	for _, path := range productsPreviews {
		information, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat Products preview %q: %v", path, err)
		}
		if !information.Mode().IsRegular() {
			t.Fatalf("Products preview %q is not a regular file", path)
		}
		if information.Size() > stage26ProductsPreviewBudget {
			t.Errorf(
				"Products preview %q bytes: got %d, budget %d",
				path,
				information.Size(),
				stage26ProductsPreviewBudget,
			)
		}
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

	// The normal application fixture contains published Products. Render a
	// second route with an empty reader so the larger 4+5 reference branch is
	// also held to the same document ceiling.
	t.Run("/products reference composition", func(t *testing.T) {
		reader := newRecordingProductCatalogueReader()
		reader.setProducts(nil)
		handler := newTestApplicationWithProductCatalogueReader(t, reader).routes()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(
			recorder,
			httptest.NewRequest(http.MethodGet, "/products", nil),
		)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200", recorder.Code)
		}
		if bodyBytes := int64(recorder.Body.Len()); bodyBytes > stage26PublicDocumentBudget {
			t.Errorf(
				"reference document bytes: got %d, budget %d",
				bodyBytes,
				stage26PublicDocumentBudget,
			)
		}
	})
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
