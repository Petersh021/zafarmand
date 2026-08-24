package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Operational constants define the complete listener, readiness, admission,
// security-header, health-path, and fixed-response contract for one process.
const (
	// operationalHTTPAddressEnvironmentName is the optional process setting for
	// the HTTP listener. The loopback default is deliberately safe for local use;
	// a deployment that needs all interfaces must opt in with an explicit value.
	operationalHTTPAddressEnvironmentName = "ZAFARMAND_HTTP_ADDRESS"
	// operationalExternalHTTPSEnvironmentName declares that every public request
	// reached a reviewed HTTPS edge before the edge forwarded it to this process.
	// It is configuration, not a client-controlled forwarding header.
	operationalExternalHTTPSEnvironmentName = "ZAFARMAND_EXTERNAL_HTTPS"
	// defaultOperationalHTTPAddress avoids the all-interface behavior of ":8080"
	// while preserving the existing local port.
	defaultOperationalHTTPAddress = "127.0.0.1:8080"

	// operationalReadinessTimeout bounds both the PostgreSQL ping and immutable
	// migration-ledger verification performed by one readiness request.
	operationalReadinessTimeout = 2 * time.Second
	// defaultOperationalMaxConcurrentRequests bounds application and readiness work
	// admitted by one process. Liveness alone bypasses the limit so saturation
	// remains observable without allowing unbounded PostgreSQL probes.
	defaultOperationalMaxConcurrentRequests = 64
	// operationalHSTSValue is emitted only when direct TLS or reviewed external
	// HTTPS configuration proves that the public origin is HTTPS.
	operationalHSTSValue = "max-age=31536000"

	// operationalLivePath is the database-independent process-health endpoint.
	operationalLivePath = "/health/live"
	// operationalReadyPath verifies PostgreSQL and the immutable migration ledger.
	operationalReadyPath = "/health/ready"

	// Fixed health bodies expose no dependency or migration diagnostic details.
	operationalLiveBody     = "live\n"
	operationalReadyBody    = "ready\n"
	operationalNotReadyBody = "not ready\n"
	// Fixed rejection bodies keep method and overload responses data-independent.
	operationalMethodNotAllowed = "method not allowed\n"
	operationalBackpressureBody = "service temporarily unavailable\n"
)

// Operational errors expose only stable configuration and readiness categories;
// rejected environment, network, database, and migration details stay private.
var (
	// errOperationalEnvironmentLookupRequired identifies a programming error at
	// startup without echoing any environment value.
	errOperationalEnvironmentLookupRequired = errors.New(
		"load operational configuration: environment lookup is required",
	)
	// errOperationalHTTPAddressInvalid is stable and intentionally omits the
	// rejected address so logs cannot be used to reflect arbitrary environment text.
	errOperationalHTTPAddressInvalid = errors.New(
		"ZAFARMAND_HTTP_ADDRESS is invalid",
	)
	// errOperationalExternalHTTPSInvalid requires an exact true/false declaration;
	// permissive parsing could accidentally enable Secure-cookie proxy semantics.
	errOperationalExternalHTTPSInvalid = errors.New(
		"ZAFARMAND_EXTERNAL_HTTPS must be exactly true or false",
	)
	// errOperationalPublicBindRequiresHTTPS prevents a plain-HTTP process from
	// becoming reachable beyond the local machine by configuration omission.
	errOperationalPublicBindRequiresHTTPS = errors.New(
		"non-loopback ZAFARMAND_HTTP_ADDRESS requires ZAFARMAND_EXTERNAL_HTTPS=true",
	)
	// errOperationalHandlerDependencyInvalid keeps composition failures separate
	// from public readiness responses.
	errOperationalHandlerDependencyInvalid = errors.New(
		"create operational HTTP handler: dependencies are invalid",
	)
	// errOperationalRequestIDUnavailable is a startup failure. Serving without
	// correlation would violate the operational log contract.
	errOperationalRequestIDUnavailable = errors.New(
		"create operational HTTP handler: request ID entropy unavailable",
	)
	// errOperationalReadinessFailed is the only failure returned by the concrete
	// PostgreSQL checker. Driver, schema, and ledger details remain private.
	errOperationalReadinessFailed = errors.New(
		"operational readiness check failed",
	)
	// errOperationalReadinessDatabaseRequired prevents a checker with no pool.
	errOperationalReadinessDatabaseRequired = errors.New(
		"create PostgreSQL readiness checker: database is required",
	)
	// errOperationalReadinessCatalogRequired prevents an empty embedded history
	// from being treated as a fully migrated database.
	errOperationalReadinessCatalogRequired = errors.New(
		"create PostgreSQL readiness checker: migration catalog is required",
	)
)

// operationalConfig contains non-secret runtime settings shared by the HTTP
// server and its outer policy layer.
type operationalConfig struct {
	// httpAddress is a validated net.Listen-compatible host and numeric port.
	httpAddress string
	// externalHTTPS is true only after exact operator configuration. Request
	// headers never change it.
	externalHTTPS bool
}

// loadOperationalConfig reads the optional process settings used by the Stage
// 25 HTTP boundary. Values are not trimmed or case-folded: deployment mistakes
// fail at startup instead of silently changing listener or cookie behavior.
func loadOperationalConfig(
	lookup environmentLookup,
) (operationalConfig, error) {
	if lookup == nil {
		return operationalConfig{}, errOperationalEnvironmentLookupRequired
	}

	config := operationalConfig{
		httpAddress: defaultOperationalHTTPAddress,
	}

	if address, exists := lookup(
		operationalHTTPAddressEnvironmentName,
	); exists {
		if !isValidOperationalHTTPAddress(address) {
			return operationalConfig{}, errOperationalHTTPAddressInvalid
		}
		config.httpAddress = address
	}

	if externalHTTPS, exists := lookup(
		operationalExternalHTTPSEnvironmentName,
	); exists {
		switch externalHTTPS {
		case "true":
			config.externalHTTPS = true
		case "false":
			config.externalHTTPS = false
		default:
			return operationalConfig{}, errOperationalExternalHTTPSInvalid
		}
	}
	if !isSafeOperationalBind(config) {
		return operationalConfig{}, errOperationalPublicBindRequiresHTTPS
	}

	return config, nil
}

// isSafeOperationalBind permits plain HTTP only on loopback. The current Go
// server does not terminate TLS, so any empty, wildcard, hostname, or public IP
// bind requires an explicit reviewed external HTTPS edge.
func isSafeOperationalBind(config operationalConfig) bool {
	if config.externalHTTPS {
		return true
	}

	host, _, err := net.SplitHostPort(config.httpAddress)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// isValidOperationalHTTPAddress accepts one explicit host and numeric TCP port.
// It rejects schemes, paths, whitespace, port zero, service-name ports, and
// ambiguous non-ASCII hostnames. An empty host is allowed only when a deployment
// deliberately supplies an address such as ":8080".
func isValidOperationalHTTPAddress(address string) bool {
	if address == "" || strings.TrimSpace(address) != address {
		return false
	}

	host, portText, err := net.SplitHostPort(address)
	if err != nil || !isValidOperationalHTTPHost(host) {
		return false
	}
	if portText == "" {
		return false
	}
	for _, character := range portText {
		if character < '0' || character > '9' {
			return false
		}
	}

	port, err := strconv.ParseUint(portText, 10, 16)
	return err == nil && port > 0
}

// isValidOperationalHTTPHost accepts an empty bind-all host, an IP literal, or
// a conservative ASCII DNS hostname. Bracket removal for IPv6 has already been
// performed by net.SplitHostPort.
func isValidOperationalHTTPHost(host string) bool {
	if host == "" || net.ParseIP(host) != nil {
		return true
	}
	if len(host) > 253 || strings.HasSuffix(host, ".") {
		return false
	}

	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 ||
			label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if character >= 'a' && character <= 'z' ||
				character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' ||
				character == '-' {
				continue
			}
			return false
		}
	}

	return true
}

// operationalReadinessChecker is the narrow dependency used by the readiness
// endpoint. It keeps HTTP probing independent from PostgreSQL in unit tests.
type operationalReadinessChecker interface {
	Check(context.Context) error
}

// operationalReadinessCheckFunc adapts a function to the readiness interface.
// It is useful for small tests and future non-PostgreSQL dependency checks.
type operationalReadinessCheckFunc func(context.Context) error

// Check implements operationalReadinessChecker.
func (check operationalReadinessCheckFunc) Check(ctx context.Context) error {
	return check(ctx)
}

// postgresOperationalReadiness verifies both connectivity and the exact
// immutable migration history compiled into this binary. Function fields keep
// its policy unit-testable without a live server or a third-party SQL mock.
type postgresOperationalReadiness struct {
	ping        func(context.Context) error
	readApplied func(context.Context) ([]appliedMigration, error)
	catalog     []migrationDefinition
}

// newPostgresOperationalReadiness constructs the production readiness check.
// Its ledger query is read-only: the least-privilege runtime role needs SELECT
// on public.schema_migrations but never migration ownership or write authority.
func newPostgresOperationalReadiness(
	database *sql.DB,
) (*postgresOperationalReadiness, error) {
	if database == nil {
		return nil, errOperationalReadinessDatabaseRequired
	}

	catalog, err := loadEmbeddedMigrationCatalog()
	if err != nil {
		return nil, fmt.Errorf(
			"create PostgreSQL readiness checker: load migration catalog: %w",
			err,
		)
	}
	if len(catalog) == 0 {
		return nil, errOperationalReadinessCatalogRequired
	}

	return newPostgresOperationalReadinessWithDependencies(
		database.PingContext,
		func(ctx context.Context) ([]appliedMigration, error) {
			return readOperationalMigrationLedger(ctx, database)
		},
		catalog,
	)
}

// newPostgresOperationalReadinessWithDependencies validates and copies the
// checker inputs. It is also the deterministic construction seam for tests.
func newPostgresOperationalReadinessWithDependencies(
	ping func(context.Context) error,
	readApplied func(context.Context) ([]appliedMigration, error),
	catalog []migrationDefinition,
) (*postgresOperationalReadiness, error) {
	if ping == nil || readApplied == nil {
		return nil, errOperationalReadinessDatabaseRequired
	}
	if len(catalog) == 0 {
		return nil, errOperationalReadinessCatalogRequired
	}

	catalogCopy := make([]migrationDefinition, len(catalog))
	copy(catalogCopy, catalog)

	return &postgresOperationalReadiness{
		ping:        ping,
		readApplied: readApplied,
		catalog:     catalogCopy,
	}, nil
}

// Check returns success only when PostgreSQL responds and its ledger is the
// complete exact prefix of the embedded catalog. Every cause maps to one stable
// error so an HTTP caller cannot expose driver or schema diagnostics.
func (checker *postgresOperationalReadiness) Check(
	ctx context.Context,
) error {
	if checker == nil || checker.ping == nil || checker.readApplied == nil ||
		len(checker.catalog) == 0 {
		return errOperationalReadinessFailed
	}
	if err := checker.ping(ctx); err != nil {
		return errOperationalReadinessFailed
	}

	applied, err := checker.readApplied(ctx)
	if err != nil {
		return errOperationalReadinessFailed
	}
	pending, err := planPendingMigrations(checker.catalog, applied)
	if err != nil || len(pending) != 0 {
		return errOperationalReadinessFailed
	}

	return nil
}

// operationalReadinessLedgerSQL reads only immutable migration identities in
// catalog order; readiness never needs ledger write authority.
const operationalReadinessLedgerSQL = `SELECT version, name, checksum
FROM public.schema_migrations
ORDER BY version`

// readOperationalMigrationLedger loads only immutable migration identities.
// It returns the stable readiness sentinel for all database failures and never
// forwards a driver error that could contain deployment details.
func readOperationalMigrationLedger(
	ctx context.Context,
	database *sql.DB,
) ([]appliedMigration, error) {
	rows, err := database.QueryContext(
		ctx,
		operationalReadinessLedgerSQL,
	)
	if err != nil {
		return nil, errOperationalReadinessFailed
	}
	defer rows.Close()

	applied := make([]appliedMigration, 0)
	for rows.Next() {
		var migration appliedMigration
		var checksum []byte
		if err := rows.Scan(
			&migration.Version,
			&migration.Name,
			&checksum,
		); err != nil || len(checksum) != sha256.Size {
			return nil, errOperationalReadinessFailed
		}
		copy(migration.Checksum[:], checksum)
		applied = append(applied, migration)
	}
	if err := rows.Err(); err != nil {
		return nil, errOperationalReadinessFailed
	}

	return applied, nil
}

// operationalExternalHTTPSContextKey is private to this package so arbitrary
// request headers or downstream values cannot enable secure-origin behavior.
type operationalExternalHTTPSContextKey struct{}

// requestWithExternalHTTPS returns a shallow request copy marked as having
// arrived through the operator-declared HTTPS edge. Tests for cookie helpers can
// use this package-private seam without forging proxy headers.
func requestWithExternalHTTPS(r *http.Request) *http.Request {
	if r == nil {
		return nil
	}

	return r.WithContext(
		context.WithValue(
			r.Context(),
			operationalExternalHTTPSContextKey{},
			true,
		),
	)
}

// requestUsesSecureCookies reports direct TLS or the explicit external-HTTPS
// marker installed by the operational wrapper. It deliberately ignores
// Forwarded and X-Forwarded-Proto from untrusted clients.
func requestUsesSecureCookies(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}

	externalHTTPS, _ := r.Context().Value(
		operationalExternalHTTPSContextKey{},
	).(bool)
	return externalHTTPS
}

// operationalRequestIDGenerator creates opaque identifiers by authenticating
// unique counter inputs with one random process key. A 128-bit HMAC prefix makes
// collisions negligible and prevents an identifier from revealing request order
// or intervening request volume. No per-request entropy read can fail after the
// server begins accepting traffic.
type operationalRequestIDGenerator struct {
	key     [sha256.Size]byte
	counter atomic.Uint64
}

// newOperationalRequestIDGenerator reads the complete private key or fails
// startup. The key exists only in process memory and is never logged.
func newOperationalRequestIDGenerator(
	entropy io.Reader,
) (*operationalRequestIDGenerator, error) {
	if entropy == nil {
		return nil, errOperationalRequestIDUnavailable
	}

	generator := &operationalRequestIDGenerator{}
	if _, err := io.ReadFull(entropy, generator.key[:]); err != nil {
		return nil, errOperationalRequestIDUnavailable
	}

	return generator, nil
}

// Next returns the first 128 HMAC bits as 32 lowercase hexadecimal characters
// suitable for an HTTP header and structured log value.
func (generator *operationalRequestIDGenerator) Next() string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], generator.counter.Add(1))

	digest := hmac.New(sha256.New, generator.key[:])
	_, _ = digest.Write(counter[:])
	return hex.EncodeToString(digest.Sum(nil)[:16])
}

// operationalHTTPDependencies contains deterministic infrastructure supplied by
// production defaults or unit tests.
type operationalHTTPDependencies struct {
	entropy               io.Reader
	now                   func() time.Time
	maxConcurrentRequests int
}

// newOperationalHTTPHandler composes health routing, bounded application
// admission, public security headers, secure-origin context, panic recovery,
// request IDs, and one structured request record.
func newOperationalHTTPHandler(
	next http.Handler,
	readiness operationalReadinessChecker,
	config operationalConfig,
	logger *slog.Logger,
) (http.Handler, error) {
	return newOperationalHTTPHandlerWithDependencies(
		next,
		readiness,
		config,
		logger,
		operationalHTTPDependencies{
			entropy:               rand.Reader,
			now:                   time.Now,
			maxConcurrentRequests: defaultOperationalMaxConcurrentRequests,
		},
	)
}

// newOperationalHTTPHandlerWithDependencies is the test seam behind the public
// composition helper. Invalid infrastructure fails before serving traffic.
func newOperationalHTTPHandlerWithDependencies(
	next http.Handler,
	readiness operationalReadinessChecker,
	config operationalConfig,
	logger *slog.Logger,
	dependencies operationalHTTPDependencies,
) (http.Handler, error) {
	if next == nil || readiness == nil || logger == nil ||
		dependencies.now == nil ||
		dependencies.maxConcurrentRequests <= 0 ||
		!isValidOperationalHTTPAddress(config.httpAddress) ||
		!isSafeOperationalBind(config) {
		return nil, errOperationalHandlerDependencyInvalid
	}

	requestIDs, err := newOperationalRequestIDGenerator(
		dependencies.entropy,
	)
	if err != nil {
		return nil, err
	}

	semaphore := make(
		chan struct{},
		dependencies.maxConcurrentRequests,
	)
	limitedApplication := &operationalConcurrencyHandler{
		next:      next,
		semaphore: semaphore,
	}
	router := http.NewServeMux()
	router.Handle(
		operationalLivePath,
		http.HandlerFunc(operationalLiveHandler),
	)
	router.Handle(
		operationalReadyPath,
		&operationalConcurrencyHandler{
			next:      operationalReadyHandler(readiness),
			semaphore: semaphore,
		},
	)
	// The fallback preserves the application's own method-aware ServeMux. Exact
	// liveness stays outside the semaphore, while readiness shares the application's
	// budget because it performs PostgreSQL work.
	router.Handle("/", limitedApplication)

	return &operationalMiddleware{
		next:          router,
		externalHTTPS: config.externalHTTPS,
		logger:        logger,
		requestIDs:    requestIDs,
		now:           dependencies.now,
	}, nil
}

// operationalLiveHandler proves only that this process can serve HTTP. It does
// not touch PostgreSQL, preventing a dependency outage from triggering a restart
// loop that cannot repair the dependency.
func operationalLiveHandler(w http.ResponseWriter, r *http.Request) {
	operationalHealthHeaders(w.Header())
	if !operationalHealthMethodAllowed(w, r) {
		return
	}

	writeOperationalPlainText(
		w,
		r,
		http.StatusOK,
		operationalLiveBody,
	)
}

// operationalReadyHandler returns a handler that bounds one dependency check
// and maps every failure to the same non-cacheable response.
func operationalReadyHandler(
	readiness operationalReadinessChecker,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operationalHealthHeaders(w.Header())
		if !operationalHealthMethodAllowed(w, r) {
			return
		}

		ctx, cancel := context.WithTimeout(
			r.Context(),
			operationalReadinessTimeout,
		)
		err := readiness.Check(ctx)
		cancel()
		if err != nil {
			writeOperationalPlainText(
				w,
				r,
				http.StatusServiceUnavailable,
				operationalNotReadyBody,
			)
			return
		}

		writeOperationalPlainText(
			w,
			r,
			http.StatusOK,
			operationalReadyBody,
		)
	})
}

// operationalHealthHeaders prevents a probe result from outliving dependency
// state and prevents MIME sniffing of its fixed plain-text body.
func operationalHealthHeaders(headers http.Header) {
	headers.Set("Cache-Control", "no-store")
	headers.Set("X-Content-Type-Options", "nosniff")
}

// operationalHealthMethodAllowed accepts exactly GET and HEAD. A methodless
// exact ServeMux pattern reaches this check for every other method, allowing an
// explicit and testable 405 response instead of falling through to application
// routing.
func operationalHealthMethodAllowed(
	w http.ResponseWriter,
	r *http.Request,
) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}

	w.Header().Set("Allow", "GET, HEAD")
	writeOperationalPlainText(
		w,
		r,
		http.StatusMethodNotAllowed,
		operationalMethodNotAllowed,
	)
	return false
}

// writeOperationalPlainText writes an exact response and suppresses the body
// for HEAD while retaining the GET representation's Content-Length.
func writeOperationalPlainText(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	body string,
) {
	headers := w.Header()
	headers.Set("Content-Type", "text/plain; charset=utf-8")
	headers.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, body)
}

// operationalConcurrencyHandler admits at most cap(semaphore) application or
// readiness requests. It rejects excess work immediately, avoiding an unbounded
// queue of handlers and database waits during overload.
type operationalConcurrencyHandler struct {
	next      http.Handler
	semaphore chan struct{}
}

// ServeHTTP implements non-blocking global backpressure.
func (handler *operationalConcurrencyHandler) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	select {
	case handler.semaphore <- struct{}{}:
		defer func() {
			<-handler.semaphore
		}()
		handler.next.ServeHTTP(w, r)
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Retry-After", "1")
		writeOperationalPlainText(
			w,
			r,
			http.StatusServiceUnavailable,
			operationalBackpressureBody,
		)
	}
}

// operationalMiddleware is the outermost HTTP boundary. Its log fields avoid
// raw URLs, query strings, network addresses, and request headers by design.
type operationalMiddleware struct {
	next          http.Handler
	externalHTTPS bool
	logger        *slog.Logger
	requestIDs    *operationalRequestIDGenerator
	now           func() time.Time
}

// ServeHTTP installs security and correlation state, recovers panics without
// recording their value, and emits one http_request record per request. A panic
// also emits a separate fixed event that contains no recovered value.
func (middleware *operationalMiddleware) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	startedAt := middleware.now()
	requestID := middleware.requestIDs.Next()
	if middleware.externalHTTPS {
		r = requestWithExternalHTTPS(r)
	}
	// Clone the header map before replacing an untrusted correlation value. This
	// keeps middleware ownership local when a request is reused by a test or an
	// upstream in-process handler.
	r.Header = r.Header.Clone()
	if r.Header == nil {
		r.Header = make(http.Header)
	}

	setOperationalSecurityHeaders(w.Header(), requestUsesSecureCookies(r))
	// Set replaces an untrusted caller-supplied value before any downstream
	// handler can observe or reflect it.
	w.Header().Set("X-Request-ID", requestID)
	r.Header.Set("X-Request-ID", requestID)

	response := &operationalResponseWriter{ResponseWriter: w}
	defer func() {
		if recover() != nil {
			// The panic value is intentionally ignored. It may contain visitor data,
			// credentials, or attacker-controlled text.
			middleware.logger.Error(
				"http_panic",
				"request_id",
				requestID,
			)
			if !response.wroteHeader {
				response.Header().Set("Cache-Control", "no-store")
				http.Error(
					response,
					"internal server error",
					http.StatusInternalServerError,
				)
			}
		}

		pattern := r.Pattern
		if pattern == "" {
			pattern = "unmatched"
		}
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := middleware.now().Sub(startedAt)
		if duration < 0 {
			duration = 0
		}

		middleware.logger.Info(
			"http_request",
			"request_id",
			requestID,
			"method",
			operationalLogMethod(r.Method),
			"pattern",
			pattern,
			"status",
			status,
			"bytes",
			response.bytes,
			"duration_ms",
			duration.Milliseconds(),
		)
	}()

	middleware.next.ServeHTTP(response, r)
}

// operationalLogMethod keeps the telemetry field inside the application's
// closed HTTP vocabulary. A method token is client-controlled even though the
// HTTP parser validates its syntax, so unknown values must not become arbitrary
// log content.
func operationalLogMethod(method string) string {
	switch method {
	case http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}

// setOperationalSecurityHeaders applies a baseline that is compatible with the
// checked-in same-origin HTML, CSS, JavaScript, and images. Route-specific admin
// policy may overwrite these values with a stricter contract downstream.
func setOperationalSecurityHeaders(
	headers http.Header,
	secureOrigin bool,
) {
	headers.Set(
		"Content-Security-Policy",
		"default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'",
	)
	headers.Set("Cross-Origin-Opener-Policy", "same-origin")
	headers.Set("Cross-Origin-Resource-Policy", "same-origin")
	headers.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	headers.Set("Referrer-Policy", "no-referrer")
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("X-Frame-Options", "DENY")
	if secureOrigin {
		headers.Set("Strict-Transport-Security", operationalHSTSValue)
	}
}

// operationalResponseWriter records status and successfully written bytes while
// preserving access to optional interfaces through ResponseController.Unwrap.
type operationalResponseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

// Unwrap lets net/http.ResponseController reach the original writer.
func (writer *operationalResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

// WriteHeader records only the first status, matching net/http semantics.
func (writer *operationalResponseWriter) WriteHeader(status int) {
	if writer.wroteHeader {
		return
	}
	writer.wroteHeader = true
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

// Write records an implicit 200 before forwarding body bytes.
func (writer *operationalResponseWriter) Write(body []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}

	written, err := writer.ResponseWriter.Write(body)
	writer.bytes += written
	return written, err
}

// Compile-time assertions protect the production interfaces used by wiring.
var (
	_ operationalReadinessChecker = (*postgresOperationalReadiness)(nil)
	_ http.Handler                = (*operationalConcurrencyHandler)(nil)
	_ http.Handler                = (*operationalMiddleware)(nil)
	_ interface {
		Unwrap() http.ResponseWriter
	} = (*operationalResponseWriter)(nil)
)
