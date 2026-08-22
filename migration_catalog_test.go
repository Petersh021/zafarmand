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

// TestEmbeddedMigrationCatalog verifies the production catalog contains the
// inquiry table, its idempotency key, the admin-access boundary, the Product,
// Interior, and Architecture content boundaries, and the independent homepage,
// hero, and Contact content boundary with exact ordered identities and
// independently reversible schema changes.
func TestEmbeddedMigrationCatalog(t *testing.T) {
	catalog, err := loadEmbeddedMigrationCatalog()
	if err != nil {
		t.Fatalf("load embedded migration catalog: %v", err)
	}
	if len(catalog) != 9 {
		t.Fatalf("embedded catalog length: got %d, want 9", len(catalog))
	}

	initialDefinition := catalog[0]
	if initialDefinition.Version != 1 ||
		initialDefinition.Name != "create_inquiries" {
		t.Errorf(
			"embedded migration identity: got %06d_%s",
			initialDefinition.Version,
			initialDefinition.Name,
		)
	}
	expectedDownSQL := `-- Reverse only the exact table introduced by version 000001. Its strict form
-- makes unexpected schema drift fail visibly instead of removing dependencies.
DROP TABLE public.inquiries;
`
	if initialDefinition.DownSQL != expectedDownSQL {
		t.Errorf(
			"embedded down SQL: got %q, want exact DROP statement",
			initialDefinition.DownSQL,
		)
	}
	upperDownSQL := strings.ToUpper(initialDefinition.DownSQL)
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
		if !strings.Contains(initialDefinition.UpSQL, expectedSQL) {
			t.Errorf("embedded up SQL does not contain %q", expectedSQL)
		}
	}

	keyDefinition := catalog[1]
	if keyDefinition.Version != 2 ||
		keyDefinition.Name != "add_inquiry_submission_key" {
		t.Errorf(
			"embedded migration identity: got %06d_%s",
			keyDefinition.Version,
			keyDefinition.Name,
		)
	}

	// These ordered fragments prove version 000002 first creates a nullable
	// compatibility boundary, then backfills legacy rows, and only afterward
	// enforces the fixed-width, required, and unique storage contract.
	orderedUpSQL := []string{
		"ADD COLUMN submission_key bytea;",
		"UPDATE public.inquiries",
		"decode(",
		"md5(",
		"WHERE submission_key IS NULL;",
		"CONSTRAINT inquiries_submission_key_length",
		"CHECK (octet_length(submission_key) = 32)",
		"ALTER COLUMN submission_key SET NOT NULL;",
		"CONSTRAINT inquiries_submission_key_unique",
		"UNIQUE (submission_key);",
	}
	previousPosition := -1
	for _, expectedSQL := range orderedUpSQL {
		position := strings.Index(keyDefinition.UpSQL, expectedSQL)
		if position < 0 {
			t.Errorf("idempotency up SQL does not contain %q", expectedSQL)
			continue
		}
		if position <= previousPosition {
			t.Errorf("idempotency up SQL has %q out of order", expectedSQL)
		}
		previousPosition = position
	}

	// Exactly two decoded MD5 digests yield 32 bytes while keeping the initial
	// backfill independent from optional PostgreSQL extensions.
	if count := strings.Count(keyDefinition.UpSQL, "decode("); count != 2 {
		t.Errorf("idempotency digest decode count: got %d, want 2", count)
	}
	upperUpSQL := strings.ToUpper(keyDefinition.UpSQL)
	for _, forbiddenSQL := range []string{
		"CREATE EXTENSION",
		"GEN_RANDOM_BYTES",
	} {
		if strings.Contains(upperUpSQL, forbiddenSQL) {
			t.Errorf("idempotency up SQL requires forbidden %q", forbiddenSQL)
		}
	}

	// The reverse migration removes both named constraints before their column
	// and deliberately leaves the inquiry table itself untouched.
	orderedDownSQL := []string{
		"DROP CONSTRAINT inquiries_submission_key_unique",
		"DROP CONSTRAINT inquiries_submission_key_length",
		"DROP COLUMN submission_key;",
	}
	previousPosition = -1
	for _, expectedSQL := range orderedDownSQL {
		position := strings.Index(keyDefinition.DownSQL, expectedSQL)
		if position < 0 {
			t.Errorf("idempotency down SQL does not contain %q", expectedSQL)
			continue
		}
		if position <= previousPosition {
			t.Errorf("idempotency down SQL has %q out of order", expectedSQL)
		}
		previousPosition = position
	}
	upperKeyDownSQL := strings.ToUpper(keyDefinition.DownSQL)
	for _, forbiddenSQL := range []string{
		"DROP TABLE",
		"CASCADE",
		"IF EXISTS",
	} {
		if strings.Contains(upperKeyDownSQL, forbiddenSQL) {
			t.Errorf("idempotency down SQL contains forbidden %q", forbiddenSQL)
		}
	}

	adminDefinition := catalog[2]
	if adminDefinition.Version != 3 ||
		adminDefinition.Name != "create_admin_access" {
		t.Errorf(
			"embedded migration identity: got %06d_%s",
			adminDefinition.Version,
			adminDefinition.Name,
		)
	}

	// These fragments lock version 000003 to normalized, unique administrators
	// and hash-only, expiring, revocable sessions. Named constraints give future
	// repository and integration tests stable schema diagnostics.
	expectedAdminUpSQL := []string{
		"CREATE TABLE public.admin_users",
		"id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY",
		"email text NOT NULL",
		"password_hash text NOT NULL",
		"role text NOT NULL",
		"CONSTRAINT admin_users_email_normalized",
		"CHECK (email = lower(btrim(email)))",
		"CONSTRAINT admin_users_email_length",
		"CHECK (char_length(email) BETWEEN 3 AND 254)",
		"CONSTRAINT admin_users_email_unique",
		"UNIQUE (email)",
		"CONSTRAINT admin_users_password_hash_trimmed",
		"CHECK (password_hash = btrim(password_hash))",
		"CONSTRAINT admin_users_password_hash_length",
		"CHECK (char_length(password_hash) BETWEEN 1 AND 255)",
		"CONSTRAINT admin_users_role_supported",
		"CHECK (role IN ('owner', 'editor'))",
		"active boolean NOT NULL DEFAULT true",
		"created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"CONSTRAINT admin_users_timestamp_order",
		"CHECK (updated_at >= created_at)",
		"CREATE TABLE public.admin_sessions",
		"token_hash bytea PRIMARY KEY",
		"user_id bigint NOT NULL",
		"csrf_token_hash bytea NOT NULL",
		"expires_at timestamptz NOT NULL",
		"revoked_at timestamptz",
		"CONSTRAINT admin_sessions_user_id_foreign",
		"REFERENCES public.admin_users (id)",
		"ON DELETE CASCADE",
		"CONSTRAINT admin_sessions_token_hash_length",
		"CHECK (octet_length(token_hash) = 32)",
		"CONSTRAINT admin_sessions_csrf_token_hash_length",
		"CHECK (octet_length(csrf_token_hash) = 32)",
		"CONSTRAINT admin_sessions_expiry_order",
		"CHECK (expires_at > created_at)",
		"CONSTRAINT admin_sessions_revocation_order",
		"CHECK (revoked_at IS NULL OR revoked_at >= created_at)",
		"CREATE INDEX admin_sessions_user_id_idx",
		"ON public.admin_sessions (user_id)",
		"CREATE INDEX admin_sessions_expires_at_idx",
		"ON public.admin_sessions (expires_at)",
	}
	for _, expectedSQL := range expectedAdminUpSQL {
		if !strings.Contains(adminDefinition.UpSQL, expectedSQL) {
			t.Errorf("admin-access up SQL does not contain %q", expectedSQL)
		}
	}

	// The only permitted cascade is the narrowly scoped user-to-session foreign
	// key. Raw browser secrets and extension dependencies do not belong here.
	upperAdminUpSQL := strings.ToUpper(adminDefinition.UpSQL)
	if count := strings.Count(upperAdminUpSQL, "ON DELETE CASCADE"); count != 1 {
		t.Errorf("admin-access ON DELETE CASCADE count: got %d, want 1", count)
	}
	for _, forbiddenSQL := range []string{
		"CREATE EXTENSION",
		"TOKEN TEXT",
		"CSRF_TOKEN TEXT",
	} {
		if strings.Contains(upperAdminUpSQL, forbiddenSQL) {
			t.Errorf("admin-access up SQL contains forbidden %q", forbiddenSQL)
		}
	}

	// The down migration drops the dependent sessions table before users and
	// omits forgiving modifiers so unexpected later dependencies fail visibly.
	orderedAdminDownSQL := []string{
		"DROP TABLE public.admin_sessions;",
		"DROP TABLE public.admin_users;",
	}
	previousPosition = -1
	for _, expectedSQL := range orderedAdminDownSQL {
		position := strings.Index(adminDefinition.DownSQL, expectedSQL)
		if position < 0 {
			t.Errorf("admin-access down SQL does not contain %q", expectedSQL)
			continue
		}
		if position <= previousPosition {
			t.Errorf("admin-access down SQL has %q out of order", expectedSQL)
		}
		previousPosition = position
	}
	upperAdminDownSQL := strings.ToUpper(adminDefinition.DownSQL)
	for _, forbiddenSQL := range []string{"CASCADE", "IF EXISTS", "INQUIRIES"} {
		if strings.Contains(upperAdminDownSQL, forbiddenSQL) {
			t.Errorf("admin-access down SQL contains forbidden %q", forbiddenSQL)
		}
	}

	productDefinition := catalog[3]
	if productDefinition.Version != 4 ||
		productDefinition.Name != "create_products" {
		t.Errorf(
			"embedded migration identity: got %06d_%s",
			productDefinition.Version,
			productDefinition.Name,
		)
	}

	// These fragments lock version 000004 to the minimal durable Product storage
	// needed by the public catalogue read boundary. Every stored catalogue text
	// value is bounded, and draft remains the fail-closed state until a later
	// authenticated publishing workflow changes it.
	expectedProductUpSQL := []string{
		"CREATE TABLE public.products",
		"id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY",
		"slug text NOT NULL",
		"name text NOT NULL",
		"category text NOT NULL",
		"sort_order integer NOT NULL",
		"publication_status text NOT NULL DEFAULT 'draft'",
		"created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"CONSTRAINT products_slug_length",
		"CHECK (char_length(slug) BETWEEN 1 AND 120)",
		"CONSTRAINT products_slug_format",
		"CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$')",
		"CONSTRAINT products_slug_unique",
		"UNIQUE (slug)",
		"CONSTRAINT products_name_trimmed",
		"CHECK (name = btrim(name))",
		"CONSTRAINT products_name_length",
		"CHECK (char_length(name) BETWEEN 1 AND 160)",
		"CONSTRAINT products_category_trimmed",
		"CHECK (category = btrim(category))",
		"CONSTRAINT products_category_length",
		"CHECK (char_length(category) BETWEEN 1 AND 80)",
		"CONSTRAINT products_sort_order_positive",
		"CHECK (sort_order > 0)",
		"CONSTRAINT products_publication_status_supported",
		"CHECK (publication_status IN ('draft', 'published', 'archived'))",
		"CONSTRAINT products_timestamp_order",
		"CHECK (updated_at >= created_at)",
		"CREATE INDEX products_published_order_idx",
		"ON public.products (sort_order, id)",
		"WHERE publication_status = 'published'",
	}
	for _, expectedSQL := range expectedProductUpSQL {
		if !strings.Contains(productDefinition.UpSQL, expectedSQL) {
			t.Errorf("products up SQL does not contain %q", expectedSQL)
		}
	}

	// Schema migrations establish structure only. Product records will enter
	// through a later administrator workflow rather than embedded sample data.
	upperProductUpSQL := strings.ToUpper(productDefinition.UpSQL)
	for _, forbiddenSQL := range []string{
		"INSERT INTO",
		"UPDATE PUBLIC.PRODUCTS",
		"DELETE FROM",
		"CREATE EXTENSION",
	} {
		if strings.Contains(upperProductUpSQL, forbiddenSQL) {
			t.Errorf("products up SQL contains forbidden %q", forbiddenSQL)
		}
	}

	expectedProductDownSQL := `-- Reverse only the Product table introduced by version 000004. A strict drop
-- makes unexpected dependencies or schema drift fail visibly.
DROP TABLE public.products;
`
	if productDefinition.DownSQL != expectedProductDownSQL {
		t.Errorf(
			"products down SQL: got %q, want exact strict Product drop",
			productDefinition.DownSQL,
		)
	}
	upperProductDownSQL := strings.ToUpper(productDefinition.DownSQL)
	for _, forbiddenSQL := range []string{
		"CASCADE",
		"IF EXISTS",
		"INQUIRIES",
		"ADMIN_USERS",
		"ADMIN_SESSIONS",
	} {
		if strings.Contains(upperProductDownSQL, forbiddenSQL) {
			t.Errorf("products down SQL contains forbidden %q", forbiddenSQL)
		}
	}

	versionDefinition := catalog[4]
	if versionDefinition.Version != 5 ||
		versionDefinition.Name != "add_product_version" {
		t.Errorf(
			"embedded migration identity: got %06d_%s",
			versionDefinition.Version,
			versionDefinition.Name,
		)
	}

	// Version 000005 adds only the positive optimistic-concurrency value needed
	// by Product edit forms. It neither rewrites catalogue rows nor introduces
	// unrelated content-management fields.
	expectedVersionUpSQL := []string{
		"ALTER TABLE public.products",
		"ADD COLUMN version bigint NOT NULL DEFAULT 1",
		"CONSTRAINT products_version_positive",
		"CHECK (version > 0)",
	}
	for _, expectedSQL := range expectedVersionUpSQL {
		if !strings.Contains(versionDefinition.UpSQL, expectedSQL) {
			t.Errorf("product-version up SQL does not contain %q", expectedSQL)
		}
	}
	upperVersionUpSQL := strings.ToUpper(versionDefinition.UpSQL)
	for _, forbiddenSQL := range []string{
		"INSERT INTO",
		"UPDATE PUBLIC.PRODUCTS",
		"DELETE FROM",
		"CREATE TABLE",
		"CREATE INDEX",
	} {
		if strings.Contains(upperVersionUpSQL, forbiddenSQL) {
			t.Errorf("product-version up SQL contains forbidden %q", forbiddenSQL)
		}
	}

	expectedVersionDownSQL := `-- Reverse only the revision boundary introduced by version 000005. PostgreSQL
-- removes the column's dependent check constraint with the column itself.
ALTER TABLE public.products
    DROP COLUMN version;
`
	if versionDefinition.DownSQL != expectedVersionDownSQL {
		t.Errorf(
			"product-version down SQL: got %q, want exact strict column drop",
			versionDefinition.DownSQL,
		)
	}
	upperVersionDownSQL := strings.ToUpper(versionDefinition.DownSQL)
	for _, forbiddenSQL := range []string{
		"CASCADE",
		"IF EXISTS",
		"DROP TABLE",
		"INQUIRIES",
		"ADMIN_USERS",
		"ADMIN_SESSIONS",
	} {
		if strings.Contains(upperVersionDownSQL, forbiddenSQL) {
			t.Errorf("product-version down SQL contains forbidden %q", forbiddenSQL)
		}
	}

	contentDefinition := catalog[5]
	if contentDefinition.Version != 6 ||
		contentDefinition.Name != "add_product_content_and_cover" {
		t.Errorf(
			"embedded migration identity: got %06d_%s",
			contentDefinition.Version,
			contentDefinition.Name,
		)
	}

	// Version 000006 adds only bounded optional editorial fields and the
	// one-cover child table. It seeds neither Product text nor image bytes.
	expectedContentUpSQL := []string{
		"ADD COLUMN description text NOT NULL DEFAULT ''",
		"ADD COLUMN material text NOT NULL DEFAULT ''",
		"ADD COLUMN dimensions text NOT NULL DEFAULT ''",
		"CONSTRAINT products_description_trimmed",
		"CONSTRAINT products_description_length",
		"CHECK (char_length(description) <= 6000)",
		"CONSTRAINT products_material_trimmed",
		"CONSTRAINT products_material_length",
		"CHECK (char_length(material) <= 500)",
		"CONSTRAINT products_dimensions_trimmed",
		"CONSTRAINT products_dimensions_length",
		"CHECK (char_length(dimensions) <= 500)",
		"CREATE TABLE public.product_cover_images",
		"product_id bigint PRIMARY KEY",
		"REFERENCES public.products (id)",
		"ON DELETE CASCADE",
		"CONSTRAINT product_cover_images_version_positive",
		"CHECK (content_type IN ('image/jpeg', 'image/png'))",
		"CHECK (byte_size BETWEEN 1 AND 8388608)",
		"CHECK (octet_length(content) = byte_size)",
		"CHECK (width BETWEEN 1 AND 10000)",
		"CHECK (height BETWEEN 1 AND 10000)",
		"CHECK ((width::bigint * height::bigint) <= 25000000)",
		"CHECK (octet_length(sha256) = 32)",
		"CONSTRAINT product_cover_images_alt_text_trimmed",
		"CHECK (char_length(alt_text) BETWEEN 1 AND 300)",
		"CONSTRAINT product_cover_images_caption_trimmed",
		"CHECK (char_length(caption) <= 500)",
		"CHECK (updated_at >= created_at)",
	}
	for _, expectedSQL := range expectedContentUpSQL {
		if !strings.Contains(contentDefinition.UpSQL, expectedSQL) {
			t.Errorf("product-content up SQL does not contain %q", expectedSQL)
		}
	}
	upperContentUpSQL := strings.ToUpper(contentDefinition.UpSQL)
	for _, forbiddenSQL := range []string{
		"INSERT INTO PUBLIC.PRODUCTS",
		"INSERT INTO PUBLIC.PRODUCT_COVER_IMAGES",
		"DELETE FROM",
		"CREATE EXTENSION",
		"SEO",
		"PRICE",
		"FEATURED",
	} {
		if strings.Contains(upperContentUpSQL, forbiddenSQL) {
			t.Errorf("product-content up SQL contains forbidden %q", forbiddenSQL)
		}
	}

	orderedContentDownSQL := []string{
		"DROP TABLE public.product_cover_images;",
		"DROP CONSTRAINT products_dimensions_length",
		"DROP CONSTRAINT products_description_trimmed",
		"DROP COLUMN dimensions",
		"DROP COLUMN material",
		"DROP COLUMN description;",
	}
	previousPosition = -1
	for _, expectedSQL := range orderedContentDownSQL {
		position := strings.Index(contentDefinition.DownSQL, expectedSQL)
		if position < 0 {
			t.Errorf("product-content down SQL does not contain %q", expectedSQL)
			continue
		}
		if position <= previousPosition {
			t.Errorf("product-content down SQL has %q out of order", expectedSQL)
		}
		previousPosition = position
	}
	upperContentDownSQL := strings.ToUpper(contentDefinition.DownSQL)
	for _, forbiddenSQL := range []string{
		"CASCADE",
		"IF EXISTS",
		"DROP TABLE PUBLIC.PRODUCTS;",
		"INQUIRIES",
		"ADMIN_USERS",
		"ADMIN_SESSIONS",
	} {
		if strings.Contains(upperContentDownSQL, forbiddenSQL) {
			t.Errorf("product-content down SQL contains forbidden %q", forbiddenSQL)
		}
	}

	interiorDefinition := catalog[6]
	if interiorDefinition.Version != 7 ||
		interiorDefinition.Name != "create_interior_projects" {
		t.Errorf(
			"embedded migration identity: got %06d_%s",
			interiorDefinition.Version,
			interiorDefinition.Name,
		)
	}

	// Version 000007 owns a complete but deliberately narrow Interior slice. Its
	// parent table carries public content, lifecycle state, ordering, and one
	// optimistic revision; its dependent table stores exactly one reviewed cover.
	// Explicit fragments catch an accidentally optional public status, a sentinel
	// year default, broader media limits, or a missing publication-only index.
	expectedInteriorUpSQL := []string{
		"CREATE TABLE public.interior_projects",
		"id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY",
		"slug text NOT NULL",
		"title text NOT NULL",
		"typology text NOT NULL",
		"location text NOT NULL DEFAULT ''",
		"project_year integer",
		"project_status text NOT NULL",
		"description text NOT NULL DEFAULT ''",
		"sort_order integer NOT NULL",
		"publication_status text NOT NULL DEFAULT 'draft'",
		"version bigint NOT NULL DEFAULT 1",
		"created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"CONSTRAINT interior_projects_slug_length",
		"CHECK (char_length(slug) BETWEEN 1 AND 120)",
		"CONSTRAINT interior_projects_slug_format",
		"CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$')",
		"CONSTRAINT interior_projects_slug_unique",
		"UNIQUE (slug)",
		"CONSTRAINT interior_projects_title_trimmed",
		"CHECK (title = btrim(title))",
		"CONSTRAINT interior_projects_title_length",
		"CHECK (char_length(title) BETWEEN 1 AND 160)",
		"CONSTRAINT interior_projects_typology_trimmed",
		"CHECK (typology = btrim(typology))",
		"CONSTRAINT interior_projects_typology_length",
		"CHECK (char_length(typology) BETWEEN 1 AND 80)",
		"CONSTRAINT interior_projects_location_trimmed",
		"CHECK (location = btrim(location))",
		"CONSTRAINT interior_projects_location_length",
		"CHECK (char_length(location) <= 160)",
		"CONSTRAINT interior_projects_project_year_supported",
		"project_year IS NULL OR",
		"project_year BETWEEN 1000 AND 9999",
		"CONSTRAINT interior_projects_project_status_trimmed",
		"CHECK (project_status = btrim(project_status))",
		"CONSTRAINT interior_projects_project_status_length",
		"CHECK (char_length(project_status) BETWEEN 1 AND 80)",
		"CONSTRAINT interior_projects_description_trimmed",
		"CHECK (description = btrim(description))",
		"CONSTRAINT interior_projects_description_length",
		"CHECK (char_length(description) <= 6000)",
		"CONSTRAINT interior_projects_sort_order_positive",
		"CHECK (sort_order > 0)",
		"CONSTRAINT interior_projects_publication_status_supported",
		"CHECK (publication_status IN ('draft', 'published', 'archived'))",
		"CONSTRAINT interior_projects_version_positive",
		"CHECK (version > 0)",
		"CONSTRAINT interior_projects_timestamp_order",
		"CHECK (updated_at >= created_at)",
		"CREATE INDEX interior_projects_published_order_idx",
		"ON public.interior_projects (sort_order, id)",
		"WHERE publication_status = 'published'",
		"CREATE TABLE public.interior_project_cover_images",
		"interior_project_id bigint NOT NULL",
		"CONSTRAINT interior_project_cover_images_pkey",
		"PRIMARY KEY (interior_project_id)",
		"CONSTRAINT interior_project_cover_images_project_id_foreign",
		"FOREIGN KEY (interior_project_id)",
		"REFERENCES public.interior_projects (id)",
		"ON DELETE CASCADE",
		"CONSTRAINT interior_project_cover_images_version_positive",
		"CONSTRAINT interior_project_cover_images_content_type_supported",
		"CHECK (content_type IN ('image/jpeg', 'image/png'))",
		"CONSTRAINT interior_project_cover_images_byte_size_supported",
		"CHECK (byte_size BETWEEN 1 AND 8388608)",
		"CONSTRAINT interior_project_cover_images_content_size_matches",
		"CHECK (octet_length(content) = byte_size)",
		"CONSTRAINT interior_project_cover_images_width_supported",
		"CHECK (width BETWEEN 1 AND 10000)",
		"CONSTRAINT interior_project_cover_images_height_supported",
		"CHECK (height BETWEEN 1 AND 10000)",
		"CONSTRAINT interior_project_cover_images_pixel_count_supported",
		"CHECK ((width::bigint * height::bigint) <= 25000000)",
		"CONSTRAINT interior_project_cover_images_sha256_length",
		"CHECK (octet_length(sha256) = 32)",
		"CONSTRAINT interior_project_cover_images_alt_text_trimmed",
		"CHECK (alt_text = btrim(alt_text))",
		"CONSTRAINT interior_project_cover_images_alt_text_length",
		"CHECK (char_length(alt_text) BETWEEN 1 AND 300)",
		"CONSTRAINT interior_project_cover_images_caption_trimmed",
		"CHECK (caption = btrim(caption))",
		"CONSTRAINT interior_project_cover_images_caption_length",
		"CHECK (char_length(caption) <= 500)",
		"CONSTRAINT interior_project_cover_images_timestamp_order",
	}
	for _, expectedSQL := range expectedInteriorUpSQL {
		if !strings.Contains(interiorDefinition.UpSQL, expectedSQL) {
			t.Errorf("Interior-project up SQL does not contain %q", expectedSQL)
		}
	}

	// Migration 7 establishes structure only. Its separate Architecture sibling,
	// homepage selection, galleries, and business records remain explicit changes.
	upperInteriorUpSQL := strings.ToUpper(interiorDefinition.UpSQL)
	if count := strings.Count(
		upperInteriorUpSQL,
		"CREATE TABLE PUBLIC.",
	); count != 2 {
		t.Errorf("Interior-project CREATE TABLE count: got %d, want 2", count)
	}
	if count := strings.Count(
		upperInteriorUpSQL,
		"ON DELETE CASCADE",
	); count != 1 {
		t.Errorf("Interior-project ON DELETE CASCADE count: got %d, want 1", count)
	}
	for _, forbiddenSQL := range []string{
		"INSERT INTO",
		"UPDATE PUBLIC.INTERIOR_PROJECTS",
		"DELETE FROM",
		"CREATE EXTENSION",
		"CREATE TABLE PUBLIC.ARCHITECTURE_PROJECTS",
		"CREATE TABLE PUBLIC.INTERIOR_PROJECT_GALLERY",
		"PROJECT_YEAR INTEGER NOT NULL",
		"PROJECT_YEAR INTEGER DEFAULT",
		"FEATURED",
		"SEO",
	} {
		if strings.Contains(upperInteriorUpSQL, forbiddenSQL) {
			t.Errorf("Interior-project up SQL contains forbidden %q", forbiddenSQL)
		}
	}

	// Reverse dependency order is part of the migration contract: the cover must
	// disappear before its parent. Neither forgiving modifiers nor unrelated
	// schema names may conceal drift or broaden a destructive rollback.
	orderedInteriorDownSQL := []string{
		"DROP TABLE public.interior_project_cover_images;",
		"DROP TABLE public.interior_projects;",
	}
	previousPosition = -1
	for _, expectedSQL := range orderedInteriorDownSQL {
		position := strings.Index(interiorDefinition.DownSQL, expectedSQL)
		if position < 0 {
			t.Errorf("Interior-project down SQL does not contain %q", expectedSQL)
			continue
		}
		if position <= previousPosition {
			t.Errorf("Interior-project down SQL has %q out of order", expectedSQL)
		}
		previousPosition = position
	}
	upperInteriorDownSQL := strings.ToUpper(interiorDefinition.DownSQL)
	for _, forbiddenSQL := range []string{
		"CASCADE",
		"IF EXISTS",
		"PRODUCTS",
		"INQUIRIES",
		"ADMIN_USERS",
		"ADMIN_SESSIONS",
		"ARCHITECTURE_PROJECTS",
	} {
		if strings.Contains(upperInteriorDownSQL, forbiddenSQL) {
			t.Errorf("Interior-project down SQL contains forbidden %q", forbiddenSQL)
		}
	}

	architectureDefinition := catalog[7]
	if architectureDefinition.Version != 8 ||
		architectureDefinition.Name != "create_architecture_projects" {
		t.Errorf(
			"embedded migration identity: got %06d_%s",
			architectureDefinition.Version,
			architectureDefinition.Name,
		)
	}

	// Version 000008 gives Architecture its own complete vertical boundary. The
	// explicit fragments protect every public-content, lifecycle, optimistic-
	// concurrency, media, and ordering guarantee without coupling the new schema
	// to the pre-existing Interior relation merely because their current shapes
	// are intentionally parallel.
	expectedArchitectureUpSQL := []string{
		"CREATE TABLE public.architecture_projects",
		"id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY",
		"slug text NOT NULL",
		"title text NOT NULL",
		"typology text NOT NULL",
		"location text NOT NULL DEFAULT ''",
		"project_year integer",
		"project_status text NOT NULL",
		"description text NOT NULL DEFAULT ''",
		"sort_order integer NOT NULL",
		"publication_status text NOT NULL DEFAULT 'draft'",
		"version bigint NOT NULL DEFAULT 1",
		"created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"CONSTRAINT architecture_projects_slug_length",
		"CHECK (char_length(slug) BETWEEN 1 AND 120)",
		"CONSTRAINT architecture_projects_slug_format",
		"CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$')",
		"CONSTRAINT architecture_projects_slug_unique",
		"UNIQUE (slug)",
		"CONSTRAINT architecture_projects_title_trimmed",
		"CHECK (title = btrim(title))",
		"CONSTRAINT architecture_projects_title_length",
		"CHECK (char_length(title) BETWEEN 1 AND 160)",
		"CONSTRAINT architecture_projects_typology_trimmed",
		"CHECK (typology = btrim(typology))",
		"CONSTRAINT architecture_projects_typology_length",
		"CHECK (char_length(typology) BETWEEN 1 AND 80)",
		"CONSTRAINT architecture_projects_location_trimmed",
		"CHECK (location = btrim(location))",
		"CONSTRAINT architecture_projects_location_length",
		"CHECK (char_length(location) <= 160)",
		"CONSTRAINT architecture_projects_project_year_supported",
		"project_year IS NULL OR",
		"project_year BETWEEN 1000 AND 9999",
		"CONSTRAINT architecture_projects_project_status_trimmed",
		"CHECK (project_status = btrim(project_status))",
		"CONSTRAINT architecture_projects_project_status_length",
		"CHECK (char_length(project_status) BETWEEN 1 AND 80)",
		"CONSTRAINT architecture_projects_description_trimmed",
		"CHECK (description = btrim(description))",
		"CONSTRAINT architecture_projects_description_length",
		"CHECK (char_length(description) <= 6000)",
		"CONSTRAINT architecture_projects_sort_order_positive",
		"CHECK (sort_order > 0)",
		"CONSTRAINT architecture_projects_publication_status_supported",
		"CHECK (publication_status IN ('draft', 'published', 'archived'))",
		"CONSTRAINT architecture_projects_version_positive",
		"CHECK (version > 0)",
		"CONSTRAINT architecture_projects_timestamp_order",
		"CHECK (updated_at >= created_at)",
		"CREATE INDEX architecture_projects_published_order_idx",
		"ON public.architecture_projects (sort_order, id)",
		"WHERE publication_status = 'published'",
		"CREATE TABLE public.architecture_project_cover_images",
		"architecture_project_id bigint NOT NULL",
		"CONSTRAINT architecture_project_cover_images_pkey",
		"PRIMARY KEY (architecture_project_id)",
		"CONSTRAINT architecture_project_cover_images_project_id_foreign",
		"FOREIGN KEY (architecture_project_id)",
		"REFERENCES public.architecture_projects (id)",
		"ON DELETE CASCADE",
		"CONSTRAINT architecture_project_cover_images_version_positive",
		"CONSTRAINT architecture_project_cover_images_content_type_supported",
		"CHECK (content_type IN ('image/jpeg', 'image/png'))",
		"CONSTRAINT architecture_project_cover_images_byte_size_supported",
		"CHECK (byte_size BETWEEN 1 AND 8388608)",
		"CONSTRAINT architecture_project_cover_images_content_size_matches",
		"CHECK (octet_length(content) = byte_size)",
		"CONSTRAINT architecture_project_cover_images_width_supported",
		"CHECK (width BETWEEN 1 AND 10000)",
		"CONSTRAINT architecture_project_cover_images_height_supported",
		"CHECK (height BETWEEN 1 AND 10000)",
		"CONSTRAINT architecture_project_cover_images_pixel_count_supported",
		"CHECK ((width::bigint * height::bigint) <= 25000000)",
		"CONSTRAINT architecture_project_cover_images_sha256_length",
		"CHECK (octet_length(sha256) = 32)",
		"CONSTRAINT architecture_project_cover_images_alt_text_trimmed",
		"CHECK (alt_text = btrim(alt_text))",
		"CONSTRAINT architecture_project_cover_images_alt_text_length",
		"CHECK (char_length(alt_text) BETWEEN 1 AND 300)",
		"CONSTRAINT architecture_project_cover_images_caption_trimmed",
		"CHECK (caption = btrim(caption))",
		"CONSTRAINT architecture_project_cover_images_caption_length",
		"CHECK (char_length(caption) <= 500)",
		"CONSTRAINT architecture_project_cover_images_timestamp_order",
	}
	for _, expectedSQL := range expectedArchitectureUpSQL {
		if !strings.Contains(architectureDefinition.UpSQL, expectedSQL) {
			t.Errorf("Architecture-project up SQL does not contain %q", expectedSQL)
		}
	}

	// Stage 23 establishes only empty Architecture structure. It must neither
	// manufacture portfolio content nor alter another discipline's durable data.
	upperArchitectureUpSQL := strings.ToUpper(architectureDefinition.UpSQL)
	if count := strings.Count(
		upperArchitectureUpSQL,
		"CREATE TABLE PUBLIC.",
	); count != 2 {
		t.Errorf("Architecture-project CREATE TABLE count: got %d, want 2", count)
	}
	if count := strings.Count(
		upperArchitectureUpSQL,
		"ON DELETE CASCADE",
	); count != 1 {
		t.Errorf("Architecture-project ON DELETE CASCADE count: got %d, want 1", count)
	}
	for _, forbiddenSQL := range []string{
		"INSERT INTO",
		"UPDATE PUBLIC.ARCHITECTURE_PROJECTS",
		"DELETE FROM",
		"CREATE EXTENSION",
		"ALTER TABLE PUBLIC.INTERIOR_PROJECTS",
		"CREATE TABLE PUBLIC.ARCHITECTURE_PROJECT_GALLERY",
		"PROJECT_YEAR INTEGER NOT NULL",
		"PROJECT_YEAR INTEGER DEFAULT",
		"FEATURED",
		"SEO",
	} {
		if strings.Contains(upperArchitectureUpSQL, forbiddenSQL) {
			t.Errorf("Architecture-project up SQL contains forbidden %q", forbiddenSQL)
		}
	}

	// Strict child-first rollback keeps the foreign-key dependency visible and
	// proves version 8 cannot remove Interior, Product, inquiry, or access data.
	orderedArchitectureDownSQL := []string{
		"DROP TABLE public.architecture_project_cover_images;",
		"DROP TABLE public.architecture_projects;",
	}
	previousPosition = -1
	for _, expectedSQL := range orderedArchitectureDownSQL {
		position := strings.Index(architectureDefinition.DownSQL, expectedSQL)
		if position < 0 {
			t.Errorf("Architecture-project down SQL does not contain %q", expectedSQL)
			continue
		}
		if position <= previousPosition {
			t.Errorf("Architecture-project down SQL has %q out of order", expectedSQL)
		}
		previousPosition = position
	}
	upperArchitectureDownSQL := strings.ToUpper(architectureDefinition.DownSQL)
	for _, forbiddenSQL := range []string{
		"CASCADE",
		"IF EXISTS",
		"INTERIOR_PROJECTS",
		"PRODUCTS",
		"INQUIRIES",
		"ADMIN_USERS",
		"ADMIN_SESSIONS",
	} {
		if strings.Contains(upperArchitectureDownSQL, forbiddenSQL) {
			t.Errorf("Architecture-project down SQL contains forbidden %q", forbiddenSQL)
		}
	}

	siteContentDefinition := catalog[8]
	if siteContentDefinition.Version != 9 ||
		siteContentDefinition.Name != "create_homepage_contact_content" {
		t.Errorf(
			"embedded migration identity: got %06d_%s",
			siteContentDefinition.Version,
			siteContentDefinition.Name,
		)
	}

	// Version 000009 persists only the two singleton documents and the optional
	// reviewed hero child. These fragments pin the public-copy bounds, complete
	// SEO titles, optimistic revisions, fixed-reference delete policy, and the
	// same reviewed-image envelope already used by discipline covers.
	expectedSiteContentUpSQL := []string{
		"CREATE TABLE public.homepage_content",
		"id smallint PRIMARY KEY",
		"studio_name text NOT NULL",
		"descriptor text NOT NULL",
		"managed_hero_enabled boolean NOT NULL DEFAULT false",
		"featured_product_id bigint",
		"featured_interior_project_id bigint",
		"featured_architecture_project_id bigint",
		"seo_title text NOT NULL",
		"seo_description text NOT NULL",
		"version bigint NOT NULL DEFAULT 1",
		"CONSTRAINT homepage_content_singleton",
		"CHECK (id = 1)",
		"CONSTRAINT homepage_content_studio_name_trimmed",
		"CONSTRAINT homepage_content_studio_name_length",
		"CHECK (char_length(studio_name) BETWEEN 1 AND 120)",
		"CONSTRAINT homepage_content_studio_name_single_line",
		"CONSTRAINT homepage_content_descriptor_trimmed",
		"CONSTRAINT homepage_content_descriptor_length",
		"CHECK (char_length(descriptor) BETWEEN 1 AND 160)",
		"CONSTRAINT homepage_content_descriptor_single_line",
		"CONSTRAINT homepage_content_featured_product_id_foreign",
		"REFERENCES public.products (id)",
		"CONSTRAINT homepage_content_featured_interior_project_id_foreign",
		"REFERENCES public.interior_projects (id)",
		"CONSTRAINT homepage_content_featured_architecture_project_id_foreign",
		"REFERENCES public.architecture_projects (id)",
		"CONSTRAINT homepage_content_seo_title_trimmed",
		"CONSTRAINT homepage_content_seo_title_length",
		"CHECK (char_length(seo_title) BETWEEN 1 AND 160)",
		"CONSTRAINT homepage_content_seo_title_single_line",
		"CONSTRAINT homepage_content_seo_description_trimmed",
		"CONSTRAINT homepage_content_seo_description_length",
		"CHECK (char_length(seo_description) BETWEEN 1 AND 320)",
		"CONSTRAINT homepage_content_seo_description_single_line",
		"CONSTRAINT homepage_content_version_positive",
		"CONSTRAINT homepage_content_timestamp_order",
		"INSERT INTO public.homepage_content",
		"'Zafarmand'",
		"'Design Studio'",
		"'Home | Zafarmand'",
		"CREATE TABLE public.homepage_hero_images",
		"homepage_content_id smallint NOT NULL",
		"CONSTRAINT homepage_hero_images_pkey",
		"PRIMARY KEY (homepage_content_id)",
		"CONSTRAINT homepage_hero_images_homepage_content_id_foreign",
		"REFERENCES public.homepage_content (id)",
		"CONSTRAINT homepage_hero_images_version_positive",
		"CONSTRAINT homepage_hero_images_content_type_supported",
		"CHECK (content_type IN ('image/jpeg', 'image/png'))",
		"CONSTRAINT homepage_hero_images_byte_size_supported",
		"CHECK (byte_size BETWEEN 1 AND 8388608)",
		"CONSTRAINT homepage_hero_images_content_size_matches",
		"CHECK (octet_length(content) = byte_size)",
		"CONSTRAINT homepage_hero_images_width_supported",
		"CHECK (width BETWEEN 1 AND 10000)",
		"CONSTRAINT homepage_hero_images_height_supported",
		"CHECK (height BETWEEN 1 AND 10000)",
		"CONSTRAINT homepage_hero_images_pixel_count_supported",
		"CHECK ((width::bigint * height::bigint) <= 25000000)",
		"CONSTRAINT homepage_hero_images_sha256_length",
		"CHECK (octet_length(sha256) = 32)",
		"CONSTRAINT homepage_hero_images_alt_text_trimmed",
		"CONSTRAINT homepage_hero_images_alt_text_length",
		"CHECK (char_length(alt_text) BETWEEN 1 AND 300)",
		"CONSTRAINT homepage_hero_images_timestamp_order",
		"CREATE TABLE public.contact_content",
		"eyebrow text NOT NULL",
		"heading text NOT NULL",
		"introduction text NOT NULL",
		"contact_email text NOT NULL DEFAULT ''",
		"phone_display text NOT NULL DEFAULT ''",
		"phone_e164 text NOT NULL DEFAULT ''",
		"address text NOT NULL DEFAULT ''",
		"CONSTRAINT contact_content_singleton",
		"CONSTRAINT contact_content_eyebrow_trimmed",
		"CONSTRAINT contact_content_eyebrow_length",
		"CHECK (char_length(eyebrow) BETWEEN 1 AND 80)",
		"CONSTRAINT contact_content_eyebrow_single_line",
		"CONSTRAINT contact_content_heading_trimmed",
		"CONSTRAINT contact_content_heading_length",
		"CHECK (char_length(heading) BETWEEN 1 AND 160)",
		"CONSTRAINT contact_content_heading_single_line",
		"CONSTRAINT contact_content_introduction_trimmed",
		"CONSTRAINT contact_content_introduction_length",
		"CHECK (char_length(introduction) BETWEEN 1 AND 1200)",
		"CONSTRAINT contact_content_email_normalized",
		"CHECK (contact_email = lower(btrim(contact_email)))",
		"CONSTRAINT contact_content_email_length",
		"CHECK (char_length(contact_email) <= 254)",
		"CONSTRAINT contact_content_email_shape",
		"CONSTRAINT contact_content_phone_display_trimmed",
		"CONSTRAINT contact_content_phone_display_length",
		"CHECK (char_length(phone_display) <= 60)",
		"CONSTRAINT contact_content_phone_display_single_line",
		"CONSTRAINT contact_content_phone_e164_normalized",
		"CONSTRAINT contact_content_phone_pair",
		"btrim(phone_e164) ~ '^\\+[1-9][0-9]{7,14}$'",
		"CONSTRAINT contact_content_address_trimmed",
		"CONSTRAINT contact_content_address_length",
		"CHECK (char_length(address) <= 500)",
		"CONSTRAINT contact_content_seo_title_trimmed",
		"CONSTRAINT contact_content_seo_title_length",
		"CONSTRAINT contact_content_seo_title_single_line",
		"CONSTRAINT contact_content_seo_description_trimmed",
		"CONSTRAINT contact_content_seo_description_length",
		"CONSTRAINT contact_content_seo_description_single_line",
		"CONSTRAINT contact_content_version_positive",
		"CONSTRAINT contact_content_timestamp_order",
		"INSERT INTO public.contact_content",
		"'Contact'",
		"'Begin a conversation'",
		"'Choose a discipline and share the context Zafarmand should review.'",
		"'Contact | Zafarmand'",
		"'Zafarmand design studio'",
	}
	for _, expectedSQL := range expectedSiteContentUpSQL {
		if !strings.Contains(siteContentDefinition.UpSQL, expectedSQL) {
			t.Errorf("site-content up SQL does not contain %q", expectedSQL)
		}
	}

	// Exactly three relations and two singleton seed writes prevent Stage 24 from
	// growing a second featured-items abstraction or manufacturing portfolio or
	// image data. The three RESTRICT references preserve deliberate selections;
	// the one CASCADE belongs only to the homepage-owned hero child.
	upperSiteContentUpSQL := strings.ToUpper(siteContentDefinition.UpSQL)
	if count := strings.Count(upperSiteContentUpSQL, "CREATE TABLE PUBLIC."); count != 3 {
		t.Errorf("site-content CREATE TABLE count: got %d, want 3", count)
	}
	if count := strings.Count(upperSiteContentUpSQL, "INSERT INTO PUBLIC."); count != 2 {
		t.Errorf("site-content INSERT count: got %d, want 2", count)
	}
	if count := strings.Count(upperSiteContentUpSQL, "ON DELETE RESTRICT"); count != 3 {
		t.Errorf("site-content ON DELETE RESTRICT count: got %d, want 3", count)
	}
	if count := strings.Count(upperSiteContentUpSQL, "ON DELETE CASCADE"); count != 1 {
		t.Errorf("site-content ON DELETE CASCADE count: got %d, want 1", count)
	}
	for _, forbiddenSQL := range []string{
		"CREATE TABLE PUBLIC.HOMEPAGE_FEATURED_ITEMS",
		"CAPTION TEXT",
		"INSERT INTO PUBLIC.HOMEPAGE_HERO_IMAGES",
		"INSERT INTO PUBLIC.PRODUCTS",
		"INSERT INTO PUBLIC.INTERIOR_PROJECTS",
		"INSERT INTO PUBLIC.ARCHITECTURE_PROJECTS",
		"@EXAMPLE.",
		"CREATE EXTENSION",
	} {
		if strings.Contains(upperSiteContentUpSQL, forbiddenSQL) {
			t.Errorf("site-content up SQL contains forbidden %q", forbiddenSQL)
		}
	}

	// Strict child-first rollback removes only Stage 24. Contact is independent,
	// while the hero must precede its homepage parent because of its foreign key.
	orderedSiteContentDownSQL := []string{
		"DROP TABLE public.homepage_hero_images;",
		"DROP TABLE public.contact_content;",
		"DROP TABLE public.homepage_content;",
	}
	previousPosition = -1
	for _, expectedSQL := range orderedSiteContentDownSQL {
		position := strings.Index(siteContentDefinition.DownSQL, expectedSQL)
		if position < 0 {
			t.Errorf("site-content down SQL does not contain %q", expectedSQL)
			continue
		}
		if position <= previousPosition {
			t.Errorf("site-content down SQL has %q out of order", expectedSQL)
		}
		previousPosition = position
	}
	upperSiteContentDownSQL := strings.ToUpper(siteContentDefinition.DownSQL)
	if count := strings.Count(upperSiteContentDownSQL, "DROP TABLE PUBLIC."); count != 3 {
		t.Errorf("site-content DROP TABLE count: got %d, want 3", count)
	}
	for _, forbiddenSQL := range []string{
		"CASCADE",
		"IF EXISTS",
		"DROP TABLE PUBLIC.PRODUCTS",
		"DROP TABLE PUBLIC.INTERIOR_PROJECTS",
		"DROP TABLE PUBLIC.ARCHITECTURE_PROJECTS",
		"DROP TABLE PUBLIC.INQUIRIES",
		"DROP TABLE PUBLIC.ADMIN_USERS",
		"DROP TABLE PUBLIC.ADMIN_SESSIONS",
	} {
		if strings.Contains(upperSiteContentDownSQL, forbiddenSQL) {
			t.Errorf("site-content down SQL contains forbidden %q", forbiddenSQL)
		}
	}
}
