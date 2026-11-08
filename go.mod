// This module path is the stable import identity for the Zafarmand application.
module github.com/petersh021/zafarmand

// The Go directive records the minimum Go version and language semantics
// required when building and testing the module.
go 1.26.7

// pgx supplies PostgreSQL persistence. The OIDC and OAuth2 libraries perform
// provider discovery, signed ID-token verification, and PKCE code exchange for
// Google administrator authentication.
require (
	github.com/coreos/go-oidc/v3 v3.21.0
	github.com/jackc/pgx/v5 v5.10.0
	golang.org/x/oauth2 v0.37.0
)

// These transitive implementation dependencies are recorded explicitly so
// builds resolve the same complete module graph.
require (
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/text v0.39.0 // indirect
)
