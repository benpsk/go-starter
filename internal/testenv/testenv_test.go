package testenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFileParsesEnvLines(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	path := filepath.Join(tmp, ".env.test")
	content := strings.Join([]string{
		"# comment",
		" SIMPLE = value ",
		`QUOTED="quoted value"`,
		"SINGLE='single value'",
		"MALFORMED",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	if err := LoadFile(path); err != nil {
		t.Fatalf("load env file: %v", err)
	}

	if got := os.Getenv("SIMPLE"); got != "value" {
		t.Fatalf("expected SIMPLE=value, got %q", got)
	}
	if got := os.Getenv("QUOTED"); got != "quoted value" {
		t.Fatalf("expected QUOTED parsed, got %q", got)
	}
	if got := os.Getenv("SINGLE"); got != "single value" {
		t.Fatalf("expected SINGLE parsed, got %q", got)
	}
}
