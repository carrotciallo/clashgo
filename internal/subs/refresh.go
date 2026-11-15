package subs

import (
	"context"
	"errors"
	"github.com/carrotciallo/clashgo/internal/config"
	"os"
	"path/filepath"
)

// Fetch stages network data away from live files. Only the final commit holds
// the registry mutex; deleting/re-adding a subscription invalidates the result.
func (r *Registry) Fetch(ctx context.Context, name, dir string) error {
	candidate := r.Find(name)
	if candidate == nil {
		return ErrStaleSubscription
	}
	if candidate.Local {
		return errors.New("local subscription has no remote URL")
	}
	return r.stage(candidate, dir, func(stage string) error { return Fetch(ctx, candidate, stage) }, ctx)
}
func (r *Registry) Ingest(name string, body []byte, dir string) error {
	candidate := r.Find(name)
	if candidate == nil {
		return ErrStaleSubscription
	}
	return r.stage(candidate, dir, func(stage string) error { return IngestLocal(candidate, body, stage) }, context.Background())
}
func (r *Registry) stage(candidate *Subscription, dir string, prepare func(string) error, ctx context.Context) error {
	stage, err := os.MkdirTemp(dir, ".refresh-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(filepath.Join(stage, candidate.Name+".yaml"))
		_ = os.Remove(filepath.Join(stage, candidate.Name+".plumbing.yaml"))
		_ = os.Remove(stage)
	}()
	if err := prepare(stage); err != nil {
		candidate.LastError = err.Error()
		return errors.Join(err, r.Update(candidate))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := os.ReadFile(filepath.Join(stage, candidate.Name+".yaml"))
	if err != nil {
		return err
	}
	plumbing, err := os.ReadFile(filepath.Join(stage, candidate.Name+".plumbing.yaml"))
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current := false
	for _, s := range r.items {
		if s.Name == candidate.Name && s.revision == candidate.revision {
			current = true
			break
		}
	}
	if !current {
		return ErrStaleSubscription
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	paths := []string{filepath.Join(dir, candidate.Name+".yaml"), filepath.Join(dir, candidate.Name+".plumbing.yaml")}
	old := make([][]byte, 2)
	existed := make([]bool, 2)
	for i, p := range paths {
		old[i], err = os.ReadFile(p)
		existed[i] = err == nil
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := installPool(dir, candidate.Name, payload, plumbing); err != nil {
		return err
	}
	if err := r.updateLocked(candidate); err != nil {
		// Restore the prior pool if persisting metadata fails. No concurrent
		// operation can observe a committed registry version during this rollback.
		for i, p := range paths {
			if existed[i] {
				err = errors.Join(err, config.WriteFileAtomic(p, old[i], 0600))
			} else {
				err = errors.Join(err, os.Remove(p))
			}
		}
		return err
	}
	return nil
}
