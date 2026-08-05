package main

import (
	"crypto/sha256"
	"embed"
	"encoding/binary"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// embeddedMigrationFiles contains the versioned SQL shipped with the
// application binary. Embedding keeps the migration catalog available even
// when the executable is started outside the repository checkout.
//
//go:embed migrations/*.sql
var embeddedMigrationFiles embed.FS

// migrationFilenamePattern defines the complete filename contract accepted by
// the catalog loader.
//
// A migration version is exactly six decimal digits. Its descriptive name uses
// one or more lowercase words separated by single underscores, and its
// direction is exactly "up" or "down" before the final .sql extension.
var migrationFilenamePattern = regexp.MustCompile(
	`^([0-9]{6})_([a-z]+(?:_[a-z]+)*)\.(up|down)\.sql$`,
)

// migrationDefinition is one complete, validated, forward-and-reverse schema
// change.
//
// SQL values have Unix line endings regardless of how their source filesystem
// represented newlines. Checksum therefore identifies the migration's logical
// content rather than the operating system on which it was loaded.
type migrationDefinition struct {
	// Version is the positive, contiguous sequence number beginning at one.
	Version int64
	// Name is the lowercase underscore description shared by both SQL files.
	Name string
	// UpSQL applies the schema change and always contains non-whitespace text.
	UpSQL string
	// DownSQL reverses the schema change and always contains non-whitespace text.
	DownSQL string
	// Checksum is the SHA-256 digest of the definition's canonical encoding.
	Checksum [sha256.Size]byte
}

// migrationFileDescriptor contains the trusted values parsed from one valid
// migration filename before its SQL is paired with the opposite direction.
type migrationFileDescriptor struct {
	// version is the numeric value represented by the six-digit prefix.
	version int64
	// name is the shared descriptive portion of the migration pair.
	name string
	// direction is either "up" or "down".
	direction string
}

// migrationPair accumulates the two files belonging to one version while the
// loader validates the complete directory.
type migrationPair struct {
	// name must remain identical across the version's up and down files.
	name string
	// upSQL contains normalized forward SQL after the up file is encountered.
	upSQL string
	// downSQL contains normalized reverse SQL after the down file is encountered.
	downSQL string
	// hasUp distinguishes an absent up file from a present file whose text was
	// rejected before catalog construction.
	hasUp bool
	// hasDown performs the same presence check for the reverse migration.
	hasDown bool
}

// loadEmbeddedMigrationCatalog loads and validates the SQL compiled into the
// application.
//
// Keeping this small wrapper separate from loadMigrationCatalog lets unit tests
// exercise the complete catalog behavior with fstest.MapFS instead of depending
// on production files.
func loadEmbeddedMigrationCatalog() ([]migrationDefinition, error) {
	return loadMigrationCatalog(
		embeddedMigrationFiles,
		"migrations",
	)
}

// loadMigrationCatalog reads one migration directory, validates every entry,
// pairs each version's up and down SQL, and returns definitions in version
// order.
//
// The loader rejects unknown files instead of silently ignoring a typo. It also
// requires an exact directional pair for every version and a contiguous
// sequence beginning at version 000001, ensuring every environment observes the
// same migration history.
func loadMigrationCatalog(
	fileSystem fs.FS,
	directory string,
) ([]migrationDefinition, error) {
	entries, err := fs.ReadDir(fileSystem, directory)
	if err != nil {
		return nil, fmt.Errorf(
			"read migration directory %q: %w",
			directory,
			err,
		)
	}

	// Pairs are indexed by numeric version while files are inspected. A map
	// makes duplicate versions visible even when their descriptive names differ.
	pairs := make(
		map[int64]*migrationPair,
		len(entries)/2,
	)

	for _, entry := range entries {
		// Migration directories must contain only regular files that satisfy the
		// filename contract. A nested directory or special file is an error rather
		// than an entry the loader might accidentally overlook.
		entryInformation, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf(
				"inspect migration entry %q: %w",
				entry.Name(),
				err,
			)
		}
		if !entryInformation.Mode().IsRegular() {
			return nil, fmt.Errorf(
				"migration entry %q is not a regular file",
				entry.Name(),
			)
		}

		descriptor, err := parseMigrationFilename(entry.Name())
		if err != nil {
			return nil, err
		}

		// Read through fs.FS so the same logic supports embedded production SQL
		// and the in-memory filesystems used by tests.
		migrationPath := path.Join(directory, entry.Name())
		rawSQL, err := fs.ReadFile(fileSystem, migrationPath)
		if err != nil {
			return nil, fmt.Errorf(
				"read migration file %q: %w",
				migrationPath,
				err,
			)
		}

		normalizedSQL := normalizeMigrationSQL(string(rawSQL))
		if strings.TrimSpace(normalizedSQL) == "" {
			return nil, fmt.Errorf(
				"migration file %q contains no SQL",
				migrationPath,
			)
		}
		if err := validateMigrationSQLSafety(normalizedSQL); err != nil {
			return nil, fmt.Errorf(
				"validate migration file %q: %w",
				migrationPath,
				err,
			)
		}

		pair, exists := pairs[descriptor.version]
		if !exists {
			// A newly encountered version records the name that its opposite
			// direction must use.
			pair = &migrationPair{name: descriptor.name}
			pairs[descriptor.version] = pair
		} else if pair.name != descriptor.name {
			// Two names with one version would make ordering ambiguous even if each
			// name supplied its own directional files.
			return nil, fmt.Errorf(
				"migration version %06d has duplicate names %q and %q",
				descriptor.version,
				pair.name,
				descriptor.name,
			)
		}

		// Store each direction once. An fs.FS normally cannot expose the same path
		// twice, but explicit flags keep this invariant local and protect callers
		// backed by custom filesystem implementations.
		switch descriptor.direction {
		case "up":
			if pair.hasUp {
				return nil, fmt.Errorf(
					"migration version %06d has duplicate up files",
					descriptor.version,
				)
			}
			pair.upSQL = normalizedSQL
			pair.hasUp = true
		case "down":
			if pair.hasDown {
				return nil, fmt.Errorf(
					"migration version %06d has duplicate down files",
					descriptor.version,
				)
			}
			pair.downSQL = normalizedSQL
			pair.hasDown = true
		}
	}

	// Sorting the discovered numeric versions makes catalog order independent of
	// the filesystem's directory enumeration behavior.
	versions := make([]int64, 0, len(pairs))
	for version := range pairs {
		versions = append(versions, version)
	}
	sort.Slice(
		versions,
		func(left int, right int) bool {
			return versions[left] < versions[right]
		},
	)

	catalog := make(
		[]migrationDefinition,
		0,
		len(versions),
	)
	// The ledger makes names globally unique so a descriptive identity cannot be
	// reused under a later version. Validate that contract before any SQL reaches
	// PostgreSQL rather than allowing the ledger insert to discover it midway.
	usedNames := make(map[string]int64, len(versions))
	for index, version := range versions {
		// The index establishes the only permitted next version. Starting the
		// comparison at one rejects 000000 as well as catalogs that begin later.
		expectedVersion := int64(index + 1)
		if version != expectedVersion {
			return nil, fmt.Errorf(
				"migration version gap: got %06d, want %06d",
				version,
				expectedVersion,
			)
		}

		pair := pairs[version]
		if !pair.hasUp || !pair.hasDown {
			missingDirection := "up"
			if pair.hasUp {
				missingDirection = "down"
			}

			return nil, fmt.Errorf(
				"migration version %06d is missing its %s file",
				version,
				missingDirection,
			)
		}
		if previousVersion, exists := usedNames[pair.name]; exists {
			return nil, fmt.Errorf(
				"migration name %q is reused by versions %06d and %06d",
				pair.name,
				previousVersion,
				version,
			)
		}
		usedNames[pair.name] = version

		definition := migrationDefinition{
			Version: version,
			Name:    pair.name,
			UpSQL:   pair.upSQL,
			DownSQL: pair.downSQL,
		}
		definition.Checksum = migrationChecksum(
			definition.Version,
			definition.Name,
			definition.UpSQL,
			definition.DownSQL,
		)

		catalog = append(catalog, definition)
	}

	return catalog, nil
}

// parseMigrationFilename validates one base filename and returns its structured
// version, name, and direction.
func parseMigrationFilename(
	filename string,
) (migrationFileDescriptor, error) {
	matches := migrationFilenamePattern.FindStringSubmatch(filename)
	if matches == nil {
		return migrationFileDescriptor{}, fmt.Errorf(
			"migration filename %q must match 000001_lowercase_name.(up|down).sql",
			filename,
		)
	}

	// The regular expression guarantees six ASCII digits. ParseInt still returns
	// an error value, so wrap it rather than assuming conversion cannot fail.
	version, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return migrationFileDescriptor{}, fmt.Errorf(
			"parse migration version in %q: %w",
			filename,
			err,
		)
	}

	return migrationFileDescriptor{
		version:   version,
		name:      matches[2],
		direction: matches[3],
	}, nil
}

// normalizeMigrationSQL converts Windows CRLF and legacy lone-CR line endings
// to a single LF representation.
//
// Replacing CRLF first prevents its CR and LF bytes from becoming two line
// breaks when the remaining lone CR characters are normalized.
func normalizeMigrationSQL(sqlText string) string {
	normalized := strings.ReplaceAll(sqlText, "\r\n", "\n")
	return strings.ReplaceAll(normalized, "\r", "\n")
}

// migrationChecksum returns the SHA-256 digest of one migration's canonical
// representation.
//
// The version uses a fixed-width big-endian integer, while every string receives
// an eight-byte length prefix. This encoding cannot confuse field boundaries
// even when SQL or names contain separator-like text. SQL is normalized again
// defensively so callers outside loadMigrationCatalog receive the same
// cross-platform checksum behavior.
func migrationChecksum(
	version int64,
	name string,
	upSQL string,
	downSQL string,
) [sha256.Size]byte {
	// A format label permits a future canonical-encoding revision without
	// accidentally treating its digests as compatible with this version.
	canonical := appendMigrationChecksumField(
		nil,
		"zafarmand-migration-checksum-v1",
	)

	var versionBytes [8]byte
	binary.BigEndian.PutUint64(
		versionBytes[:],
		uint64(version),
	)
	canonical = append(canonical, versionBytes[:]...)

	canonical = appendMigrationChecksumField(canonical, name)
	canonical = appendMigrationChecksumField(
		canonical,
		normalizeMigrationSQL(upSQL),
	)
	canonical = appendMigrationChecksumField(
		canonical,
		normalizeMigrationSQL(downSQL),
	)

	return sha256.Sum256(canonical)
}

// appendMigrationChecksumField appends one length-prefixed UTF-8 string to a
// canonical checksum byte sequence.
func appendMigrationChecksumField(
	destination []byte,
	value string,
) []byte {
	var lengthBytes [8]byte
	binary.BigEndian.PutUint64(
		lengthBytes[:],
		uint64(len(value)),
	)

	destination = append(destination, lengthBytes[:]...)
	return append(destination, value...)
}
