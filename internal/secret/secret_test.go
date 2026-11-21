package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func exercise(t *testing.T, s Store) {
	if _, err := s.Get("a"); err != ErrNotFound {
		t.Fatalf("empty get: %v", err)
	}
	if err := s.Set("a", `p"a\ss word`); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get("a"); err != nil || v != `p"a\ss word` {
		t.Fatalf("get = %q, %v", v, err)
	}
	if err := s.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("a"); err != ErrNotFound {
		t.Fatalf("after delete: %v", err)
	}
}

func TestMemory(t *testing.T) { exercise(t, NewMemory()) }

func TestFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "d", "secrets.json")
	exercise(t, NewFile(p))
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}
