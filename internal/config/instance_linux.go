package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var ErrInstanceRunning = errors.New("another ClashGo instance owns this configuration directory")

// InstanceLock is held for the entire application lifetime. The lock inode
// lives beside the configuration so changing XDG_RUNTIME_HOME or STATE_HOME
// cannot create a second owner. Never unlink it: waiters must share one inode.
// This is the only file created before the normal store layout is initialized.
type InstanceLock struct {
	file *os.File
	once sync.Once
	err  error
}

func AcquireInstance(configDir string) (*InstanceLock, error) {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(filepath.Join(configDir, ".instance.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("open instance lock: %w", err)
	}
	f := os.NewFile(uintptr(fd), "clashgo-instance")
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrInstanceRunning
		}
		return nil, fmt.Errorf("lock instance: %w", err)
	}
	return &InstanceLock{file: f}, nil
}

func (l *InstanceLock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() { l.err = l.file.Close() })
	return l.err
}
