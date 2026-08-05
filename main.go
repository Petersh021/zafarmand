// Package main assembles the Zafarmand web application and its explicit
// PostgreSQL migration command.
//
// The executable is intentionally small: routing, request handling, and
// template work live in separate files so each concern can be learned and
// changed independently.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
)

// main is the application's composition root and process entry point.
//
// It validates process arguments before selecting one of two isolated paths:
// the ordinary HTTP server or an operator-requested migration action. Fatal
// logging stays at this outer boundary so lower-level functions can return
// testable errors without deciding how the process exits.
func main() {
	command, err := parseProgramCommand(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	// Migration commands receive an interrupt-aware context so Ctrl+C can cancel
	// lock acquisition or SQL without starting the public HTTP server.
	if command.Name == programCommandMigrate {
		ctx, stop := signal.NotifyContext(
			context.Background(),
			os.Interrupt,
		)
		defer stop()

		if err := executeMigrationCommand(
			ctx,
			command,
			os.LookupEnv,
			os.Stdout,
		); err != nil {
			log.Fatal(err)
		}

		return
	}

	if err := runServer(); err != nil {
		log.Fatal(err)
	}
}

// runServer preserves the existing database-independent public application
// startup path.
//
// Stage 13 owns schema tooling only. Stage 14 will introduce the first runtime
// repository and decide how a long-lived pool participates in server shutdown.
func runServer() error {
	app, err := newApplication()
	if err != nil {
		return err
	}

	// Keeping the address in one named constant makes the server configuration
	// easy to find while development still uses a fixed local port.
	const address = ":8080"

	log.Printf(
		"Zafarmand website is running at http://localhost%s",
		address,
	)

	// ListenAndServe blocks while the process is running. app.routes() supplies
	// the http.Handler that decides which code handles each incoming request.
	return http.ListenAndServe(address, app.routes())
}
