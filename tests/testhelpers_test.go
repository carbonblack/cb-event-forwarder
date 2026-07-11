package tests

import (
	"os"
	"path/filepath"
	"testing"
)

// readRepoFile reads a file relative to the repo root (one level above the tests/ directory).
func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
