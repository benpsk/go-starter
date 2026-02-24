package static

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed *
var files embed.FS

func FileSystem() http.FileSystem {
	return http.FS(files)
}

func FS() fs.FS {
	return files
}
