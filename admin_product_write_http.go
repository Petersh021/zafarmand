package main

import (
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
)

const (
	// adminProductNewPath is the one canonical GET address for an empty form.
	adminProductNewPath = "/admin/products/new"
	// adminProductCreatePath is the fixed collection POST destination.
	adminProductCreatePath = "/admin/products"
	// adminProductFormMaximumBytes accommodates every semantically valid field
	// even when four-byte UTF-8 characters expand to twelve bytes under percent
	// encoding. The reviewed fixed cap still rejects an unbounded request body.
	adminProductFormMaximumBytes = 128 * 1024
)

// adminProductFormPageData is the complete create/edit template contract. All
// request text remains ordinary escaped data; paths and option values are built
// only from trusted constants or validated numeric identities.
type adminProductFormPageData struct {
	// Eyebrow distinguishes a new record from an existing Product revision.
	Eyebrow string
	// Heading is the page's single primary heading.
	Heading string
	// Introduction explains what a successful save affects.
	Introduction string
	// Action is the canonical POST destination.
	Action string
	// CancelPath returns to the catalogue or existing detail without mutation.
	CancelPath string
	// SubmitLabel is the visible and accessible native button name.
	SubmitLabel string
	// IsEdit selects the hidden revision field and edit-specific guidance.
	IsEdit bool
	// Version is the canonical positive revision rendered only for an edit.
	Version string
	// Values restores administrator-entered visible fields after validation.
	Values adminProductFormValues
	// Errors contains one safe explanation for each rejected visible field.
	Errors adminProductFormErrors
	// HasErrors selects the accessible error summary.
	HasErrors bool
	// StatusOptions is the closed, trusted publication-state vocabulary.
	StatusOptions []adminProductStatusOptionPageData
	// HasValidStatus keeps an invalid submitted value from silently selecting a
	// different lifecycle option when the form is rendered again.
	HasValidStatus bool
}

// adminProductFormValues contains only visible administrator-controlled text.
// It is a presentation shape, not a trusted persistence record.
type adminProductFormValues struct {
	// Slug is restored to the public-path field.
	Slug string
	// Name is restored to the catalogue-heading field.
	Name string
	// Category is restored to the Product-family field.
	Category string
	// SortOrder is restored as entered so a formatting error remains visible.
	SortOrder string
	// PublicationStatus is compared only with trusted option values.
	PublicationStatus string
	// Description restores optional long-form public copy exactly as entered.
	Description string
	// Material restores the optional reviewed material fact.
	Material string
	// Dimensions restores the optional reviewed dimensions fact.
	Dimensions string
}

// adminProductFormErrors stores field-level semantic validation messages. Empty
// values mean the corresponding visible field passed validation.
type adminProductFormErrors struct {
	// Slug explains the canonical route-segment rule or a uniqueness conflict.
	Slug string
	// Name explains the required trimmed length boundary.
	Name string
	// Category explains the required trimmed length boundary.
	Category string
	// SortOrder explains the positive 32-bit integer boundary.
	SortOrder string
	// PublicationStatus explains the closed lifecycle choice.
	PublicationStatus string
	// Description explains the optional trimming and length boundary.
	Description string
	// Material explains the optional trimming and length boundary.
	Material string
	// Dimensions explains the optional trimming and length boundary.
	Dimensions string
}

// adminProductStatusOptionPageData pairs one trusted machine value with visible
// text and its server-selected state.
type adminProductStatusOptionPageData struct {
	// Value is one exact database lifecycle value.
	Value string
	// Label is its trusted administrator-facing name.
	Label string
	// Selected is true only when the current form value exactly matches Value.
	Selected bool
}

// adminProductConflictPageData gives a stale editor safe fixed navigation
// without echoing submitted values or revealing the newer database record.
type adminProductConflictPageData struct {
	// DetailPath opens the current protected Product record.
	DetailPath string
	// EditPath fetches a fresh form carrying the latest revision.
	EditPath string
	// ActionLabel optionally specializes the primary recovery-link text.
	ActionLabel string
	// Guidance optionally specializes the fixed recovery explanation.
	Guidance string
}

// adminProductNewHandler renders a database-free Draft form. Opening or
// refreshing this GET never creates a Product.
func (app *application) adminProductNewHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if r.URL.EscapedPath() != adminProductNewPath {
		http.NotFound(w, r)

		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Product form request", http.StatusBadRequest)

		return
	}

	form := newAdminProductFormPageData(
		false,
		adminProductCreatePath,
		adminProductNavigationPath,
		"",
		adminProductFormValues{
			SortOrder:         "1",
			PublicationStatus: productPublicationStatusDraft,
			Description:       "",
			Material:          "",
			Dimensions:        "",
		},
		adminProductFormErrors{},
	)
	data := newAuthenticatedAdminPageData(
		"Create Product",
		adminProductNavigationPath,
		requestIdentity,
	)
	data.ProductForm = &form

	app.renderAdmin(w, http.StatusOK, "product-form.html", data)
}

// adminProductEditHandler reads one current Product revision and renders it as
// a form. The GET is strictly read-only and cannot publish or update a record.
func (app *application) adminProductEditHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		http.Error(w, "invalid Product form request", http.StatusBadRequest)

		return
	}
	productID, _, valid := canonicalAdminProductWritePath(
		r,
		"/edit",
	)
	if !valid {
		http.NotFound(w, r)

		return
	}
	if app == nil || app.adminProducts == nil {
		log.Print("admin product reader unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	product, err := app.adminProducts.FindByID(ctx, productID)
	cancel()
	if errors.Is(err, errAdminProductNotFound) {
		http.NotFound(w, r)

		return
	}
	if err != nil || product.ID != productID || !isValidStoredAdminProduct(product) {
		log.Print("admin product edit read failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	detailPath := adminProductPath(productID)
	form := newAdminProductFormPageData(
		true,
		detailPath,
		detailPath,
		strconv.FormatInt(product.Version, 10),
		adminProductFormValues{
			Slug:              product.Slug,
			Name:              product.Name,
			Category:          product.Category,
			SortOrder:         strconv.Itoa(product.SortOrder),
			PublicationStatus: product.PublicationStatus,
			Description:       product.Description,
			Material:          product.Material,
			Dimensions:        product.Dimensions,
		},
		adminProductFormErrors{},
	)
	data := newAuthenticatedAdminPageData(
		"Edit Product",
		adminProductNavigationPath,
		requestIdentity,
	)
	data.ProductForm = &form

	app.renderAdmin(w, http.StatusOK, "product-form.html", data)
}

// adminProductCreateHandler validates one exact native form, inserts through
// the narrow writer, and redirects to the canonical detail route.
func (app *application) adminProductCreateHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}
	if !adminProductMutationRequestIsCanonical(w, r, adminProductCreatePath) {
		return
	}

	form, parsed := parseStrictAdminFormWithMaximum(
		w,
		r,
		[]string{
			"csrf_token",
			"slug",
			"name",
			"category",
			"sort_order",
			"publication_status",
			"description",
			"material",
			"dimensions",
		},
		adminProductFormMaximumBytes,
	)
	if !parsed {
		return
	}
	if !adminSessionCSRFTokenIsValid(
		requestIdentity.CSRFToken,
		form.Get("csrf_token"),
	) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	values := adminProductValuesFromForm(form)
	input, validationErrors := validateAdminProductFormValues(values)
	if validationErrors != (adminProductFormErrors{}) {
		app.renderAdminProductFormResponse(
			w,
			requestIdentity,
			http.StatusUnprocessableEntity,
			false,
			adminProductCreatePath,
			adminProductNavigationPath,
			"",
			values,
			validationErrors,
		)

		return
	}
	if app == nil || app.adminProductWrites == nil {
		log.Print("admin product writer unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	result, err := app.adminProductWrites.Create(ctx, input)
	cancel()
	if errors.Is(err, errAdminProductSlugConflict) {
		validationErrors.Slug = "That slug is already used by another Product."
		app.renderAdminProductFormResponse(
			w,
			requestIdentity,
			http.StatusConflict,
			false,
			adminProductCreatePath,
			adminProductNavigationPath,
			"",
			values,
			validationErrors,
		)

		return
	}
	if err != nil || !isValidAdminProductWriteResult(result) {
		log.Print("admin product create failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	http.Redirect(w, r, adminProductPath(result.ID), http.StatusSeeOther)
}

// adminProductUpdateHandler validates the hidden revision before applying an
// edit. A stale valid form receives a fixed 409 page and never overwrites data.
func (app *application) adminProductUpdateHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	productID, canonicalPath, valid := canonicalAdminProductWritePath(r, "")
	if !valid {
		http.NotFound(w, r)

		return
	}
	if !adminProductMutationRequestIsCanonical(w, r, canonicalPath) {
		return
	}

	form, parsed := parseStrictAdminFormWithMaximum(
		w,
		r,
		[]string{
			"csrf_token",
			"version",
			"slug",
			"name",
			"category",
			"sort_order",
			"publication_status",
			"description",
			"material",
			"dimensions",
		},
		adminProductFormMaximumBytes,
	)
	if !parsed {
		return
	}
	if !adminSessionCSRFTokenIsValid(
		requestIdentity.CSRFToken,
		form.Get("csrf_token"),
	) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	expectedVersion, valid := parseCanonicalPositiveInt64(form.Get("version"))
	if !valid || expectedVersion == math.MaxInt64 {
		// Version is a server-issued concurrency control, not a visible catalogue
		// field. Tampering or overflow is a malformed request rather than a form
		// correction opportunity.
		http.Error(w, "invalid Product revision", http.StatusBadRequest)

		return
	}
	values := adminProductValuesFromForm(form)
	input, validationErrors := validateAdminProductFormValues(values)
	detailPath := adminProductPath(productID)
	editPath := detailPath + "/edit"
	if validationErrors != (adminProductFormErrors{}) {
		app.renderAdminProductFormResponse(
			w,
			requestIdentity,
			http.StatusUnprocessableEntity,
			true,
			detailPath,
			detailPath,
			strconv.FormatInt(expectedVersion, 10),
			values,
			validationErrors,
		)

		return
	}
	if app == nil || app.adminProductWrites == nil {
		log.Print("admin product writer unavailable")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := contextWithAdminRepositoryTimeout(r)
	result, err := app.adminProductWrites.Update(
		ctx,
		productID,
		expectedVersion,
		input,
	)
	cancel()
	switch {
	case errors.Is(err, errAdminProductNotFound):
		http.NotFound(w, r)

		return
	case errors.Is(err, errAdminProductWriteConflict):
		data := newAuthenticatedAdminPageData(
			"Product changed",
			adminProductNavigationPath,
			requestIdentity,
		)
		data.ProductConflict = &adminProductConflictPageData{
			DetailPath: detailPath,
			EditPath:   editPath,
		}
		app.renderAdmin(w, http.StatusConflict, "product-conflict.html", data)

		return
	case errors.Is(err, errAdminProductSlugConflict):
		validationErrors.Slug = "That slug is already used by another Product."
		app.renderAdminProductFormResponse(
			w,
			requestIdentity,
			http.StatusConflict,
			true,
			detailPath,
			detailPath,
			strconv.FormatInt(expectedVersion, 10),
			values,
			validationErrors,
		)

		return
	case err != nil || result.ID != productID ||
		result.Version != expectedVersion+1:
		log.Print("admin product update failed")
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)

		return
	}

	http.Redirect(w, r, detailPath, http.StatusSeeOther)
}

// adminProductMutationRequestIsCanonical rejects alternate queries and content
// codings before the strict form parser reads a body.
func adminProductMutationRequestIsCanonical(
	w http.ResponseWriter,
	r *http.Request,
	canonicalPath string,
) bool {
	if !adminProductRequestPathAndQueryAreCanonical(r, canonicalPath) {
		if r.URL.ForceQuery || r.URL.RawQuery != "" {
			http.Error(w, "invalid Product request", http.StatusBadRequest)
		} else {
			http.NotFound(w, r)
		}

		return false
	}
	if r.Header.Get("Content-Encoding") != "" {
		http.Error(w, "content encoding is not supported", http.StatusUnsupportedMediaType)

		return false
	}

	return true
}

// adminProductRequestPathAndQueryAreCanonical accepts one escaped path spelling
// and no query delimiter or query data.
func adminProductRequestPathAndQueryAreCanonical(
	r *http.Request,
	canonicalPath string,
) bool {
	return r.URL.EscapedPath() == canonicalPath &&
		!r.URL.ForceQuery && r.URL.RawQuery == ""
}

// canonicalAdminProductWritePath validates one positive ID and exact optional
// suffix. It returns the canonical path for later rendering and redirects.
func canonicalAdminProductWritePath(
	r *http.Request,
	suffix string,
) (int64, string, bool) {
	productID, valid := parseCanonicalPositiveInt64(r.PathValue("id"))
	if !valid {
		return 0, "", false
	}
	canonicalPath := adminProductPath(productID) + suffix
	if r.URL.EscapedPath() != canonicalPath {
		return 0, "", false
	}

	return productID, canonicalPath, true
}

// adminProductValuesFromForm copies only visible Product controls from the
// already cardinality-checked form.
func adminProductValuesFromForm(form mapFormValues) adminProductFormValues {
	return adminProductFormValues{
		Slug:              form.Get("slug"),
		Name:              form.Get("name"),
		Category:          form.Get("category"),
		SortOrder:         form.Get("sort_order"),
		PublicationStatus: form.Get("publication_status"),
		Description:       form.Get("description"),
		Material:          form.Get("material"),
		Dimensions:        form.Get("dimensions"),
	}
}

// validateAdminProductFormValues converts visible fields into persistence data
// and returns safe field messages without normalizing an invalid spelling into
// a different Product value.
func validateAdminProductFormValues(
	values adminProductFormValues,
) (adminProductWriteInput, adminProductFormErrors) {
	var validationErrors adminProductFormErrors
	if !isCanonicalProductSlug(values.Slug) {
		validationErrors.Slug = "Use 1–120 lowercase letters or numbers, separated only by single hyphens."
	}
	if !isValidProductCatalogueText(values.Name, productNameMaximumLength) {
		validationErrors.Name = "Enter a trimmed Product name between 1 and 160 characters."
	}
	if !isValidProductCatalogueText(values.Category, productCategoryMaximumLength) {
		validationErrors.Category = "Enter a trimmed category between 1 and 80 characters."
	}

	sortOrder64, valid := parseCanonicalPositiveInt64(values.SortOrder)
	if !valid || sortOrder64 > math.MaxInt32 {
		validationErrors.SortOrder = "Enter a whole number from 1 to 2147483647."
	}
	if !isValidProductPublicationStatus(values.PublicationStatus) {
		validationErrors.PublicationStatus = "Choose Draft, Published, or Archived."
	}
	if !isValidOptionalProductText(
		values.Description,
		productDescriptionMaximumLength,
	) {
		validationErrors.Description = "Use at most 6000 characters and remove leading or trailing whitespace."
	}
	if !isValidOptionalProductText(
		values.Material,
		productMaterialMaximumLength,
	) {
		validationErrors.Material = "Use at most 500 characters and remove leading or trailing whitespace."
	}
	if !isValidOptionalProductText(
		values.Dimensions,
		productDimensionsMaximumLength,
	) {
		validationErrors.Dimensions = "Use at most 500 characters and remove leading or trailing whitespace."
	}

	if validationErrors != (adminProductFormErrors{}) {
		return adminProductWriteInput{}, validationErrors
	}

	return adminProductWriteInput{
		Slug:              values.Slug,
		Name:              values.Name,
		Category:          values.Category,
		SortOrder:         int(sortOrder64),
		PublicationStatus: values.PublicationStatus,
		Description:       values.Description,
		Material:          values.Material,
		Dimensions:        values.Dimensions,
	}, adminProductFormErrors{}
}

// newAdminProductFormPageData builds trusted labels, paths, and options around
// escaped form values for either create or edit mode.
func newAdminProductFormPageData(
	isEdit bool,
	action string,
	cancelPath string,
	version string,
	values adminProductFormValues,
	validationErrors adminProductFormErrors,
) adminProductFormPageData {
	form := adminProductFormPageData{
		Eyebrow:       "New catalogue record",
		Heading:       "Create Product",
		Introduction:  "Create the durable catalogue facts, reviewed editorial content, and publication state.",
		Action:        action,
		CancelPath:    cancelPath,
		SubmitLabel:   "Create Product",
		IsEdit:        isEdit,
		Version:       version,
		Values:        values,
		Errors:        validationErrors,
		HasErrors:     validationErrors != (adminProductFormErrors{}),
		StatusOptions: adminProductStatusOptions(values.PublicationStatus),
		HasValidStatus: isValidProductPublicationStatus(
			values.PublicationStatus,
		),
	}
	if isEdit {
		form.Eyebrow = "Catalogue revision " + version
		form.Heading = "Edit Product"
		form.Introduction = "Save deliberate catalogue, editorial, or publication-state changes. A stale form cannot overwrite a newer revision."
		form.SubmitLabel = "Save Product"
	}

	return form
}

// adminProductStatusOptions creates the closed select list and never turns an
// untrusted submitted value into option markup.
func adminProductStatusOptions(
	selected string,
) []adminProductStatusOptionPageData {
	return []adminProductStatusOptionPageData{
		{Value: productPublicationStatusDraft, Label: "Draft", Selected: selected == productPublicationStatusDraft},
		{Value: publishedProductStatus, Label: "Published", Selected: selected == publishedProductStatus},
		{Value: productPublicationStatusArchived, Label: "Archived", Selected: selected == productPublicationStatusArchived},
	}
}

// renderAdminProductFormResponse reuses one form contract for semantic and slug
// conflicts while preserving the authenticated shell and session CSRF token.
func (app *application) renderAdminProductFormResponse(
	w http.ResponseWriter,
	requestIdentity authenticatedAdminRequest,
	status int,
	isEdit bool,
	action string,
	cancelPath string,
	version string,
	values adminProductFormValues,
	validationErrors adminProductFormErrors,
) {
	form := newAdminProductFormPageData(
		isEdit,
		action,
		cancelPath,
		version,
		values,
		validationErrors,
	)
	title := "Create Product"
	if isEdit {
		title = "Edit Product"
	}
	data := newAuthenticatedAdminPageData(
		title,
		adminProductNavigationPath,
		requestIdentity,
	)
	data.ProductForm = &form

	app.renderAdmin(w, status, "product-form.html", data)
}
