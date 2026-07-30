package main

import "net/http"

// homeHandler renders the public homepage for a request already matched to
// GET / by the router.
//
// The request value is deliberately named "_" because this handler currently
// needs no headers, query values, or form data from it. The pageData value is
// the boundary between Go and the HTML template: CurrentPath controls active
// navigation state, HomeHero supplies temporary structured hero content, and
// HomeDisciplines supplies an ordered collection for the template to range
// over. These view models can later be populated from a database without
// changing their HTML contract.
func (app *application) homeHandler(
	w http.ResponseWriter,
	_ *http.Request,
) {
	app.render(
		w,
		http.StatusOK,
		"home.html",
		pageData{
			Title:       "Home",
			CurrentPath: "/",
			HomeHero: &homeHeroData{
				StudioName: "Zafarmand",
				Descriptor: "Design Studio",
				ImageURL: "/static/images/" +
					"home-hero-placeholder.jpg",
				ImageAlt: "Warm minimalist living room " +
					"with stone walls, sculptural seating, " +
					"and a wooden chair",
				ImageWidth:  1536,
				ImageHeight: 1024,
			},
			// The order matches the desktop header and drawer navigation.
			// These are structural route entrances, not database records.
			HomeDisciplines: []disciplineEntranceData{
				{
					Number: "01",
					Name:   "Interior Design",
					Path:   "/interior-design",
				},
				{
					Number: "02",
					Name:   "Architecture Design",
					Path:   "/architecture-design",
				},
				{
					Number: "03",
					Name:   "Products",
					Path:   "/products",
				},
			},
		},
	)
}

// productsHandler renders the Products landing page with an HTTP 200 response.
//
// CurrentPath matches the route exactly so shared navigation templates can
// mark Products as the current page for sighted users and assistive technology.
// DisciplinePage contains truthful route-level presentation data, while
// ProductListing introduces the first Products-only vertical slice. Its items
// are clearly structural catalogue slots rather than final product records:
// prices, descriptions, media, slugs, and database state remain deferred.
func (app *application) productsHandler(
	w http.ResponseWriter,
	_ *http.Request,
) {
	disciplinePage := &disciplinePageData{
		Number:   "03",
		Name:     "Products",
		NextName: "Interior Design",
		NextPath: "/interior-design",
	}

	// Temporary slice data demonstrates how a handler supplies an ordered
	// collection to html/template. These broad product families are structural
	// placeholders drawn from the approved design direction, not claims about
	// products that Zafarmand has already published.
	productListing := &productListingData{
		Eyebrow: "Zafarmand objects",
		Heading: "Product catalogue",
		Introduction: "An evolving index of furniture, lighting, " +
			"objects, and material studies.",
		EmptyMessage: "Product entries are being prepared for publication.",
		Items: []productPreviewData{
			{
				Number:   "01",
				Category: "Furniture",
				Status:   "Content in preparation",
			},
			{
				Number:   "02",
				Category: "Lighting",
				Status:   "Content in preparation",
			},
			{
				Number:   "03",
				Category: "Objects",
				Status:   "Content in preparation",
			},
			{
				Number:   "04",
				Category: "Materials",
				Status:   "Content in preparation",
			},
		},
	}

	app.render(
		w,
		http.StatusOK,
		"products.html",
		pageData{
			Title:          disciplinePage.Name,
			CurrentPath:    "/products",
			DisciplinePage: disciplinePage,
			ProductListing: productListing,
		},
	)
}

// interiorDesignHandler renders the Interior Design landing page.
//
// The handler supplies the shared discipline view model while common response
// behavior such as template lookup, execution, headers, and errors remains in
// application.render. No fictional interior projects or descriptions are
// introduced during this shared-pattern stage.
func (app *application) interiorDesignHandler(
	w http.ResponseWriter,
	_ *http.Request,
) {
	disciplinePage := &disciplinePageData{
		Number:   "01",
		Name:     "Interior Design",
		NextName: "Architecture Design",
		NextPath: "/architecture-design",
	}

	app.render(
		w,
		http.StatusOK,
		"interior-design.html",
		pageData{
			Title:          disciplinePage.Name,
			CurrentPath:    "/interior-design",
			DisciplinePage: disciplinePage,
		},
	)
}

// architectureDesignHandler renders the Architecture Design landing page.
//
// Separating each route into its own handler leaves a clear place for the later
// Architecture vertical slice, while DisciplinePage lets the current route
// reuse the same accessible landing-page structure as the other disciplines.
func (app *application) architectureDesignHandler(
	w http.ResponseWriter,
	_ *http.Request,
) {
	disciplinePage := &disciplinePageData{
		Number:   "02",
		Name:     "Architecture Design",
		NextName: "Products",
		NextPath: "/products",
	}

	app.render(
		w,
		http.StatusOK,
		"architecture-design.html",
		pageData{
			Title:          disciplinePage.Name,
			CurrentPath:    "/architecture-design",
			DisciplinePage: disciplinePage,
		},
	)
}
