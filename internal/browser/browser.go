// Package browser opens a URL in the user's default browser (for Google
// sign-in). Nothing else in Laser Email opens web pages.
package browser

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Open opens an https URL.
func Open(url string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("refusing to open %q", url)
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("/usr/bin/open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
