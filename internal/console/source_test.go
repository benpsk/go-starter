package console

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSourcePrefersAnExistingDirectory(t *testing.T) {
	dir := t.TempDir()

	source, err := migrationSource(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if source.embedded() {
		t.Fatalf("an existing directory should win over the embedded copies")
	}
	if source.describe() != dir {
		t.Fatalf("describe() = %q, want %q", source.describe(), dir)
	}
}

func TestResolveSourceFallsBackToEmbedded(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		// An empty path asks for the embedded set explicitly.
		{"empty path", ""},
		// A deployed binary has no repository checkout beside it, so the
		// default path being absent is normal, not an error.
		{"absent default path", defaultMigrationsDir},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			source, err := migrationSource(tc.path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !source.embedded() {
				t.Fatalf("expected the embedded source")
			}
			if source.describe() != "embedded" {
				t.Fatalf("describe() = %q", source.describe())
			}
		})
	}
}

func TestResolveSourceRejectsAnAbsentExplicitPath(t *testing.T) {
	// Asking for something specific and not getting it is a mistake worth
	// reporting, rather than silently running the embedded set.
	_, err := migrationSource(filepath.Join(t.TempDir(), "nope"))
	if !IsUsageError(err) {
		t.Fatalf("expected a usage error, got %v", err)
	}
}

func TestResolveSourceRejectsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "migrations")
	if err := os.WriteFile(file, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := migrationSource(file); !IsUsageError(err) {
		t.Fatalf("expected a usage error, got %v", err)
	}
}

func TestSeederSourceUsesTheSeedersDirectory(t *testing.T) {
	t.Chdir(t.TempDir())

	source, err := seederSource(defaultSeedersDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !source.embedded() {
		t.Fatalf("expected the embedded seeders")
	}
}

func TestEmbeddedSourcesCarryTheRepositoryFiles(t *testing.T) {
	// Guards against an //go:embed path change silently emptying the bundle a
	// deployed binary relies on.
	migrations, err := migrationSource("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names, err := sqlFileNames(migrations)
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	if len(names) == 0 {
		t.Fatalf("expected at least one embedded migration")
	}
}
