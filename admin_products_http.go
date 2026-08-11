package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

const (
	// adminProductNavigationPath is the canonical parent route and trusted active-
	// navigation value shared by Product list and detail pages.
	adminProductNavigationPath = "/admin/products"
	// adminProductTimeLayout is display-ready UTC text prepared outside templates.
	adminProductTimeLayout = "02 Jan 2006, 15:04 UTC"
)

// errAdminProductReaderRequired prevents protected Product routes from starting
// without their read dependency.
var errAdminProductReaderRequired = errors.New(
	"create application: admin product reader is required",
)

// errAdminProductWriterRequired prevents protected Product mutation routes
// from starting without their explicit text/cover write dependency.
var errAdminProductWriterRequired = errors.New(
	"create application: admin product writer is required",
)

// adminProductListPageData is the complete protected Product-list template
// contract. An empty slice truthfully renders an empty database state.
type adminProductListPageData struct {
	// NewPath is the trusted protected route for creating a Product.
	NewPath string
	// Items contains every validated Product in editorial order.
	Items []adminProductSummaryPageData
	// EmptyMessage explains why no cards appear after a successful empty read.
	EmptyMessage string
}

// adminProductSummaryPageData contains display-ready values for one list card.
type adminProductSummaryPageData struct {
	// Reference is a stable administrative label derived from the internal ID.
	Reference string
	// Path is the canonical protected detail URL.
	Path string
	// Name is escaped contextually by html/template.
	Name string
	// Slug is shown as stored text so administrators can review the public path.
	Slug string
	// Category is the stored product-family label.
	Category string
	// SortOrder is formatted base-10 interface text.
	SortOrder string
	// StatusLabel is trusted visible text from the closed lifecycle vocabulary.
	StatusLabel string
	// StatusClass is one trusted CSS modifier chosen by Go.
	StatusClass string
	// UpdatedAtISO is the machine-readable UTC timestamp for a time element.
	UpdatedAtISO string
	// UpdatedAtLabel is concise, human-readable UTC text.
	UpdatedAtLabel string
}

// adminProductDetailPageData contains migration-6 read fields and trusted
// presentation/navigation values. Mutations remain on separate forms.
type adminProductDetailPageData struct {
	// Reference is the stable administrative label derived from the internal ID.
	Reference string
	// EditPath is the canonical protected form URL for this Product.
	EditPath string
	// Name is the page's primary Product heading.
	Name string
	// Slug is the stored canonical route segment.
	Slug string
	// Category is the stored product-family label.
	Category string
	// SortOrder is formatted base-10 interface text.
	SortOrder string
	// Version is the positive revision shown as concurrency context.
	Version string
	// StatusLabel is trusted visible lifecycle text.
	StatusLabel string
	// StatusClass is one trusted CSS modifier selected by Go.
	StatusClass string
	// VisibilityMessage explains the public consequence of the current status.
	VisibilityMessage string
	// PublicPath is present only for a published record.
	PublicPath string
	// Description is optional reviewed long-form public copy.
	Description string
	// Material is an optional reviewed material fact.
	Material string
	// Dimensions is an optional reviewed dimensions fact.
	Dimensions string
	// Cover contains protected preview metadata, or nil when no cover exists.
	Cover *productCoverPageData
	// CoverManagementPath opens the authenticated upload-or-replace form.
	CoverManagementPath string
	// CreatedAtISO is the machine-readable UTC creation timestamp.
	CreatedAtISO string
	// CreatedAtLabel is concise, human-readable UTC creation text.
	CreatedAtLabel string
	// UpdatedAtISO is the machine-readable UTC update timestamp.
	UpdatedAtISO string
	// UpdatedAtLabel is concise, human-readable UTC update text.
	UpdatedAtLabel string
}

// adminProductListHandler reads every Product state for authenticated owner or
// editor roles and renders the isolated private catalogue view.
func (app *application) adminProductListHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if r.URL.EscapedPath() != adminProductNavigationPath {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid product catalogue request", http.StatusBadRequest)

		return
	}
	if app == nil || app.adminProducts == nil {
		log.Print("admin product reader unavailable")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	products, err := app.adminProducts.List(ctx)
	cancel()
	if err != nil || !isValidAdminProductList(products) {
		// Logs retain only a fixed event category; Product text, SQL, path input,
		// and driver diagnostics remain absent.
		log.Print("admin product list failed")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	items := make([]adminProductSummaryPageData, 0, len(products))
	for _, product := range products {
		item, valid := newAdminProductSummaryPageData(product)
		if !valid {
			log.Print("admin product list mapping failed")
			http.Error(
				w,
				"service temporarily unavailable",
				http.StatusServiceUnavailable,
			)

			return
		}
		items = append(items, item)
	}

	data := newAuthenticatedAdminPageData(
		"Products",
		adminProductNavigationPath,
		requestIdentity,
	)
	data.ProductList = &adminProductListPageData{
		NewPath:      adminProductNewPath,
		Items:        items,
		EmptyMessage: "No Products have been created yet.",
	}

	app.renderAdmin(w, http.StatusOK, "products.html", data)
}

// adminProductDetailHandler accepts one canonical positive ID after the shared
// authentication and explicit Product-role middleware have run.
func (app *application) adminProductDetailHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid product request", http.StatusBadRequest)

		return
	}

	productID, valid := parseCanonicalPositiveInt64(r.PathValue("id"))
	if !valid {
		http.NotFound(w, r)

		return
	}
	canonicalPath := adminProductPath(productID)
	if r.URL.EscapedPath() != canonicalPath {
		// ServeMux decodes escaped path segments before PathValue. Requiring the
		// exact escaped path keeps one visible URL spelling per protected record.
		http.NotFound(w, r)

		return
	}
	if app == nil || app.adminProducts == nil {
		log.Print("admin product reader unavailable")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	product, err := app.adminProducts.FindByID(ctx, productID)
	cancel()
	if errors.Is(err, errAdminProductNotFound) {
		http.NotFound(w, r)

		return
	}
	if err != nil {
		log.Print("admin product detail failed")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	detail, valid := newAdminProductDetailPageData(product)
	if !valid || product.ID != productID {
		log.Print("admin product detail mapping failed")
		http.Error(
			w,
			"service temporarily unavailable",
			http.StatusServiceUnavailable,
		)

		return
	}

	data := newAuthenticatedAdminPageData(
		"Product detail",
		adminProductNavigationPath,
		requestIdentity,
	)
	data.ProductDetail = &detail

	app.renderAdmin(w, http.StatusOK, "product-detail.html", data)
}

// contextWithAdminRepositoryTimeout centralizes the existing private database
// deadline while retaining the request as its cancellation parent.
func contextWithAdminRepositoryTimeout(
	r *http.Request,
) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), adminRepositoryTimeout)
}

// isValidAdminProductList rechecks ordering, identity, slug uniqueness, and
// stored-field validity at the handler-facing interface boundary.
func isValidAdminProductList(products []adminProductRecord) bool {
	seenIDs := make(map[int64]struct{}, len(products))
	seenSlugs := make(map[string]struct{}, len(products))
	var previous adminProductRecord
	for index, product := range products {
		if !isValidStoredAdminProduct(product) ||
			(index > 0 && !adminProductFollows(product, previous)) {
			return false
		}
		if _, exists := seenIDs[product.ID]; exists {
			return false
		}
		if _, exists := seenSlugs[product.Slug]; exists {
			return false
		}

		seenIDs[product.ID] = struct{}{}
		seenSlugs[product.Slug] = struct{}{}
		previous = product
	}

	return true
}

// newAdminProductSummaryPageData validates and converts one repository record
// into display-ready list values.
func newAdminProductSummaryPageData(
	product adminProductRecord,
) (adminProductSummaryPageData, bool) {
	if !isValidStoredAdminProduct(product) {
		return adminProductSummaryPageData{}, false
	}

	statusLabel, statusClass, _, valid := adminProductStatusPresentation(
		product.PublicationStatus,
	)
	if !valid {
		return adminProductSummaryPageData{}, false
	}
	updatedAt := product.UpdatedAt.UTC()

	return adminProductSummaryPageData{
		Reference:      adminProductReference(product.ID),
		Path:           adminProductPath(product.ID),
		Name:           product.Name,
		Slug:           product.Slug,
		Category:       product.Category,
		SortOrder:      strconv.Itoa(product.SortOrder),
		StatusLabel:    statusLabel,
		StatusClass:    statusClass,
		UpdatedAtISO:   updatedAt.Format(time.RFC3339),
		UpdatedAtLabel: updatedAt.Format(adminProductTimeLayout),
	}, true
}

// newAdminProductDetailPageData validates and converts one repository record
// into the complete read-only detail and edit-navigation contract.
func newAdminProductDetailPageData(
	product adminProductRecord,
) (adminProductDetailPageData, bool) {
	if !isValidStoredAdminProduct(product) {
		return adminProductDetailPageData{}, false
	}

	statusLabel, statusClass, visibilityMessage, valid :=
		adminProductStatusPresentation(product.PublicationStatus)
	if !valid {
		return adminProductDetailPageData{}, false
	}
	createdAt := product.CreatedAt.UTC()
	updatedAt := product.UpdatedAt.UTC()
	detail := adminProductDetailPageData{
		Reference:         adminProductReference(product.ID),
		EditPath:          adminProductPath(product.ID) + "/edit",
		Name:              product.Name,
		Slug:              product.Slug,
		Category:          product.Category,
		SortOrder:         strconv.Itoa(product.SortOrder),
		Version:           strconv.FormatInt(product.Version, 10),
		StatusLabel:       statusLabel,
		StatusClass:       statusClass,
		VisibilityMessage: visibilityMessage,
		Description:       product.Description,
		Material:          product.Material,
		Dimensions:        product.Dimensions,
		CoverManagementPath: adminProductCoverPath(
			product.ID,
		),
		CreatedAtISO:   createdAt.Format(time.RFC3339),
		CreatedAtLabel: createdAt.Format(adminProductTimeLayout),
		UpdatedAtISO:   updatedAt.Format(time.RFC3339),
		UpdatedAtLabel: updatedAt.Format(adminProductTimeLayout),
	}
	if product.PublicationStatus == publishedProductStatus {
		detail.PublicPath = productDetailPath(product.Slug)
	}
	if product.Cover != nil {
		cover := productCoverPageData{
			Path: adminProductCoverAssetPath(
				product.ID,
				product.Cover.Version,
			),
			AltText: product.Cover.AltText,
			Caption: product.Cover.Caption,
			Width:   strconv.Itoa(product.Cover.Width),
			Height:  strconv.Itoa(product.Cover.Height),
		}
		detail.Cover = &cover
	}

	return detail, true
}

// adminProductStatusPresentation translates one closed stored state into
// trusted visible text, a CSS suffix, and an explicit visibility explanation.
func adminProductStatusPresentation(
	status string,
) (string, string, string, bool) {
	switch status {
	case productPublicationStatusDraft:
		return "Draft", "draft", "This Product is not visible on the public website.", true
	case publishedProductStatus:
		return "Published", "published", "This Product is visible in the public catalogue.", true
	case productPublicationStatusArchived:
		return "Archived", "archived", "This Product remains stored but is hidden from the public website.", true
	default:
		return "", "", "", false
	}
}

// adminProductReference formats a positive internal identity as concise
// administrative interface text.
func adminProductReference(id int64) string {
	return fmt.Sprintf("P-%04d", id)
}

// adminProductPath constructs the canonical protected detail URL from a
// validated positive identity.
func adminProductPath(id int64) string {
	return adminProductNavigationPath + "/" + strconv.FormatInt(id, 10)
}
