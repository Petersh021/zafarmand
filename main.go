// Package main assembles the Zafarmand web application and its explicit
// PostgreSQL migration and administrator-bootstrap commands.
//
// The executable is intentionally small: routing, request handling, and
// template work live in separate files so each concern can be learned and
// changed independently.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"
)

const (
	// serverAddress is the local development address used by the public site.
	serverAddress = ":8080"
	// serverReadHeaderTimeout limits slow or incomplete request headers before a
	// handler receives control.
	serverReadHeaderTimeout = 5 * time.Second
	// serverReadTimeout bounds the complete request, including a slowly streamed
	// Contact body that has not yet reached its separate byte limit.
	serverReadTimeout = 15 * time.Second
	// serverIdleTimeout bounds how long an unused keep-alive connection remains.
	serverIdleTimeout = 60 * time.Second
	// serverShutdownTimeout gives active handlers a bounded opportunity to finish
	// after Ctrl+C before the process returns.
	serverShutdownTimeout = 5 * time.Second
)

// main is the application's composition root and process entry point.
//
// It validates process arguments before selecting one of three isolated paths:
// the ordinary HTTP server, an operator-requested migration action, or the
// environment-protected administrator bootstrap. Fatal logging stays at this
// outer boundary so lower-level functions can return testable errors without
// deciding how the process exits.
func main() {
	command, err := parseProgramCommand(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	// Every operating mode receives one interrupt-aware process context. Database
	// commands can cancel promptly, while the server uses the same signal to stop
	// accepting requests and close its shared database pool in order.
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
	)
	defer stop()

	if command.Name == programCommandMigrate {
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
	if command.Name == programCommandAdmin {
		if err := executeAdminCreateUserCommand(
			ctx,
			command.AdminCreateUser,
			os.LookupEnv,
			os.Stdout,
		); err != nil {
			log.Fatal(err)
		}

		return
	}

	if err := runServer(ctx, os.LookupEnv); err != nil {
		log.Fatal(err)
	}
}

// runServer composes the long-lived PostgreSQL pool, all three public catalogue
// readers, their protected read/write boundaries, Contact and inquiry services,
// administrator authentication, templates, routes, and interrupt-aware server.
//
// Opening and pinging PostgreSQL before ListenAndServe prevents the site from
// starting when its configured persistence dependency is unreachable. Schema
// versions remain an explicit migration responsibility; an outdated schema is
// reported through safe Product, Interior, Contact, or administrator
// service-failure responses until the operator applies migrations. This
// function owns the pool and closes it only after the server stops using every
// repository.
func runServer(
	ctx context.Context,
	lookup environmentLookup,
) error {
	databaseConfig, err := loadDatabaseConfig(lookup)
	if err != nil {
		return err
	}

	database, err := openPostgresDatabase(ctx, databaseConfig)
	if err != nil {
		return err
	}
	defer func() {
		// The server has already stopped before this deferred close runs. There is
		// no useful recovery action at process shutdown, and the database/sql Close
		// error contains no result needed by an HTTP response.
		_ = database.Close()
	}()

	products, err := newPostgresProductCatalogueReader(database)
	if err != nil {
		return err
	}
	interiorProjects, err := newPostgresInteriorProjectCatalogueReader(database)
	if err != nil {
		return err
	}
	architectureProjects, err := newPostgresArchitectureProjectCatalogueReader(
		database,
	)
	if err != nil {
		return err
	}
	// Site content remains an independent read boundary even though it borrows
	// the same process-owned pool. It supplies only managed Homepage, Contact,
	// featured-card, SEO, and exact current hero projections to public handlers.
	siteContent, err := newPostgresSiteContentReader(database)
	if err != nil {
		return err
	}
	inquiries, err := newPostgresInquiryRepository(database)
	if err != nil {
		return err
	}
	admins, err := newPostgresAdminRepository(database)
	if err != nil {
		return err
	}
	adminProducts, err := newPostgresAdminProductReader(database)
	if err != nil {
		return err
	}
	adminProductWrites, err := newPostgresAdminProductWriter(database)
	if err != nil {
		return err
	}
	adminInteriorProjects, err := newPostgresAdminInteriorProjectReader(database)
	if err != nil {
		return err
	}
	adminInteriorProjectWrites, err := newPostgresAdminInteriorProjectWriter(database)
	if err != nil {
		return err
	}
	adminArchitectureProjects, err := newPostgresAdminArchitectureProjectReader(
		database,
	)
	if err != nil {
		return err
	}
	adminArchitectureProjectWrites, err := newPostgresAdminArchitectureProjectWriter(
		database,
	)
	if err != nil {
		return err
	}
	// The protected reader and writer share the process-owned pool while keeping
	// Site-content read and mutation authority separate at the application edge.
	adminSiteContent, err := newPostgresAdminSiteContentReader(database)
	if err != nil {
		return err
	}
	adminSiteContentWrites, err := newPostgresAdminSiteContentWriter(database)
	if err != nil {
		return err
	}
	adminInquiries, err := newPostgresAdminInquiryReader(database)
	if err != nil {
		return err
	}
	adminInquiryStatuses, err := newPostgresAdminInquiryStatusUpdater(database)
	if err != nil {
		return err
	}

	app, err := newApplication(
		products,
		interiorProjects,
		architectureProjects,
		siteContent,
		inquiries,
		admins,
		adminProducts,
		adminProductWrites,
		adminInteriorProjects,
		adminInteriorProjectWrites,
		adminArchitectureProjects,
		adminArchitectureProjectWrites,
		adminSiteContent,
		adminSiteContentWrites,
		adminInquiries,
		adminInquiryStatuses,
		newAdminPasswordManager(),
	)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              serverAddress,
		Handler:           app.routes(),
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		IdleTimeout:       serverIdleTimeout,
	}

	log.Printf(
		"Zafarmand website is running at http://localhost%s",
		serverAddress,
	)

	// A buffered channel lets ListenAndServe report an immediate bind failure
	// even if the process context is canceled at the same moment.
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		// Shutdown receives a fresh context because the process context has already
		// been canceled. It stops new connections and waits for active handlers up
		// to the explicit deadline.
		shutdownContext, cancel := context.WithTimeout(
			context.Background(),
			serverShutdownTimeout,
		)
		defer cancel()

		if err := server.Shutdown(shutdownContext); err != nil {
			// A deadline means active handlers did not finish in the graceful window.
			// Close forces their connections down before the deferred database close,
			// preserving the rule that no handler can outlive its shared pool.
			_ = server.Close()
			return fmt.Errorf("shut down HTTP server: %w", err)
		}

		if err := <-serverErrors; err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP during shutdown: %w", err)
		}

		return nil
	}
}
