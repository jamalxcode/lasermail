//go:build darwin

package secret

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Keychain stores secrets as generic passwords in the login Keychain using
// /usr/bin/security. Values are passed on its standard input (interactive
// mode), never on the command line, so they don't show up in `ps`.
type Keychain struct{}

// Default is the platform's store.
func Default(dataDir string) Store { return Keychain{} }

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func (Keychain) Get(k string) (string, error) {
	out, err := exec.Command("/usr/bin/security", "find-generic-password", "-s", Service, "-a", k, "-w").Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 44 {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("keychain: %w", err)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

func (Keychain) Set(k, v string) error {
	if strings.ContainsAny(v, "\n\r") {
		return fmt.Errorf("keychain: value contains a line break")
	}
	cmd := exec.Command("/usr/bin/security", "-i")
	cmd.Stdin = strings.NewReader("add-generic-password -U -s " + quote(Service) + " -a " + quote(k) +
		" -l " + quote(Service+" ("+k+")") + " -w " + quote(v) + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("keychain: %v: %s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "error") {
		return fmt.Errorf("keychain: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (Keychain) Delete(k string) error {
	err := exec.Command("/usr/bin/security", "delete-generic-password", "-s", Service, "-a", k).Run()
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 44 {
		return nil
	}
	return err
}
