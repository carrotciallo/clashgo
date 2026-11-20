package core

import (
	"os"
	"path/filepath"
)

// OpenLogFile opens a log file for writing. When sealed is set, the
// file and its directory are made owner-accessible just for the open
// and sealed again (mode 0000) immediately after; the returned
// descriptor is unaffected by the mode change, so logging continues
// while the file at rest is unreadable to everyone.
func OpenLogFile(path string, flag int, sealed bool) (*os.File, error) {
	if !sealed {
		return os.OpenFile(path, flag, 0o600)
	}
	dir := filepath.Dir(path)
	_ = os.Chmod(dir, 0o700)
	_ = os.Chmod(path, 0o600)
	f, err := os.OpenFile(path, flag, 0o600)
	_ = os.Chmod(path, 0o000)
	_ = os.Chmod(dir, 0o000)
	return f, err
}
