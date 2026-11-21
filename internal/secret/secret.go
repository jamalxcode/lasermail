// Package secret keeps account passwords and OAuth tokens out of the
// database: in the macOS Keychain on a Mac, and in a 0600 file elsewhere
// (development and tests).
package secret

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Service is the Keychain service name every secret is stored under.
const Service = "Laser Email"

// ErrNotFound means there is no secret for that key.
var ErrNotFound = errors.New("secret not found")

// Store keeps secrets by key (an account id plus a purpose).
type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

// Memory is an in-memory store for tests.
type Memory struct {
	mu sync.Mutex
	m  map[string]string
}

func NewMemory() *Memory { return &Memory{m: map[string]string{}} }

func (s *Memory) Get(k string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[k]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *Memory) Set(k, v string) error {
	s.mu.Lock()
	s.m[k] = v
	s.mu.Unlock()
	return nil
}

func (s *Memory) Delete(k string) error {
	s.mu.Lock()
	delete(s.m, k)
	s.mu.Unlock()
	return nil
}

// File is a JSON file readable only by the owner. It is the fallback off
// macOS; on a Mac the Keychain is used.
type File struct {
	mu   sync.Mutex
	path string
}

func NewFile(path string) *File { return &File{path: path} }

func (s *File) load() (map[string]string, error) {
	m := map[string]string{}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	return m, json.Unmarshal(b, &m)
}

func (s *File) save(m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(m)
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *File) Get(k string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return "", err
	}
	v, ok := m[k]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *File) Set(k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	m[k] = v
	return s.save(m)
}

func (s *File) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	delete(m, k)
	return s.save(m)
}
