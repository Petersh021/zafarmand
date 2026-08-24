package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// TestNewHTTPServerAppliesOperationalLimits keeps the production transport
// constructor aligned with the reviewed Stage 25 resource boundaries.
func TestNewHTTPServerAppliesOperationalLimits(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	server := newHTTPServer("127.0.0.1:8080", handler, logger)
	if server.Addr != "127.0.0.1:8080" || server.Handler == nil ||
		server.ReadHeaderTimeout != serverReadHeaderTimeout ||
		server.ReadTimeout != serverReadTimeout ||
		server.WriteTimeout != serverWriteTimeout ||
		server.IdleTimeout != serverIdleTimeout ||
		server.MaxHeaderBytes != serverMaximumHeaderBytes ||
		server.ErrorLog == nil {
		t.Fatalf("HTTP server hardening drifted: %#v", server)
	}

	const unsafeDiagnostic = "private@example.test password=secret"
	server.ErrorLog.Print(unsafeDiagnostic)
	if strings.Contains(logs.String(), unsafeDiagnostic) ||
		!strings.Contains(logs.String(), "http_server_error") {
		t.Errorf("server error log was not redacted: %q", logs.String())
	}
}

// TestNewOperationalLoggerWritesJSON protects the machine-readable lifecycle
// contract consumed by deployment log collection and alerting.
func TestNewOperationalLoggerWritesJSON(t *testing.T) {
	var output bytes.Buffer
	logger := newOperationalLogger(&output)
	logger.Info("stage25_test", "status", "ok")

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("decode operational JSON log: %v", err)
	}
	if record["msg"] != "stage25_test" || record["status"] != "ok" ||
		record["level"] != "INFO" {
		t.Errorf("operational log record: %#v", record)
	}
}

// TestNewOperationalLegacyLoggerUsesErrorSeverity protects alerting for the
// fixed failure messages still emitted through the standard log package.
func TestNewOperationalLegacyLoggerUsesErrorSeverity(t *testing.T) {
	var output bytes.Buffer
	logger := newOperationalLogger(&output)
	newOperationalLegacyLogger(logger).Print("fixed_dependency_failure")

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("decode legacy operational JSON log: %v", err)
	}
	if record["msg"] != "fixed_dependency_failure" ||
		record["level"] != "ERROR" {
		t.Errorf("legacy operational log record: %#v", record)
	}
}

// TestRedactedHTTPServerErrorWriterToleratesMissingLogger verifies a defensive
// server path still satisfies log.Logger without exposing its input.
func TestRedactedHTTPServerErrorWriterToleratesMissingLogger(t *testing.T) {
	writer := &redactedHTTPServerErrorWriter{}
	message := []byte("attacker-controlled transport text")

	written, err := writer.Write(message)
	if err != nil || written != len(message) {
		t.Errorf("redacted writer: written=%d err=%v", written, err)
	}
}
