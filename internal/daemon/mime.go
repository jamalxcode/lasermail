package daemon

import (
	"mime"
	"path/filepath"
)

func contentType(path string, _ []byte) string {
	if t := mime.TypeByExtension(filepath.Ext(path)); t != "" {
		return t
	}
	return "application/octet-stream"
}
