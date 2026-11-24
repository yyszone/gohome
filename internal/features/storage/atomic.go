// Package storage defines filesystem safety helpers shared by feature modules.
package storage

import (
	"os"
)

// WriteAtomic writes to a sibling temporary file and renames it into place.
// The temporary file is always created with the requested restrictive mode.
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
