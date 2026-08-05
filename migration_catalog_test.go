package main

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// migrationTestFile creates one regular in-memory SQL file for catalog tests.
func migrationTestFile(sqlText string) *fstest.MapFile {
	return &fstest.MapFile{
		Data: []byte(sqlText),
		Mode: 0o644,
	}
}

// migrationTestFileSystem creates one complete version 000001 pair for tests
// that need to vary SQL while preserving the filename contract.
func migrationTestFileSystem(
	upSQL string,
	downSQL string,
) fstest.MapFS {
	return fstest.MapFS{
		"migrations/000001_create_inquiries.up.sql":   migrationTestFile(upSQL),
		"migrations/000001_create_inquiries.down.sql": migrationTestFile(downSQL),
	}
}

// loadSingleTestMigration loads one complete version 000001 pair and returns
// its sole definition.
func loadSingleTestMigration(
	t *testing.T,
	upSQL string,
	downSQL string,
) migrationDefinition {
	t.Helper()

	fileSystem := migrationTestFileSystem(upSQL, downSQL)

	catalog, err := loadMigrationCatalog(fileSystem, "migrations")
	if err != nil {
		t.Fatalf("load single migration: %v", err)
	}
	if len(catalog) != 1 {
		t.Fatalf("catalog length: got %d, want 1", len(catalog))
	}

	return catalog[0]
}

// TestLoadMigrationCatalog verifies pairing, numeric ordering, line-ending
// normalization, and checksum construction for a valid catalog.
func TestLoadMigrationCatalog(t *testing.T) {
	// Version two is inserted before version one so the result cannot inherit
	// the map's insertion order accidentally.
	fileSystem := fstest.MapFS{
		"migrations/000002_add_index.up.sql": migrationTestFile(
			"CREATE INDEX example;\r\n",
		),
		"migrations/000001_create_inquiries.down.sql": migrationTestFile(
			"DROP TABLE example;\r",
		),
		"migrations/000002_add_index.down.sql": migrationTestFile(
			"DROP INDEX example;\r\n",
		),
		"migrations/000001_create_inquiries.up.sql": migrationTestFile(
			"CREATE TABLE example;\r\n",
		),
	}

	catalog, err := loadMigrationCatalog(fileSystem, "migrations")
	if err != nil {
		t.Fatalf("load migration catalog: %v", err)
	}
	if len(catalog) != 2 {
		t.Fatalf("catalog length: got %d, want 2", len(catalog))
	}

	// Exact fields prove that opposite directions were paired and both CRLF and
	// lone-CR input became LF before checksum calculation.
	expected := []migrationDefinition{
		{
			Version: 1,
			Name:    "create_inquiries",
			UpSQL:   "CREATE TABLE example;\n",
			DownSQL: "DROP TABLE example;\n",
		},
		{
			Version: 2,
			Name:    "add_index",
			UpSQL:   "CREATE INDEX example;\n",
			DownSQL: "DROP INDEX example;\n",
		},
	}

	for index, want := range expected {
		got := catalog[index]
		if got.Version != want.Version ||
			got.Name != want.Name ||
			got.UpSQL != want.UpSQL ||
			got.DownSQL != want.DownSQL {
			t.Errorf(
				"migration %d fields: got %#v, want %#v",
				index,
				got,
				want,
			)
		}

		wantChecksum := migrationChecksum(
			got.Version,
			got.Name,
			got.UpSQL,
			got.DownSQL,
		)
		if got.Checksum != wantChecksum {
			t.Errorf("migration %d checksum does not cover its fields", index)
		}
	}
}

// TestMigrationCatalogChecksumIgnoresLineEndingStyle verifies that equivalent
// LF, CRLF, and lone-CR SQL has identical normalized text and digest.
func TestMigrationCatalogChecksumIgnoresLineEndingStyle(t *testing.T) {
	lfDefinition := loadSingleTestMigration(
		t,
		"SELECT 1;\nSELECT 2;\nSELECT 3;\n",
		"SELECT 4;\nSELECT 5;\n",
	)
	mixedDefinition := loadSingleTestMigration(
		t,
		"SELECT 1;\r\nSELECT 2;\rSELECT 3;\r\n",
		"SELECT 4;\rSELECT 5;\r\n",
	)

	if mixedDefinition.UpSQL != lfDefinition.UpSQL {
		t.Errorf(
			"normalized up SQL: got %q, want %q",
			mixedDefinition.UpSQL,
			lfDefinition.UpSQL,
		)
	}
	if mixedDefinition.DownSQL != lfDefinition.DownSQL {
		t.Errorf(
			"normalized down SQL: got %q, want %q",
			mixedDefinition.DownSQL,
			lfDefinition.DownSQL,
		)
	}
	if mixedDefinition.Checksum != lfDefinition.Checksum {
		t.Error("equivalent line endings produced different checksums")
	}
}

// TestMigrationChecksumCoversEveryDefinitionField verifies that a change to
// either SQL direction, as well as the identity fields, changes the digest.
func TestMigrationChecksumCoversEveryDefinitionField(t *testing.T) {
	baseline := migrationChecksum(
		1,
		"create_inquiries",
		"CREATE TABLE example;\n",
		"DROP TABLE example;\n",
	)

	tests := []struct {
		// name identifies the field varied by this case.
		name string
		// checksum is calculated after changing only the named field.
		checksum [32]byte
	}{
		{
			name: "version",
			checksum: migrationChecksum(
				2,
				"create_inquiries",
				"CREATE TABLE example;\n",
				"DROP TABLE example;\n",
			),
		},
		{
			name: "name",
			checksum: migrationChecksum(
				1,
				"create_projects",
				"CREATE TABLE example;\n",
				"DROP TABLE example;\n",
			),
		},
		{
			name: "up SQL",
			checksum: migrationChecksum(
				1,
				"create_inquiries",
				"CREATE TABLE changed;\n",
				"DROP TABLE example;\n",
			),
		},
		{
			name: "down SQL",
			checksum: migrationChecksum(
				1,
				"create_inquiries",
				"CREATE TABLE example;\n",
				"DROP TABLE changed;\n",
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.checksum == baseline {
				t.Errorf("changing %s did not change checksum", test.name)
			}
		})
	}
}

// TestLoadMigrationCatalogRejectsMalformedFilenames verifies the exact
// six-digit, lowercase underscore name, direction, and extension contract.
func TestLoadMigrationCatalogRejectsMalformedFilenames(t *testing.T) {
	malformedFilenames := []string{
		"00001_create_inquiries.up.sql",
		"000001_Create_inquiries.up.sql",
		"000001_create-Inquiries.up.sql",
		"000001_create__inquiries.up.sql",
		"000001_create_inquiries.forward.sql",
		"000001_create_inquiries.up.txt",
	}

	for _, filename := range malformedFilenames {
		t.Run(filename, func(t *testing.T) {
			// A valid pair accompanies the malformed extra entry so an unrelated
			// missing-pair error cannot satisfy this case.
			fileSystem := fstest.MapFS{
				"migrations/000001_create_inquiries.up.sql": migrationTestFile(
					"SELECT 1;",
				),
				"migrations/000001_create_inquiries.down.sql": migrationTestFile(
					"SELECT 2;",
				),
				"migrations/" + filename: migrationTestFile("SELECT 3;"),
			}

			if _, err := loadMigrationCatalog(
				fileSystem,
				"migrations",
			); err == nil {
				t.Fatalf("malformed filename %q was accepted", filename)
			}
		})
	}
}

// TestLoadMigrationCatalogRejectsMissingPairs verifies that neither migration
// direction can exist without its exact counterpart.
func TestLoadMigrationCatalogRejectsMissingPairs(t *testing.T) {
	tests := []struct {
		// name labels the absent direction.
		name string
		// filename is the only migration direction present.
		filename string
	}{
		{
			name:     "missing down",
			filename: "migrations/000001_create_inquiries.up.sql",
		},
		{
			name:     "missing up",
			filename: "migrations/000001_create_inquiries.down.sql",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fileSystem := fstest.MapFS{
				test.filename: migrationTestFile("SELECT 1;"),
			}

			if _, err := loadMigrationCatalog(
				fileSystem,
				"migrations",
			); err == nil {
				t.Fatal("unpaired migration file was accepted")
			}
		})
	}
}

// TestLoadMigrationCatalogRejectsDuplicateVersions verifies that two
// descriptive names cannot claim the same version even when each has a pair.
func TestLoadMigrationCatalogRejectsDuplicateVersions(t *testing.T) {
	fileSystem := fstest.MapFS{
		"migrations/000001_create_inquiries.up.sql": migrationTestFile(
			"SELECT 1;",
		),
		"migrations/000001_create_inquiries.down.sql": migrationTestFile(
			"SELECT 2;",
		),
		"migrations/000001_create_projects.up.sql": migrationTestFile(
			"SELECT 3;",
		),
		"migrations/000001_create_projects.down.sql": migrationTestFile(
			"SELECT 4;",
		),
	}

	if _, err := loadMigrationCatalog(
		fileSystem,
		"migrations",
	); err == nil {
		t.Fatal("duplicate migration version was accepted")
	}
}

// TestLoadMigrationCatalogRejectsDuplicateNames verifies that a descriptive
// identity cannot be reused by two otherwise valid migration versions.
func TestLoadMigrationCatalogRejectsDuplicateNames(t *testing.T) {
	fileSystem := fstest.MapFS{
		"migrations/000001_create_inquiries.up.sql": migrationTestFile(
			"SELECT 1;",
		),
		"migrations/000001_create_inquiries.down.sql": migrationTestFile(
			"SELECT 2;",
		),
		"migrations/000002_create_inquiries.up.sql": migrationTestFile(
			"SELECT 3;",
		),
		"migrations/000002_create_inquiries.down.sql": migrationTestFile(
			"SELECT 4;",
		),
	}

	if _, err := loadMigrationCatalog(
		fileSystem,
		"migrations",
	); err == nil {
		t.Fatal("duplicate migration name was accepted")
	}
}

// TestLoadMigrationCatalogRejectsVersionGaps verifies that the sequence starts
// at 000001 and advances without a skipped numeric version.
func TestLoadMigrationCatalogRejectsVersionGaps(t *testing.T) {
	tests := []struct {
		// name describes the sequence error.
		name string
		// files contains complete pairs so only version validation can fail.
		files fstest.MapFS
	}{
		{
			name: "starts after one",
			files: fstest.MapFS{
				"migrations/000002_second.up.sql":   migrationTestFile("SELECT 1;"),
				"migrations/000002_second.down.sql": migrationTestFile("SELECT 2;"),
			},
		},
		{
			name: "internal gap",
			files: fstest.MapFS{
				"migrations/000001_first.up.sql":   migrationTestFile("SELECT 1;"),
				"migrations/000001_first.down.sql": migrationTestFile("SELECT 2;"),
				"migrations/000003_third.up.sql":   migrationTestFile("SELECT 3;"),
				"migrations/000003_third.down.sql": migrationTestFile("SELECT 4;"),
			},
		},
		{
			name: "zero version",
			files: fstest.MapFS{
				"migrations/000000_zero.up.sql":   migrationTestFile("SELECT 1;"),
				"migrations/000000_zero.down.sql": migrationTestFile("SELECT 2;"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := loadMigrationCatalog(
				test.files,
				"migrations",
			); err == nil {
				t.Fatal("migration version gap was accepted")
			}
		})
	}
}

// TestLoadMigrationCatalogRejectsEmptySQL verifies that an empty or
// whitespace-only file cannot become an executable migration definition.
func TestLoadMigrationCatalogRejectsEmptySQL(t *testing.T) {
	tests := []struct {
		// name identifies the empty direction.
		name string
		// upSQL is the forward file's exact contents.
		upSQL string
		// downSQL is the reverse file's exact contents.
		downSQL string
	}{
		{
			name:    "empty up",
			upSQL:   "",
			downSQL: "SELECT 1;",
		},
		{
			name:    "whitespace down",
			upSQL:   "SELECT 1;",
			downSQL: " \t\r\n ",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fileSystem := fstest.MapFS{
				"migrations/000001_create_inquiries.up.sql": migrationTestFile(
					test.upSQL,
				),
				"migrations/000001_create_inquiries.down.sql": migrationTestFile(
					test.downSQL,
				),
			}

			if _, err := loadMigrationCatalog(
				fileSystem,
				"migrations",
			); err == nil {
				t.Fatal("empty migration SQL was accepted")
			}
		})
	}
}

// TestLoadMigrationCatalogRejectsNonRegularEntries verifies that a nested
// directory cannot be silently ignored inside the strict migration directory.
func TestLoadMigrationCatalogRejectsNonRegularEntries(t *testing.T) {
	fileSystem := fstest.MapFS{
		"migrations/000001_create_inquiries.up.sql": migrationTestFile(
			"SELECT 1;",
		),
		"migrations/000001_create_inquiries.down.sql": migrationTestFile(
			"SELECT 2;",
		),
		"migrations/nested": {
			Mode: fs.ModeDir | 0o755,
		},
	}

	if _, err := loadMigrationCatalog(
		fileSystem,
		"migrations",
	); err == nil {
		t.Fatal("nested migration directory was accepted")
	}
}

// TestEmbeddedMigrationCatalog verifies the production catalog contains only
// the initial inquiry table and its exact reverse statement.
func TestEmbeddedMigrationCatalog(t *testing.T) {
	catalog, err := loadEmbeddedMigrationCatalog()
	if err != nil {
		t.Fatalf("load embedded migration catalog: %v", err)
	}
	if len(catalog) != 1 {
		t.Fatalf("embedded catalog length: got %d, want 1", len(catalog))
	}

	definition := catalog[0]
	if definition.Version != 1 || definition.Name != "create_inquiries" {
		t.Errorf(
			"embedded migration identity: got %06d_%s",
			definition.Version,
			definition.Name,
		)
	}
	expectedDownSQL := `-- Reverse only the exact table introduced by version 000001. Its strict form
-- makes unexpected schema drift fail visibly instead of removing dependencies.
DROP TABLE public.inquiries;
`
	if definition.DownSQL != expectedDownSQL {
		t.Errorf(
			"embedded down SQL: got %q, want exact DROP statement",
			definition.DownSQL,
		)
	}
	upperDownSQL := strings.ToUpper(definition.DownSQL)
	if strings.Contains(upperDownSQL, "CASCADE") ||
		strings.Contains(upperDownSQL, "IF EXISTS") {
		t.Error("embedded down SQL contains a forbidden DROP modifier")
	}

	// These fragments lock the migration to known Contact limits, trusted
	// choices, lifecycle states, and ordered timestamps without parsing SQL.
	expectedUpSQL := []string{
		"CREATE TABLE public.inquiries",
		"id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY",
		"CHECK (name = btrim(name))",
		"CHECK (char_length(name) BETWEEN 1 AND 100)",
		"CHECK (email = btrim(email))",
		"CHECK (char_length(email) BETWEEN 3 AND 254)",
		"'interior-design'",
		"'architecture-design'",
		"'products'",
		"CHECK (message = btrim(message))",
		"CHECK (char_length(message) BETWEEN 1 AND 3000)",
		"CHECK (status IN ('new', 'reviewed', 'archived'))",
		"created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"CHECK (updated_at >= created_at)",
	}

	for _, expectedSQL := range expectedUpSQL {
		if !strings.Contains(definition.UpSQL, expectedSQL) {
			t.Errorf("embedded up SQL does not contain %q", expectedSQL)
		}
	}
}
