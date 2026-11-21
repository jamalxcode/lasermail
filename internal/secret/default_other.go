//go:build !darwin

package secret

import "path/filepath"

// Default is the platform's store: off macOS, a 0600 file in the data folder.
func Default(dataDir string) Store { return NewFile(filepath.Join(dataDir, "secrets.json")) }
