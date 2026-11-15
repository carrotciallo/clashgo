package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FileChange is a complete replacement; transactions must be serialized by the
// caller. Multi-file rollback handles ordinary errors, not power loss.
type FileChange struct {
	Path string
	Data []byte
}

func CommitFiles(changes []FileChange, validate func(map[string]string) error, activate, rollback func() error) error {
	type saved struct {
		path, temp string
		data       []byte
		exists     bool
		mode       os.FileMode
	}
	old := make([]saved, 0, len(changes))
	paths := map[string]string{}
	defer func() {
		for _, s := range old {
			_ = os.Remove(s.temp)
		}
	}()
	for _, c := range changes {
		if _, ok := paths[c.Path]; ok {
			return fmt.Errorf("duplicate transaction path")
		}
		s := saved{path: c.Path, mode: 0600}
		fi, err := os.Lstat(c.Path)
		if err == nil {
			if !fi.Mode().IsRegular() {
				return fmt.Errorf("transaction target is not a regular file")
			}
			s.mode = fi.Mode().Perm()
			s.exists = true
			s.data, err = os.ReadFile(c.Path)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		f, err := os.CreateTemp(filepath.Dir(c.Path), ".transaction-*")
		if err != nil {
			return err
		}
		s.temp = f.Name()
		old = append(old, s)
		paths[c.Path] = s.temp
		_, err = f.Write(c.Data)
		if err == nil {
			err = f.Sync()
		}
		err = errors.Join(err, f.Close())
		if err != nil {
			return err
		}
	}
	if validate != nil {
		if err := validate(paths); err != nil {
			return err
		}
	}
	restore := func(n int) error {
		var errs []error
		for i := n - 1; i >= 0; i-- {
			s := old[i]
			var err error
			if s.exists {
				err = WriteFileAtomic(s.path, s.data, s.mode)
			} else {
				err = os.Remove(s.path)
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("restore %s: %w", s.path, err))
			}
		}
		return errors.Join(errs...)
	}
	for i, s := range old {
		if err := os.Rename(s.temp, s.path); err != nil {
			return errors.Join(err, restore(i))
		}
	}
	if activate != nil {
		if err := activate(); err != nil {
			restoreErr := restore(len(old))
			var rollbackErr error
			if rollback != nil {
				rollbackErr = rollback()
			}
			return errors.Join(fmt.Errorf("activation failed: %w", err), restoreErr, rollbackErr)
		}
	}
	return nil
}
