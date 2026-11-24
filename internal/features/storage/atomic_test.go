package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := WriteAtomic(path, []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "ok" {
		t.Fatalf("atomic write failed: %q, %v", got, err)
	}
}
