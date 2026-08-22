// Package static embeds the built frontend assets into the binary.
package static

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
)

//go:embed *
var files embed.FS

// FileSystem exposes the embedded assets as an http.FileSystem.
func FileSystem() http.FileSystem {
	return http.FS(files)
}

// FileSystemWithFallback serves embedded assets first, then falls back to the
// provided filesystem when an asset was not embedded. Production uses this.
func FileSystemWithFallback(fallback http.FileSystem) http.FileSystem {
	if fallback == nil {
		return FileSystem()
	}
	return fallbackFileSystem{primary: FileSystem(), fallback: fallback}
}

// FileSystemPreferring serves the provided filesystem first and falls back to
// the embedded assets. Development uses this so a rebuilt stylesheet or bundle
// reaches the browser without recompiling the binary the assets are baked into.
func FileSystemPreferring(preferred http.FileSystem) http.FileSystem {
	if preferred == nil {
		return FileSystem()
	}
	return fallbackFileSystem{primary: preferred, fallback: FileSystem()}
}

// FS returns the embedded assets as a generic fs.FS.
func FS() fs.FS {
	return files
}

type fallbackFileSystem struct {
	primary  http.FileSystem
	fallback http.FileSystem
}

func (f fallbackFileSystem) Open(name string) (http.File, error) {
	file, err := f.primary.Open(name)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return file, err
	}
	return f.fallback.Open(name)
}
