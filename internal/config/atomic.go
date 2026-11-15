package config

import (
	"errors"
	"os"
	"path/filepath"
)

// WriteFileAtomic uses a private, unique staging file on the same filesystem.
// A failed write/sync/close leaves the destination intact; concurrent writers
// never share a temporary inode. Callers serialize read-modify-write operations.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".clashgo-write-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(perm); err != nil {
		return errors.Join(err, f.Close())
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
