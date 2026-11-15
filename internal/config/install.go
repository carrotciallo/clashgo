package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// InstallRuntime validates a private candidate before replacing the live file.
// Activation failures restore both the disk snapshot and the running core.
// Callers serialize transactions; the callbacks must not mutate the master.
func InstallRuntime(path, content string, validate, activate func(string) error) error {
	old, readErr := os.ReadFile(path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	stage := func(data []byte) (string, error) {
		f, err := os.CreateTemp(filepath.Dir(path), ".runtime-*")
		if err != nil {
			return "", err
		}
		name := f.Name()
		_, err = f.Write(data)
		if err == nil {
			err = f.Sync()
		}
		err = errors.Join(err, f.Close())
		if err != nil {
			_ = os.Remove(name)
			return "", err
		}
		return name, nil
	}
	candidate, err := stage([]byte(content))
	if err != nil {
		return err
	}
	defer os.Remove(candidate)
	if err = validate(candidate); err != nil {
		return err
	}
	if err = os.Rename(candidate, path); err != nil {
		return err
	}
	if activate == nil {
		return nil
	}
	if err = activate(path); err == nil {
		return nil
	}
	activationErr := err
	if errors.Is(readErr, os.ErrNotExist) {
		return errors.Join(activationErr, os.Remove(path))
	}
	backup, err := stage(old)
	if err != nil {
		return errors.Join(activationErr, fmt.Errorf("stage rollback: %w", err))
	}
	defer os.Remove(backup)
	if err = os.Rename(backup, path); err != nil {
		return errors.Join(activationErr, fmt.Errorf("restore runtime: %w", err))
	}
	if err = activate(path); err != nil {
		return errors.Join(activationErr, fmt.Errorf("reload restored runtime: %w", err))
	}
	return fmt.Errorf("activation failed; previous runtime restored: %w", activationErr)
}
