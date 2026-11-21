// Package paths says where Laser Email keeps its data.
package paths

import (
	"os"
	"path/filepath"
)

// DataDir is ~/Library/Application Support/Laser Email on a Mac
// (LASER_DATA overrides it).
func DataDir() string {
	if d := os.Getenv("LASER_DATA"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "Laser Email")
}

// Socket is the daemon's Unix socket (LASER_SOCKET overrides it).
func Socket() string {
	if s := os.Getenv("LASER_SOCKET"); s != "" {
		return s
	}
	return filepath.Join(DataDir(), "laserd.sock")
}
