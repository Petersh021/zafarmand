package main

import "net/http"

// adminReferenceWorkspacePageData is a presentation-only contract for
// Architectural reference workspaces whose operational backends have not been approved
// or implemented. Every value is trusted sample content declared in this file.
type adminReferenceWorkspacePageData struct {
	Kind       string
	Eyebrow    string
	Title      string
	Summary    string
	Notice     string
	Tabs       []string
	Metrics    []adminReferenceMetricPageData
	ChartLabel string
	ChartBars  []adminReferenceChartBarPageData
	Cards      []adminReferenceCardPageData
	Rows       []adminReferenceRowPageData
}

type adminReferenceMetricPageData struct {
	Label string
	Value string
	Delta string
	Tone  string
}

type adminReferenceChartBarPageData struct {
	Label string
	Level int
	Tone  string
}

type adminReferenceCardPageData struct {
	Title       string
	Description string
	Meta        string
	Tone        string
}

type adminReferenceRowPageData struct {
	Primary   string
	Secondary string
	Value     string
	Status    string
	Tone      string
}

// adminReferenceWorkspacePaths is also used when routes are registered. The
// explicit list prevents an authenticated wildcard from swallowing unknown
// admin URLs or colliding with existing record routes.
var adminReferenceWorkspacePaths = []string{
	"/admin/orders",
	"/admin/analytics",
	"/admin/customers",
	"/admin/finances",
	"/admin/invoices",
	"/admin/discounts",
	"/admin/chats",
	"/admin/email",
	"/admin/apps",
	"/admin/settings",
	"/admin/help",
}

var adminReferenceWorkspaces = map[string]adminReferenceWorkspacePageData{
	"/admin/orders": {
		Kind:    "orders",
		Eyebrow: "Shop operations",
		Title:   "Orders",
		Summary: "Review the order queue, packing stages, drafts, and returns before a commerce backend is connected.",
		Notice:  "All order names, totals, and states on this page are illustrative and cannot be changed.",
		Tabs:    []string{"All orders", "Packing board", "Drafts", "Returns"},
		Metrics: []adminReferenceMetricPageData{
			{Label: "Open orders", Value: "24", Delta: "Demo queue", Tone: "blue"},
			{Label: "To pack", Value: "8", Delta: "Illustrative", Tone: "amber"},
			{Label: "Packed", Value: "13", Delta: "Preview state", Tone: "green"},
			{Label: "Returns", Value: "3", Delta: "Sample only", Tone: "red"},
		},
		ChartLabel: "Order volume",
		ChartBars:  referenceWeekBars("blue"),
		Cards: []adminReferenceCardPageData{
			{Title: "Packing queue", Description: "Eight sample orders are waiting in the first board column.", Meta: "No drag action is persisted", Tone: "amber"},
			{Title: "Dispatch window", Description: "The demonstration carrier collection is shown for 16:30.", Meta: "Preview schedule", Tone: "green"},
			{Title: "Returns review", Description: "Three illustrative returns need a decision.", Meta: "Read-only", Tone: "red"},
		},
		Rows: []adminReferenceRowPageData{
			{Primary: "ZF-1048", Secondary: "Sofia Moreno · 2 items", Value: "$37.50", Status: "To pack", Tone: "amber"},
			{Primary: "ZF-1047", Secondary: "Yara Moreno · 3 items", Value: "$62.05", Status: "Packed", Tone: "green"},
			{Primary: "ZF-1046", Secondary: "June Hart · 2 items", Value: "$33.50", Status: "Paid", Tone: "blue"},
		},
	},
	"/admin/analytics": {
		Kind:    "analytics",
		Eyebrow: "Shop intelligence",
		Title:   "Analytics",
		Summary: "Explore sales, visitor, and checkout-funnel views in Zafarmand's architectural operations workspace.",
		Notice:  "Charts and conversion values are sample data and do not measure the public Zafarmand website.",
		Tabs:    []string{"Sales", "Visitors", "Checkout funnel", "Sources"},
		Metrics: []adminReferenceMetricPageData{
			{Label: "Sales", Value: "$6,409", Delta: "+5.5% demo", Tone: "green"},
			{Label: "Visitors", Value: "1,284", Delta: "Last 7 days", Tone: "blue"},
			{Label: "Conversion", Value: "2.5%", Delta: "+0.3 demo", Tone: "green"},
			{Label: "Average order", Value: "$84", Delta: "Illustrative", Tone: "amber"},
		},
		ChartLabel: "Sales studio",
		ChartBars:  referenceMonthBars("blue"),
		Cards: []adminReferenceCardPageData{
			{Title: "Web store", Description: "The sample source represents 64% of attributed sales.", Meta: "$15,007 illustrative", Tone: "blue"},
			{Title: "Instagram", Description: "A demonstration social source contributes 15%.", Meta: "$3,518 illustrative", Tone: "amber"},
			{Title: "Wholesale", Description: "The sample wholesale channel contributes 8%.", Meta: "$1,875 illustrative", Tone: "green"},
		},
		Rows: []adminReferenceRowPageData{
			{Primary: "Product page", Secondary: "1,284 visits", Value: "3.2%", Status: "Healthy", Tone: "green"},
			{Primary: "Basket", Secondary: "312 sessions", Value: "24.3%", Status: "Preview", Tone: "blue"},
			{Primary: "Checkout", Secondary: "84 sessions", Value: "26.9%", Status: "Review", Tone: "amber"},
		},
	},
	"/admin/customers": {
		Kind:    "customers",
		Eyebrow: "Audience",
		Title:   "Customers",
		Summary: "Preview customer profiles, segments, order history, and review signals from the reference dashboard.",
		Notice:  "These are invented demonstration identities. No customer account database is connected.",
		Tabs:    []string{"Directory", "Segments", "Reviews"},
		Metrics: []adminReferenceMetricPageData{
			{Label: "Profiles", Value: "428", Delta: "Demo directory", Tone: "blue"},
			{Label: "Returning", Value: "76", Delta: "17.8% sample", Tone: "green"},
			{Label: "Segments", Value: "5", Delta: "Preview groups", Tone: "amber"},
			{Label: "Reviews", Value: "18", Delta: "Illustrative", Tone: "blue"},
		},
		ChartLabel: "Customer growth",
		ChartBars:  referenceMonthBars("green"),
		Cards: []adminReferenceCardPageData{
			{Title: "Recent buyers", Description: "A read-only sample segment for recently active profiles.", Meta: "48 profiles", Tone: "green"},
			{Title: "Studio clients", Description: "A proposed segment for architecture and interior clients.", Meta: "12 profiles", Tone: "blue"},
			{Title: "Needs review", Description: "Illustrative accounts with incomplete information.", Meta: "6 profiles", Tone: "amber"},
		},
		Rows: []adminReferenceRowPageData{
			{Primary: "Nora Ellis", Secondary: "Recent buyer", Value: "3 orders", Status: "Active", Tone: "green"},
			{Primary: "Emil Hart", Secondary: "Studio client", Value: "1 project", Status: "Preview", Tone: "blue"},
			{Primary: "Mina Stone", Secondary: "Newsletter", Value: "No orders", Status: "New", Tone: "amber"},
		},
	},
	"/admin/finances": {
		Kind:    "finances",
		Eyebrow: "Money",
		Title:   "Finances",
		Summary: "Preview profit and loss, payouts, expenses, and the studio's illustrative cash position.",
		Notice:  "No bank, payment processor, or accounting system is connected; every amount is demonstration data.",
		Tabs:    []string{"Profit & loss", "Payouts", "Expenses"},
		Metrics: []adminReferenceMetricPageData{
			{Label: "Revenue", Value: "$12,840", Delta: "Demo month", Tone: "green"},
			{Label: "Expenses", Value: "$7,120", Delta: "Illustrative", Tone: "red"},
			{Label: "Gross margin", Value: "$5,720", Delta: "44.5% sample", Tone: "blue"},
			{Label: "Next payout", Value: "$3,400", Delta: "Preview only", Tone: "amber"},
		},
		ChartLabel: "Cash movement",
		ChartBars:  referenceMonthBars("green"),
		Cards: []adminReferenceCardPageData{
			{Title: "Payout schedule", Description: "The next illustrative settlement is grouped for Friday.", Meta: "$3,400 sample", Tone: "green"},
			{Title: "Operating costs", Description: "Materials and fulfilment lead the demonstration expenses.", Meta: "4 categories", Tone: "amber"},
			{Title: "Reconciliation", Description: "No live ledger is connected to this workspace.", Meta: "Integration required", Tone: "red"},
		},
		Rows: []adminReferenceRowPageData{
			{Primary: "Materials", Secondary: "Demonstration expense", Value: "$2,840", Status: "Recorded", Tone: "blue"},
			{Primary: "Fulfilment", Secondary: "Demonstration expense", Value: "$1,420", Status: "Recorded", Tone: "blue"},
			{Primary: "Studio services", Secondary: "Demonstration expense", Value: "$980", Status: "Review", Tone: "amber"},
		},
	},
	"/admin/invoices": {
		Kind:    "invoices",
		Eyebrow: "Money",
		Title:   "Invoices",
		Summary: "Inspect the invoice overview, filters, and status presentation demonstrated in the reference video.",
		Notice:  "Invoice documents and payment states are read-only samples; sending and marking paid are disabled.",
		Tabs:    []string{"All invoices", "Open", "Overdue", "Paid"},
		Metrics: []adminReferenceMetricPageData{
			{Label: "Invoices", Value: "42", Delta: "Demo total", Tone: "blue"},
			{Label: "Open", Value: "$8,920", Delta: "Illustrative", Tone: "amber"},
			{Label: "Overdue", Value: "3", Delta: "Action preview", Tone: "red"},
			{Label: "Paid", Value: "$21,420", Delta: "Sample period", Tone: "green"},
		},
		ChartLabel: "Invoice settlement",
		ChartBars:  referenceWeekBars("blue"),
		Cards: []adminReferenceCardPageData{
			{Title: "Awaiting payment", Description: "Seven sample invoices remain open.", Meta: "$8,920 illustrative", Tone: "amber"},
			{Title: "Overdue review", Description: "Three demonstration records show the overdue state.", Meta: "No reminders sent", Tone: "red"},
			{Title: "Settled", Description: "Paid samples demonstrate the final document state.", Meta: "32 records", Tone: "green"},
		},
		Rows: []adminReferenceRowPageData{
			{Primary: "INV-2042", Secondary: "Atelier North", Value: "$2,400", Status: "Open", Tone: "amber"},
			{Primary: "INV-2041", Secondary: "Field House", Value: "$1,250", Status: "Overdue", Tone: "red"},
			{Primary: "INV-2040", Secondary: "Noor Studio", Value: "$3,100", Status: "Paid", Tone: "green"},
		},
	},
	"/admin/discounts": {
		Kind:    "discounts",
		Eyebrow: "Money",
		Title:   "Discounts",
		Summary: "Preview discount codes, usage, and campaign scheduling from the complete commerce dashboard.",
		Notice:  "Codes cannot be applied to the public site until checkout and campaign backends exist.",
		Tabs:    []string{"Discount codes", "Campaigns"},
		Metrics: []adminReferenceMetricPageData{
			{Label: "Active codes", Value: "12", Delta: "Demo set", Tone: "green"},
			{Label: "Redemptions", Value: "238", Delta: "Illustrative", Tone: "blue"},
			{Label: "Attributed", Value: "$1,440", Delta: "Sample revenue", Tone: "green"},
			{Label: "Scheduled", Value: "3", Delta: "Preview campaigns", Tone: "amber"},
		},
		ChartLabel: "Code usage",
		ChartBars:  referenceWeekBars("amber"),
		Cards: []adminReferenceCardPageData{
			{Title: "WELCOME10", Description: "A sample welcome code with a ten percent reduction.", Meta: "84 uses", Tone: "green"},
			{Title: "STUDIO15", Description: "A proposed studio-client campaign.", Meta: "Scheduled preview", Tone: "amber"},
			{Title: "ARCHIVE20", Description: "An expired example showing the inactive state.", Meta: "Expired", Tone: "red"},
		},
		Rows: []adminReferenceRowPageData{
			{Primary: "WELCOME10", Secondary: "10% percentage", Value: "84 uses", Status: "Active", Tone: "green"},
			{Primary: "STUDIO15", Secondary: "15% percentage", Value: "0 uses", Status: "Scheduled", Tone: "amber"},
			{Primary: "ARCHIVE20", Secondary: "20% percentage", Value: "112 uses", Status: "Expired", Tone: "red"},
		},
	},
	"/admin/chats": {
		Kind:    "chats",
		Eyebrow: "Talk",
		Title:   "Chats",
		Summary: "Preview a unified inbox for website, Instagram, and text conversations without transmitting messages.",
		Notice:  "Conversation names and messages are invented. Sending, replying, and external account connections are disabled.",
		Tabs:    []string{"All conversations", "Website", "Instagram", "SMS"},
		Metrics: []adminReferenceMetricPageData{
			{Label: "Open", Value: "18", Delta: "Demo inbox", Tone: "blue"},
			{Label: "Website", Value: "7", Delta: "Illustrative", Tone: "green"},
			{Label: "Instagram", Value: "6", Delta: "Not connected", Tone: "amber"},
			{Label: "SMS", Value: "5", Delta: "Not connected", Tone: "red"},
		},
		ChartLabel: "Conversation volume",
		ChartBars:  referenceWeekBars("blue"),
		Cards: []adminReferenceCardPageData{
			{Title: "Website inquiry", Description: "A sample visitor asks about an interior project.", Meta: "4 minutes ago", Tone: "green"},
			{Title: "Instagram preview", Description: "External messaging requires a reviewed integration.", Meta: "Not connected", Tone: "amber"},
			{Title: "SMS preview", Description: "Text messaging requires a provider and consent workflow.", Meta: "Not connected", Tone: "red"},
		},
		Rows: []adminReferenceRowPageData{
			{Primary: "Lena Morris", Secondary: "Website · Interior question", Value: "2 messages", Status: "Open", Tone: "green"},
			{Primary: "Sample social lead", Secondary: "Instagram preview", Value: "1 message", Status: "Offline", Tone: "amber"},
			{Primary: "Sample text lead", Secondary: "SMS preview", Value: "3 messages", Status: "Offline", Tone: "red"},
		},
	},
	"/admin/email": {
		Kind:    "email",
		Eyebrow: "Talk",
		Title:   "Email",
		Summary: "Preview the dense inbox, priority markers, drafts, and message-detail layout from the reference dashboard.",
		Notice:  "This is not connected to Gmail or another mailbox. Reading and composing are visual previews only.",
		Tabs:    []string{"Inbox", "Priority", "Drafts", "Flagged"},
		Metrics: []adminReferenceMetricPageData{
			{Label: "Unread", Value: "32", Delta: "Demo inbox", Tone: "blue"},
			{Label: "Priority", Value: "14", Delta: "Illustrative", Tone: "red"},
			{Label: "Drafts", Value: "8", Delta: "Preview only", Tone: "amber"},
			{Label: "Flagged", Value: "3", Delta: "Sample", Tone: "green"},
		},
		ChartLabel: "Inbox activity",
		ChartBars:  referenceWeekBars("blue"),
		Cards: []adminReferenceCardPageData{
			{Title: "Project enquiry", Description: "A demonstration message about architecture services.", Meta: "Unread", Tone: "blue"},
			{Title: "Supplier note", Description: "A sample materials update for the studio.", Meta: "Priority preview", Tone: "red"},
			{Title: "Publication draft", Description: "An illustrative draft waiting for editorial review.", Meta: "Draft", Tone: "amber"},
		},
		Rows: []adminReferenceRowPageData{
			{Primary: "Project enquiry", Secondary: "Ava Stone · Architecture services", Value: "10:42", Status: "Unread", Tone: "blue"},
			{Primary: "Materials schedule", Secondary: "North Workshop", Value: "09:18", Status: "Priority", Tone: "red"},
			{Primary: "Journal proposal", Secondary: "Editorial preview", Value: "Yesterday", Status: "Draft", Tone: "amber"},
		},
	},
	"/admin/apps": {
		Kind:    "apps",
		Eyebrow: "Extend",
		Title:   "Apps",
		Summary: "Review possible commerce, messaging, analytics, and accounting integrations before approving any connection.",
		Notice:  "No third-party application is connected. Integration cards are visual planning aids only.",
		Tabs:    []string{"All apps", "Commerce", "Communication", "Analytics"},
		Metrics: []adminReferenceMetricPageData{
			{Label: "Connected", Value: "0", Delta: "No live apps", Tone: "green"},
			{Label: "Available", Value: "8", Delta: "Planning list", Tone: "blue"},
			{Label: "Categories", Value: "4", Delta: "Preview", Tone: "amber"},
			{Label: "Requires review", Value: "8", Delta: "Security first", Tone: "red"},
		},
		ChartLabel: "Integration readiness",
		ChartBars:  referenceWeekBars("blue"),
		Cards: []adminReferenceCardPageData{
			{Title: "Payments", Description: "A future payment provider for checkout and refunds.", Meta: "Not connected", Tone: "blue"},
			{Title: "Instagram", Description: "A future reviewed connection for social messages.", Meta: "Not connected", Tone: "amber"},
			{Title: "Analytics", Description: "A privacy-reviewed measurement integration.", Meta: "Not connected", Tone: "green"},
		},
		Rows: []adminReferenceRowPageData{
			{Primary: "Commerce provider", Secondary: "Orders and payments", Value: "Planned", Status: "Not connected", Tone: "blue"},
			{Primary: "Social messaging", Secondary: "Instagram inbox", Value: "Review", Status: "Not connected", Tone: "amber"},
			{Primary: "Accounting export", Secondary: "Finance records", Value: "Planned", Status: "Not connected", Tone: "green"},
		},
	},
	"/admin/settings": {
		Kind:    "settings",
		Eyebrow: "System",
		Title:   "Settings",
		Summary: "Preview general, appearance, account, and integration controls in the Zafarmand visual system.",
		Notice:  "Controls are read-only until each preference has a secure persistence and authorization design.",
		Tabs:    []string{"General", "Appearance", "Account", "Integrations"},
		Metrics: []adminReferenceMetricPageData{
			{Label: "Sections", Value: "4", Delta: "Preview groups", Tone: "blue"},
			{Label: "Theme", Value: "Dark", Delta: "Current shell", Tone: "green"},
			{Label: "Accent", Value: "Warm stone", Delta: "Zafarmand palette", Tone: "blue"},
			{Label: "Pending", Value: "6", Delta: "Not persisted", Tone: "amber"},
		},
		ChartLabel: "Configuration coverage",
		ChartBars:  referenceWeekBars("blue"),
		Cards: []adminReferenceCardPageData{
			{Title: "Appearance", Description: "Architectural charcoal, paper, and warm-stone accents.", Meta: "Current presentation", Tone: "blue"},
			{Title: "Account", Description: "Identity and role remain controlled by the authenticated backend.", Meta: "Protected", Tone: "green"},
			{Title: "Integrations", Description: "External connections require explicit review and secrets handling.", Meta: "Not configured", Tone: "amber"},
		},
		Rows: []adminReferenceRowPageData{
			{Primary: "General", Secondary: "Studio name and locale", Value: "Preview", Status: "Read-only", Tone: "blue"},
			{Primary: "Appearance", Secondary: "Theme and accent", Value: "Charcoal / Stone", Status: "Current", Tone: "green"},
			{Primary: "Integrations", Secondary: "External services", Value: "0 connected", Status: "Review", Tone: "amber"},
		},
	},
	"/admin/help": {
		Kind:    "help",
		Eyebrow: "System",
		Title:   "Help center",
		Summary: "Browse a preview knowledge base for publishing, content, authentication, and future commerce workflows.",
		Notice:  "The support form from the reference video is intentionally omitted until a ticket backend exists.",
		Tabs:    []string{"Frequently asked", "Publishing", "Accounts", "Commerce"},
		Metrics: []adminReferenceMetricPageData{
			{Label: "Articles", Value: "24", Delta: "Demo library", Tone: "blue"},
			{Label: "Categories", Value: "6", Delta: "Preview", Tone: "green"},
			{Label: "Open tickets", Value: "0", Delta: "No ticket backend", Tone: "amber"},
			{Label: "System", Value: "Online", Delta: "Local application", Tone: "green"},
		},
		ChartLabel: "Article coverage",
		ChartBars:  referenceWeekBars("green"),
		Cards: []adminReferenceCardPageData{
			{Title: "Publish a project", Description: "Review the create, cover, publication, and revision workflow.", Meta: "Content guide", Tone: "blue"},
			{Title: "Administrator access", Description: "Understand password, email-code, and Google sign-in methods.", Meta: "Access guide", Tone: "green"},
			{Title: "Preview workspaces", Description: "Learn which planning modules are not connected to live data.", Meta: "System guide", Tone: "amber"},
		},
		Rows: []adminReferenceRowPageData{
			{Primary: "How do I publish a project?", Secondary: "Publishing", Value: "5 min", Status: "Available", Tone: "green"},
			{Primary: "Which login method should I use?", Secondary: "Accounts", Value: "4 min", Status: "Available", Tone: "green"},
			{Primary: "Are preview totals real?", Secondary: "Commerce", Value: "2 min", Status: "Important", Tone: "amber"},
		},
	},
}

func referenceWeekBars(tone string) []adminReferenceChartBarPageData {
	return []adminReferenceChartBarPageData{
		{Label: "Mon", Level: 4, Tone: tone},
		{Label: "Tue", Level: 7, Tone: tone},
		{Label: "Wed", Level: 5, Tone: tone},
		{Label: "Thu", Level: 9, Tone: tone},
		{Label: "Fri", Level: 8, Tone: tone},
		{Label: "Sat", Level: 6, Tone: tone},
		{Label: "Sun", Level: 10, Tone: tone},
	}
}

func referenceMonthBars(tone string) []adminReferenceChartBarPageData {
	return []adminReferenceChartBarPageData{
		{Label: "Jan", Level: 3, Tone: tone},
		{Label: "Feb", Level: 5, Tone: tone},
		{Label: "Mar", Level: 4, Tone: tone},
		{Label: "Apr", Level: 7, Tone: tone},
		{Label: "May", Level: 6, Tone: tone},
		{Label: "Jun", Level: 8, Tone: tone},
		{Label: "Jul", Level: 7, Tone: tone},
		{Label: "Aug", Level: 9, Tone: tone},
	}
}

// adminReferenceWorkspaceHandler renders only definitions from the exact-path
// map above. It performs no repository calls and exposes no mutation forms.
func (app *application) adminReferenceWorkspaceHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	requestIdentity, ok := authenticatedAdminFromContext(r.Context())
	if !ok {
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	definition, ok := adminReferenceWorkspaces[r.URL.Path]
	if !ok {
		http.NotFound(w, r)

		return
	}

	data := newAuthenticatedAdminPageData(
		definition.Title,
		r.URL.Path,
		requestIdentity,
	)
	data.ReferenceWorkspace = &definition
	templateName := "reference-workspace.html"
	if definition.Kind == "orders" {
		// Orders has a richer browser-only packing-board demonstration while
		// retaining the same authenticated, exact-path, read-only HTTP boundary.
		templateName = "orders.html"
	}

	app.renderAdmin(
		w,
		http.StatusOK,
		templateName,
		data,
	)
}
