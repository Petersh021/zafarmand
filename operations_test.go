package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// mapOperationalEnvironment returns an isolated environment lookup. Missing
// keys retain LookupEnv's false result instead of becoming present blank values.
func mapOperationalEnvironment(
	values map[string]string,
) environmentLookup {
	return func(name string) (string, bool) {
		value, exists := values[name]
		return value, exists
	}
}

// TestLoadOperationalConfig covers the safe defaults and every strict process
// setting accepted by the HTTP boundary.
func TestLoadOperationalConfig(t *testing.T) {
	tests := []struct {
		name          string
		lookup        environmentLookup
		wantAddress   string
		wantHTTPS     bool
		expectedError error
	}{
		{
			name:        "safe defaults",
			lookup:      mapOperationalEnvironment(nil),
			wantAddress: defaultOperationalHTTPAddress,
		},
		{
			name: "explicit loopback and HTTPS",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalHTTPAddressEnvironmentName:   "[::1]:8443",
				operationalExternalHTTPSEnvironmentName: "true",
			}),
			wantAddress: "[::1]:8443",
			wantHTTPS:   true,
		},
		{
			name: "explicit all interfaces with HTTPS",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalHTTPAddressEnvironmentName:   ":8080",
				operationalExternalHTTPSEnvironmentName: "true",
			}),
			wantAddress: ":8080",
			wantHTTPS:   true,
		},
		{
			name: "all interfaces without HTTPS",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalHTTPAddressEnvironmentName:   ":8080",
				operationalExternalHTTPSEnvironmentName: "false",
			}),
			expectedError: errOperationalPublicBindRequiresHTTPS,
		},
		{
			name: "public IP without HTTPS",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalHTTPAddressEnvironmentName: "192.0.2.10:8080",
			}),
			expectedError: errOperationalPublicBindRequiresHTTPS,
		},
		{
			name: "hostname without HTTPS",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalHTTPAddressEnvironmentName: "app.internal:8080",
			}),
			expectedError: errOperationalPublicBindRequiresHTTPS,
		},
		{
			name:          "missing lookup",
			expectedError: errOperationalEnvironmentLookupRequired,
		},
		{
			name: "blank address",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalHTTPAddressEnvironmentName: "",
			}),
			expectedError: errOperationalHTTPAddressInvalid,
		},
		{
			name: "address surrounding whitespace",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalHTTPAddressEnvironmentName: " 127.0.0.1:8080",
			}),
			expectedError: errOperationalHTTPAddressInvalid,
		},
		{
			name: "URL instead of address",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalHTTPAddressEnvironmentName: "http://localhost:8080",
			}),
			expectedError: errOperationalHTTPAddressInvalid,
		},
		{
			name: "service port",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalHTTPAddressEnvironmentName: "localhost:http",
			}),
			expectedError: errOperationalHTTPAddressInvalid,
		},
		{
			name: "zero port",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalHTTPAddressEnvironmentName: "localhost:0",
			}),
			expectedError: errOperationalHTTPAddressInvalid,
		},
		{
			name: "invalid hostname",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalHTTPAddressEnvironmentName: "bad_host:8080",
			}),
			expectedError: errOperationalHTTPAddressInvalid,
		},
		{
			name: "blank HTTPS",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalExternalHTTPSEnvironmentName: "",
			}),
			expectedError: errOperationalExternalHTTPSInvalid,
		},
		{
			name: "case folded HTTPS",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalExternalHTTPSEnvironmentName: "TRUE",
			}),
			expectedError: errOperationalExternalHTTPSInvalid,
		},
		{
			name: "numeric HTTPS",
			lookup: mapOperationalEnvironment(map[string]string{
				operationalExternalHTTPSEnvironmentName: "1",
			}),
			expectedError: errOperationalExternalHTTPSInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := loadOperationalConfig(test.lookup)
			if !errors.Is(err, test.expectedError) {
				t.Fatalf("error: got %v, want %v", err, test.expectedError)
			}
			if test.expectedError != nil {
				if strings.Contains(err.Error(), "bad_host") ||
					strings.Contains(err.Error(), "http://") {
					t.Fatalf("configuration error reflects value: %q", err)
				}
				return
			}
			if config.httpAddress != test.wantAddress {
				t.Errorf(
					"address: got %q, want %q",
					config.httpAddress,
					test.wantAddress,
				)
			}
			if config.externalHTTPS != test.wantHTTPS {
				t.Errorf(
					"external HTTPS: got %t, want %t",
					config.externalHTTPS,
					test.wantHTTPS,
				)
			}
		})
	}
}

// TestOperationalHTTPAddressValidation covers representative accepted address
// forms independently from environment loading.
func TestOperationalHTTPAddressValidation(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1:8080",
		"0.0.0.0:443",
		":8080",
		"localhost:65535",
		"app.internal:9000",
		"[::]:8080",
	} {
		if !isValidOperationalHTTPAddress(address) {
			t.Errorf("valid address %q was rejected", address)
		}
	}

	for _, address := range []string{
		"localhost",
		"localhost:",
		"localhost:65536",
		"localhost:-1",
		"-bad.example:80",
		"bad-.example:80",
		"example.com.:80",
		"[fe80::1%zone]:80",
		"例.example:80",
	} {
		if isValidOperationalHTTPAddress(address) {
			t.Errorf("invalid address %q was accepted", address)
		}
	}
}

// TestRequestUsesSecureCookies proves that only direct TLS or the private
// context marker can enable Secure cookies. Proxy-like headers are inert.
func TestRequestUsesSecureCookies(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	request.Header.Set("Forwarded", "proto=https")
	request.Header.Set("X-Forwarded-Proto", "https")
	if requestUsesSecureCookies(request) {
		t.Fatal("untrusted proxy headers enabled Secure cookies")
	}

	marked := requestWithExternalHTTPS(request)
	if !requestUsesSecureCookies(marked) {
		t.Fatal("external HTTPS marker did not enable Secure cookies")
	}
	if requestUsesSecureCookies(request) {
		t.Fatal("external HTTPS marker mutated the original request")
	}

	directTLS := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	directTLS.TLS = &tls.ConnectionState{}
	if !requestUsesSecureCookies(directTLS) {
		t.Fatal("direct TLS did not enable Secure cookies")
	}
	if requestUsesSecureCookies(nil) || requestWithExternalHTTPS(nil) != nil {
		t.Fatal("nil request helpers did not fail closed")
	}
}

// testMigrationCatalog creates two deterministic definitions with real
// checksums so readiness exercises the same immutable identity comparison as
// the migration runner.
func testMigrationCatalog() []migrationDefinition {
	catalog := []migrationDefinition{
		{
			Version: 1,
			Name:    "first",
			UpSQL:   "SELECT 1",
			DownSQL: "SELECT 1",
		},
		{
			Version: 2,
			Name:    "second",
			UpSQL:   "SELECT 2",
			DownSQL: "SELECT 2",
		},
	}
	for index := range catalog {
		catalog[index].Checksum = migrationChecksum(
			catalog[index].Version,
			catalog[index].Name,
			catalog[index].UpSQL,
			catalog[index].DownSQL,
		)
	}
	return catalog
}

// appliedMigrationsForCatalog maps trusted definitions to their ledger shape.
func appliedMigrationsForCatalog(
	catalog []migrationDefinition,
) []appliedMigration {
	applied := make([]appliedMigration, len(catalog))
	for index, migration := range catalog {
		applied[index] = appliedMigration{
			Version:  migration.Version,
			Name:     migration.Name,
			Checksum: migration.Checksum,
		}
	}
	return applied
}

// TestPostgresOperationalReadiness verifies connectivity, complete-ledger, and
// drift boundaries while proving every private cause becomes one sentinel.
func TestPostgresOperationalReadiness(t *testing.T) {
	catalog := testMigrationCatalog()
	current := appliedMigrationsForCatalog(catalog)
	secretError := errors.New("postgres://user:secret@example/private")

	tests := []struct {
		name        string
		pingError   error
		readError   error
		applied     []appliedMigration
		wantReady   bool
		wantReadRun bool
	}{
		{
			name:        "fully current",
			applied:     current,
			wantReady:   true,
			wantReadRun: true,
		},
		{
			name:      "ping failed",
			pingError: secretError,
			applied:   current,
		},
		{
			name:        "ledger read failed",
			readError:   secretError,
			applied:     current,
			wantReadRun: true,
		},
		{
			name:        "pending migration",
			applied:     current[:1],
			wantReadRun: true,
		},
		{
			name: "name drift",
			applied: []appliedMigration{
				current[0],
				{
					Version:  current[1].Version,
					Name:     "changed",
					Checksum: current[1].Checksum,
				},
			},
			wantReadRun: true,
		},
		{
			name: "checksum drift",
			applied: []appliedMigration{
				current[0],
				{
					Version: current[1].Version,
					Name:    current[1].Name,
				},
			},
			wantReadRun: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			readRan := false
			checker, err := newPostgresOperationalReadinessWithDependencies(
				func(context.Context) error {
					return test.pingError
				},
				func(context.Context) ([]appliedMigration, error) {
					readRan = true
					return test.applied, test.readError
				},
				catalog,
			)
			if err != nil {
				t.Fatalf("construct readiness checker: %v", err)
			}

			err = checker.Check(t.Context())
			if test.wantReady && err != nil {
				t.Fatalf("current database was not ready: %v", err)
			}
			if !test.wantReady && !errors.Is(
				err,
				errOperationalReadinessFailed,
			) {
				t.Fatalf("readiness error: got %v, want sentinel", err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("readiness error exposes cause: %q", err)
			}
			if readRan != test.wantReadRun {
				t.Errorf("ledger read ran: got %t, want %t", readRan, test.wantReadRun)
			}
		})
	}
}

// TestPostgresOperationalReadinessConstruction validates dependencies and
// proves the checker owns an immutable copy of the migration catalog.
func TestPostgresOperationalReadinessConstruction(t *testing.T) {
	catalog := testMigrationCatalog()

	if _, err := newPostgresOperationalReadinessWithDependencies(
		nil,
		func(context.Context) ([]appliedMigration, error) { return nil, nil },
		catalog,
	); !errors.Is(err, errOperationalReadinessDatabaseRequired) {
		t.Fatalf("missing ping error: got %v", err)
	}
	if _, err := newPostgresOperationalReadinessWithDependencies(
		func(context.Context) error { return nil },
		nil,
		catalog,
	); !errors.Is(err, errOperationalReadinessDatabaseRequired) {
		t.Fatalf("missing ledger reader error: got %v", err)
	}
	if _, err := newPostgresOperationalReadinessWithDependencies(
		func(context.Context) error { return nil },
		func(context.Context) ([]appliedMigration, error) { return nil, nil },
		nil,
	); !errors.Is(err, errOperationalReadinessCatalogRequired) {
		t.Fatalf("missing catalog error: got %v", err)
	}
	if _, err := newPostgresOperationalReadiness(nil); !errors.Is(
		err,
		errOperationalReadinessDatabaseRequired,
	) {
		t.Fatalf("nil production database error: got %v", err)
	}

	checker, err := newPostgresOperationalReadinessWithDependencies(
		func(context.Context) error { return nil },
		func(context.Context) ([]appliedMigration, error) {
			return appliedMigrationsForCatalog(testMigrationCatalog()), nil
		},
		catalog,
	)
	if err != nil {
		t.Fatalf("construct checker: %v", err)
	}
	catalog[0].Name = "mutated"
	if checker.catalog[0].Name != "first" {
		t.Fatal("readiness checker retained caller catalog backing array")
	}
}

// recordingOperationalReadiness captures calls and context deadlines.
type recordingOperationalReadiness struct {
	mu          sync.Mutex
	calls       int
	hasDeadline bool
	err         error
}

// Check implements operationalReadinessChecker.
func (readiness *recordingOperationalReadiness) Check(
	ctx context.Context,
) error {
	readiness.mu.Lock()
	defer readiness.mu.Unlock()
	readiness.calls++
	_, readiness.hasDeadline = ctx.Deadline()
	return readiness.err
}

// snapshot returns synchronized readiness observations.
func (readiness *recordingOperationalReadiness) snapshot() (int, bool) {
	readiness.mu.Lock()
	defer readiness.mu.Unlock()
	return readiness.calls, readiness.hasDeadline
}

// TestOperationalHealthHandlers verifies exact status, body, method, cache, and
// dependency behavior for both probes.
func TestOperationalHealthHandlers(t *testing.T) {
	secretError := errors.New("readiness-secret-must-not-leak")
	tests := []struct {
		name           string
		method         string
		path           string
		readyError     error
		wantStatus     int
		wantBody       string
		wantLength     int
		wantAllow      string
		wantReadyCalls int
	}{
		{
			name:       "live GET",
			method:     http.MethodGet,
			path:       operationalLivePath,
			wantStatus: http.StatusOK,
			wantBody:   operationalLiveBody,
			wantLength: len(operationalLiveBody),
		},
		{
			name:       "live HEAD",
			method:     http.MethodHead,
			path:       operationalLivePath,
			wantStatus: http.StatusOK,
			wantLength: len(operationalLiveBody),
		},
		{
			name:       "live rejects POST",
			method:     http.MethodPost,
			path:       operationalLivePath,
			wantStatus: http.StatusMethodNotAllowed,
			wantBody:   operationalMethodNotAllowed,
			wantLength: len(operationalMethodNotAllowed),
			wantAllow:  "GET, HEAD",
		},
		{
			name:           "ready GET",
			method:         http.MethodGet,
			path:           operationalReadyPath,
			wantStatus:     http.StatusOK,
			wantBody:       operationalReadyBody,
			wantLength:     len(operationalReadyBody),
			wantReadyCalls: 1,
		},
		{
			name:           "not ready is fixed",
			method:         http.MethodGet,
			path:           operationalReadyPath,
			readyError:     secretError,
			wantStatus:     http.StatusServiceUnavailable,
			wantBody:       operationalNotReadyBody,
			wantLength:     len(operationalNotReadyBody),
			wantReadyCalls: 1,
		},
		{
			name:           "ready HEAD failure",
			method:         http.MethodHead,
			path:           operationalReadyPath,
			readyError:     secretError,
			wantStatus:     http.StatusServiceUnavailable,
			wantLength:     len(operationalNotReadyBody),
			wantReadyCalls: 1,
		},
		{
			name:       "ready rejects DELETE before check",
			method:     http.MethodDelete,
			path:       operationalReadyPath,
			wantStatus: http.StatusMethodNotAllowed,
			wantBody:   operationalMethodNotAllowed,
			wantLength: len(operationalMethodNotAllowed),
			wantAllow:  "GET, HEAD",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			readiness := &recordingOperationalReadiness{err: test.readyError}
			var handler http.Handler = http.HandlerFunc(operationalLiveHandler)
			if test.path == operationalReadyPath {
				handler = operationalReadyHandler(readiness)
			}

			request := httptest.NewRequest(test.method, test.path, nil)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Errorf("status: got %d, want %d", recorder.Code, test.wantStatus)
			}
			if recorder.Body.String() != test.wantBody {
				t.Errorf("body: got %q, want %q", recorder.Body.String(), test.wantBody)
			}
			if recorder.Header().Get("Content-Length") !=
				strconv.Itoa(test.wantLength) {
				t.Errorf(
					"Content-Length: got %q, want %d",
					recorder.Header().Get("Content-Length"),
					test.wantLength,
				)
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control: got %q", recorder.Header().Get("Cache-Control"))
			}
			if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("X-Content-Type-Options missing")
			}
			if recorder.Header().Get("Allow") != test.wantAllow {
				t.Errorf("Allow: got %q, want %q", recorder.Header().Get("Allow"), test.wantAllow)
			}
			if strings.Contains(recorder.Body.String(), "secret") {
				t.Fatal("readiness response exposes dependency error")
			}

			calls, hasDeadline := readiness.snapshot()
			if calls != test.wantReadyCalls {
				t.Errorf("readiness calls: got %d, want %d", calls, test.wantReadyCalls)
			}
			if calls > 0 && !hasDeadline {
				t.Error("readiness check received no deadline")
			}
		})
	}
}

// zeroReader supplies deterministic request-ID entropy.
type zeroReader struct{}

// Read fills every destination byte and always succeeds.
func (zeroReader) Read(destination []byte) (int, error) {
	clear(destination)
	return len(destination), nil
}

// failingReader always fails without exposing its private marker through the
// request-ID constructor.
type failingReader struct{}

// Read implements io.Reader.
func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy-private-cause")
}

// TestOperationalRequestIDGenerator verifies the keyed opaque sequence and
// redacted startup failure. The fixed values also prevent a future change from
// exposing the internal counter directly.
func TestOperationalRequestIDGenerator(t *testing.T) {
	generator, err := newOperationalRequestIDGenerator(zeroReader{})
	if err != nil {
		t.Fatalf("construct request ID generator: %v", err)
	}
	if got := generator.Next(); got != "a4a11ce5fbe8f96bf3028035286c2c92" {
		t.Errorf("first request ID: got %q", got)
	}
	if got := generator.Next(); got != "c9fd8f5cd4e947bb0901fdf4726309b3" {
		t.Errorf("second request ID: got %q", got)
	}
	if _, err := newOperationalRequestIDGenerator(nil); !errors.Is(
		err,
		errOperationalRequestIDUnavailable,
	) {
		t.Fatalf("nil entropy error: got %v", err)
	}
	if _, err := newOperationalRequestIDGenerator(failingReader{}); !errors.Is(
		err,
		errOperationalRequestIDUnavailable,
	) || strings.Contains(err.Error(), "private") {
		t.Fatalf("entropy error was not redacted: %v", err)
	}
}

// lockedBuffer makes concurrent slog writes safe in backpressure tests.
type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

// Write implements io.Writer under a mutex.
func (writer *lockedBuffer) Write(value []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.buffer.Write(value)
}

// String returns a synchronized copy of the accumulated log text.
func (writer *lockedBuffer) String() string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.buffer.String()
}

// newOperationalTestHandler constructs the full wrapper with deterministic
// entropy and caller-controlled concurrency.
func newOperationalTestHandler(
	t *testing.T,
	next http.Handler,
	readiness operationalReadinessChecker,
	config operationalConfig,
	logWriter io.Writer,
	now func() time.Time,
	maxConcurrentRequests int,
) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewJSONHandler(logWriter, nil))
	handler, err := newOperationalHTTPHandlerWithDependencies(
		next,
		readiness,
		config,
		logger,
		operationalHTTPDependencies{
			entropy:               zeroReader{},
			now:                   now,
			maxConcurrentRequests: maxConcurrentRequests,
		},
	)
	if err != nil {
		t.Fatalf("construct operational handler: %v", err)
	}
	return handler
}

// decodeOperationalLogRecords parses newline-delimited slog JSON records.
func decodeOperationalLogRecords(
	t *testing.T,
	logText string,
) []map[string]any {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(logText), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode operational log %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

// TestOperationalTelemetryPrivacy verifies the complete structured request
// contract and ensures attacker-controlled metadata never reaches the log.
func TestOperationalTelemetryPrivacy(t *testing.T) {
	const (
		pathSecret    = "visitor-email@example.test"
		querySecret   = "query-private-value"
		incomingID    = "attacker-request-id"
		networkSecret = "203.0.113.99"
		headerSecret  = "header-private-value"
		responseBody  = "hello"
	)

	inner := http.NewServeMux()
	var downstreamRequestID string
	inner.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		downstreamRequestID = r.Header.Get("X-Request-ID")
		_, _ = io.WriteString(w, responseBody)
	})

	start := time.Date(2026, time.August, 22, 10, 0, 0, 0, time.UTC)
	clockCalls := 0
	now := func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			return start
		}
		return start.Add(1500 * time.Millisecond)
	}
	logs := &lockedBuffer{}
	handler := newOperationalTestHandler(
		t,
		inner,
		operationalReadinessCheckFunc(func(context.Context) error { return nil }),
		operationalConfig{httpAddress: defaultOperationalHTTPAddress},
		logs,
		now,
		2,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"http://example.test/items/"+pathSecret+"?token="+querySecret,
		nil,
	)
	request.RemoteAddr = networkSecret + ":4567"
	request.Header.Set("X-Request-ID", incomingID)
	request.Header.Set("User-Agent", headerSecret)
	request.Header.Set("Referer", "https://example.test/"+headerSecret)
	request.Header.Set("Cookie", "session="+headerSecret)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	requestID := recorder.Header().Get("X-Request-ID")
	if requestID != "a4a11ce5fbe8f96bf3028035286c2c92" {
		t.Errorf("response request ID: got %q", requestID)
	}
	if downstreamRequestID != requestID || downstreamRequestID == incomingID {
		t.Errorf(
			"downstream request ID: got %q, response %q",
			downstreamRequestID,
			requestID,
		)
	}
	if recorder.Code != http.StatusOK || recorder.Body.String() != responseBody {
		t.Fatalf("response: status=%d body=%q", recorder.Code, recorder.Body.String())
	}

	logText := logs.String()
	for _, forbidden := range []string{
		pathSecret,
		querySecret,
		incomingID,
		networkSecret,
		headerSecret,
		"User-Agent",
		"Referer",
		"Cookie",
	} {
		if strings.Contains(logText, forbidden) {
			t.Errorf("operational log contains forbidden value %q", forbidden)
		}
	}

	records := decodeOperationalLogRecords(t, logText)
	if len(records) != 1 {
		t.Fatalf("log records: got %d, want 1", len(records))
	}
	record := records[0]
	for key, want := range map[string]any{
		"msg":         "http_request",
		"request_id":  requestID,
		"method":      http.MethodGet,
		"pattern":     "GET /items/{id}",
		"status":      float64(http.StatusOK),
		"bytes":       float64(len(responseBody)),
		"duration_ms": float64(1500),
	} {
		if got := record[key]; got != want {
			t.Errorf("log field %s: got %#v, want %#v", key, got, want)
		}
	}
	allowedKeys := map[string]bool{
		"time": true, "level": true, "msg": true,
		"request_id": true, "method": true, "pattern": true,
		"status": true, "bytes": true, "duration_ms": true,
	}
	for key := range record {
		if !allowedKeys[key] {
			t.Errorf("unexpected request log field %q", key)
		}
	}
}

// TestOperationalLogMethod prevents an arbitrary but syntactically valid HTTP
// method token from becoming attacker-controlled structured-log content.
func TestOperationalLogMethod(t *testing.T) {
	for _, test := range []struct {
		method string
		want   string
	}{
		{method: http.MethodGet, want: http.MethodGet},
		{method: http.MethodPost, want: http.MethodPost},
		{method: http.MethodOptions, want: http.MethodOptions},
		{method: "PRIVATEVISITORVALUE", want: "OTHER"},
		{want: "OTHER"},
	} {
		if got := operationalLogMethod(test.method); got != test.want {
			t.Errorf(
				"method %q: got %q, want %q",
				test.method,
				got,
				test.want,
			)
		}
	}
}

// TestOperationalSecurityHeadersAndHTTPSMarker verifies baseline policy, HSTS,
// and the context signal consumed by cookie helpers.
func TestOperationalSecurityHeadersAndHTTPSMarker(t *testing.T) {
	tests := []struct {
		name          string
		externalHTTPS bool
		directTLS     bool
		wantSecure    bool
	}{
		{name: "plain HTTP"},
		{name: "external HTTPS", externalHTTPS: true, wantSecure: true},
		{name: "direct TLS", directTLS: true, wantSecure: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var downstreamSecure bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				downstreamSecure = requestUsesSecureCookies(r)
				w.WriteHeader(http.StatusNoContent)
			})
			handler := newOperationalTestHandler(
				t,
				next,
				operationalReadinessCheckFunc(func(context.Context) error { return nil }),
				operationalConfig{
					httpAddress:   defaultOperationalHTTPAddress,
					externalHTTPS: test.externalHTTPS,
				},
				io.Discard,
				time.Now,
				1,
			)
			request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
			request.Header.Set("X-Forwarded-Proto", "https")
			if test.directTLS {
				request.TLS = &tls.ConnectionState{}
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if downstreamSecure != test.wantSecure {
				t.Errorf("downstream secure origin: got %t, want %t", downstreamSecure, test.wantSecure)
			}
			wantHSTS := ""
			if test.wantSecure {
				wantHSTS = operationalHSTSValue
			}
			if got := recorder.Header().Get("Strict-Transport-Security"); got != wantHSTS {
				t.Errorf("HSTS: got %q, want %q", got, wantHSTS)
			}
			for header, want := range map[string]string{
				"Content-Security-Policy":      "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'",
				"Cross-Origin-Opener-Policy":   "same-origin",
				"Cross-Origin-Resource-Policy": "same-origin",
				"Permissions-Policy":           "camera=(), microphone=(), geolocation=()",
				"Referrer-Policy":              "no-referrer",
				"X-Content-Type-Options":       "nosniff",
				"X-Frame-Options":              "DENY",
			} {
				if got := recorder.Header().Get(header); got != want {
					t.Errorf("%s: got %q, want %q", header, got, want)
				}
			}
		})
	}
}

// TestOperationalPanicRecovery proves that an attacker-controlled panic value
// is absent from both response and logs while access telemetry records the 500.
func TestOperationalPanicRecovery(t *testing.T) {
	const panicSecret = "panic-secret-visitor-value"
	inner := http.NewServeMux()
	inner.HandleFunc("GET /panic/{id}", func(http.ResponseWriter, *http.Request) {
		panic(panicSecret)
	})

	logs := &lockedBuffer{}
	handler := newOperationalTestHandler(
		t,
		inner,
		operationalReadinessCheckFunc(func(context.Context) error { return nil }),
		operationalConfig{httpAddress: defaultOperationalHTTPAddress},
		logs,
		time.Now,
		1,
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/panic/private-path", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError ||
		recorder.Body.String() != "internal server error\n" {
		t.Fatalf("panic response: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("panic response Cache-Control: got %q", recorder.Header().Get("Cache-Control"))
	}
	logText := logs.String()
	if strings.Contains(logText, panicSecret) || strings.Contains(logText, "private-path") {
		t.Fatalf("panic log exposes private value: %s", logText)
	}

	records := decodeOperationalLogRecords(t, logText)
	if len(records) != 2 {
		t.Fatalf("panic log records: got %d, want 2", len(records))
	}
	var panicRecord, requestRecord map[string]any
	for _, record := range records {
		switch record["msg"] {
		case "http_panic":
			panicRecord = record
		case "http_request":
			requestRecord = record
		}
	}
	if panicRecord == nil || requestRecord == nil {
		t.Fatalf("panic records incomplete: %#v", records)
	}
	if panicRecord["request_id"] != requestRecord["request_id"] {
		t.Error("panic and request records use different request IDs")
	}
	if requestRecord["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("panic request status: got %#v", requestRecord["status"])
	}
	for key := range panicRecord {
		if key != "time" && key != "level" && key != "msg" && key != "request_id" {
			t.Errorf("panic log contains unexpected field %q", key)
		}
	}
}

// TestOperationalConcurrencyBackpressure proves that application requests are
// bounded, excess work is rejected immediately, and health remains responsive.
func TestOperationalConcurrencyBackpressure(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hold" {
			entered <- struct{}{}
			<-release
		}
		_, _ = io.WriteString(w, "application\n")
	})
	readiness := &recordingOperationalReadiness{}
	handler := newOperationalTestHandler(
		t,
		next,
		readiness,
		operationalConfig{httpAddress: defaultOperationalHTTPAddress},
		io.Discard,
		time.Now,
		1,
	)

	firstRecorder := httptest.NewRecorder()
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		handler.ServeHTTP(
			firstRecorder,
			httptest.NewRequest(http.MethodGet, "/hold", nil),
		)
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not enter application")
	}

	overloadRecorder := httptest.NewRecorder()
	handler.ServeHTTP(
		overloadRecorder,
		httptest.NewRequest(http.MethodGet, "/overloaded", nil),
	)
	if overloadRecorder.Code != http.StatusServiceUnavailable ||
		overloadRecorder.Body.String() != operationalBackpressureBody {
		t.Errorf(
			"overload response: status=%d body=%q",
			overloadRecorder.Code,
			overloadRecorder.Body.String(),
		)
	}
	if overloadRecorder.Header().Get("Retry-After") != "1" ||
		overloadRecorder.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("overload headers: %#v", overloadRecorder.Header())
	}

	liveRecorder := httptest.NewRecorder()
	handler.ServeHTTP(
		liveRecorder,
		httptest.NewRequest(http.MethodGet, operationalLivePath, nil),
	)
	if liveRecorder.Code != http.StatusOK {
		t.Errorf("live probe at capacity: got %d", liveRecorder.Code)
	}
	readyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(
		readyRecorder,
		httptest.NewRequest(http.MethodGet, operationalReadyPath, nil),
	)
	if readyRecorder.Code != http.StatusServiceUnavailable {
		t.Errorf("ready probe at capacity: got %d", readyRecorder.Code)
	}
	if calls, _ := readiness.snapshot(); calls != 0 {
		t.Errorf("readiness check entered at capacity: got %d calls", calls)
	}

	close(release)
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("admitted request did not finish after release")
	}
	if firstRecorder.Code != http.StatusOK {
		t.Errorf("admitted request status: got %d", firstRecorder.Code)
	}

	readyAfterRelease := httptest.NewRecorder()
	handler.ServeHTTP(
		readyAfterRelease,
		httptest.NewRequest(http.MethodGet, operationalReadyPath, nil),
	)
	if readyAfterRelease.Code != http.StatusOK {
		t.Errorf("ready probe after release: got %d", readyAfterRelease.Code)
	}
	if calls, _ := readiness.snapshot(); calls != 1 {
		t.Errorf("readiness calls after release: got %d, want 1", calls)
	}
}

// TestOperationalHealthPathsAreExact ensures near-miss paths remain owned by
// the application rather than receiving a misleading healthy response.
func TestOperationalHealthPathsAreExact(t *testing.T) {
	applicationCalls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		applicationCalls++
		w.WriteHeader(http.StatusTeapot)
	})
	handler := newOperationalTestHandler(
		t,
		next,
		operationalReadinessCheckFunc(func(context.Context) error { return nil }),
		operationalConfig{httpAddress: defaultOperationalHTTPAddress},
		io.Discard,
		time.Now,
		1,
	)

	for _, path := range []string{
		operationalLivePath + "/",
		operationalReadyPath + "/extra",
		"/health",
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(
			recorder,
			httptest.NewRequest(http.MethodGet, path, nil),
		)
		if recorder.Code != http.StatusTeapot {
			t.Errorf("near-miss path %q status: got %d", path, recorder.Code)
		}
	}
	if applicationCalls != 3 {
		t.Errorf("application calls: got %d, want 3", applicationCalls)
	}
}

// TestOperationalHTTPHandlerDependencyValidation protects startup from partial
// observability or unbounded admission configuration.
func TestOperationalHTTPHandlerDependencyValidation(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	readiness := operationalReadinessCheckFunc(func(context.Context) error { return nil })
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	validDependencies := operationalHTTPDependencies{
		entropy:               zeroReader{},
		now:                   time.Now,
		maxConcurrentRequests: 1,
	}
	validConfig := operationalConfig{httpAddress: defaultOperationalHTTPAddress}

	tests := []struct {
		name         string
		next         http.Handler
		readiness    operationalReadinessChecker
		config       operationalConfig
		logger       *slog.Logger
		dependencies operationalHTTPDependencies
		wantEntropy  bool
	}{
		{name: "missing application", readiness: readiness, config: validConfig, logger: logger, dependencies: validDependencies},
		{name: "missing readiness", next: next, config: validConfig, logger: logger, dependencies: validDependencies},
		{name: "missing logger", next: next, readiness: readiness, config: validConfig, dependencies: validDependencies},
		{name: "invalid address", next: next, readiness: readiness, config: operationalConfig{}, logger: logger, dependencies: validDependencies},
		{name: "missing clock", next: next, readiness: readiness, config: validConfig, logger: logger, dependencies: operationalHTTPDependencies{entropy: zeroReader{}, maxConcurrentRequests: 1}},
		{name: "zero concurrency", next: next, readiness: readiness, config: validConfig, logger: logger, dependencies: operationalHTTPDependencies{entropy: zeroReader{}, now: time.Now}},
		{name: "entropy failure", next: next, readiness: readiness, config: validConfig, logger: logger, dependencies: operationalHTTPDependencies{entropy: failingReader{}, now: time.Now, maxConcurrentRequests: 1}, wantEntropy: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newOperationalHTTPHandlerWithDependencies(
				test.next,
				test.readiness,
				test.config,
				test.logger,
				test.dependencies,
			)
			want := errOperationalHandlerDependencyInvalid
			if test.wantEntropy {
				want = errOperationalRequestIDUnavailable
			}
			if !errors.Is(err, want) {
				t.Fatalf("error: got %v, want %v", err, want)
			}
		})
	}
}

// TestOperationalResponseWriter verifies implicit status, repeated-header, byte,
// and unwrap behavior independently of middleware.
func TestOperationalResponseWriter(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := &operationalResponseWriter{ResponseWriter: recorder}
	writer.WriteHeader(http.StatusCreated)
	writer.WriteHeader(http.StatusForbidden)
	written, err := writer.Write([]byte("body"))
	if err != nil || written != 4 {
		t.Fatalf("write: bytes=%d err=%v", written, err)
	}
	if writer.status != http.StatusCreated || writer.bytes != 4 ||
		!writer.wroteHeader || recorder.Code != http.StatusCreated {
		t.Errorf("writer state: %#v recorder=%d", writer, recorder.Code)
	}
	if writer.Unwrap() != recorder {
		t.Fatal("response writer did not unwrap original")
	}

	implicitRecorder := httptest.NewRecorder()
	implicit := &operationalResponseWriter{ResponseWriter: implicitRecorder}
	_, _ = implicit.Write([]byte("x"))
	if implicit.status != http.StatusOK || implicitRecorder.Code != http.StatusOK {
		t.Errorf("implicit status: writer=%d recorder=%d", implicit.status, implicitRecorder.Code)
	}
}
