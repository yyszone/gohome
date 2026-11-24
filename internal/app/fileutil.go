package app

import (
	"os"
	"path/filepath"

	"gohome/internal/features/storage"
)

func mkdirAll(p string) error                                { return os.MkdirAll(p, 0700) }
func readFileOS(p string) ([]byte, error)                    { return os.ReadFile(p) }
func writeFileOS(p string, b []byte, mode os.FileMode) error { return os.WriteFile(p, b, mode) }
func writeFileAtomicOS(p string, b []byte, mode os.FileMode) error {
	return storage.WriteAtomic(p, b, mode)
}
func isNotExist(e error) bool { return os.IsNotExist(e) }
func errFileNotExist() error  { return os.ErrNotExist }

var _ = filepath.Separator
