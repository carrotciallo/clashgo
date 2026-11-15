package config

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAtomicWriteConcurrentCompleteRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data")
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := WriteFileAtomic(path, bytes.Repeat([]byte{byte('a' + i)}, 8192), 0600); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 8192 || !bytes.Equal(b, bytes.Repeat(b[:1], 8192)) {
		t.Fatal("torn record")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary files leaked")
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatal("permissions")
	}
}
