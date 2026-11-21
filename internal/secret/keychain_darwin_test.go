//go:build darwin

package secret

import (
	"os"
	"testing"
)

func TestKeychain(t *testing.T) {
	if os.Getenv("LASER_TEST_KEYCHAIN") == "" {
		t.Skip("set LASER_TEST_KEYCHAIN=1 to touch the login Keychain")
	}
	exercise(t, Keychain{})
}
