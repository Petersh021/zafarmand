// This module path is the stable import identity for the Zafarmand application.
module github.com/petersh021/zafarmand

// The Go directive records the minimum Go version and language semantics
// required when building and testing the module.
go 1.24.3

// pgx supplies PostgreSQL parsing, connections, and the database/sql adapter
// used by migrations, Contact persistence, and administrator authentication.
require github.com/jackc/pgx/v5 v5.8.0

// These modules are pgx's transitive implementation dependencies. Go records
// them explicitly so builds resolve the same complete module graph.
require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/text v0.29.0 // indirect
)
