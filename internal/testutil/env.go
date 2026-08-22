// Package testutil holds the shared test scaffolding: loading .env.test,
// serialising access to the integration database, and preparing its schema.
package testutil

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// LoadEnv finds the nearest .env.test and loads it into the process
// environment, overriding any value already set so a developer's shell cannot
// point a test run at the development database.
func LoadEnv() error {
	path, err := FindUp(".env.test")
	if err != nil {
		return err
	}
	if err := godotenv.Overload(path); err != nil {
		return fmt.Errorf("load %s: %w", path, err)
	}
	return nil
}

// FindUp walks up from the working directory looking for name.
func FindUp(name string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	for {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%s not found in any parent of the working directory", name)
		}
		dir = parent
	}
}

// RepoRoot returns the directory holding go.mod.
func RepoRoot() (string, error) {
	path, err := FindUp("go.mod")
	if err != nil {
		return "", err
	}
	return filepath.Dir(path), nil
}
