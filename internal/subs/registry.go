package subs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/carrotciallo/clashgo/internal/config"
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func ValidName(name string) bool { return nameRe.MatchString(name) }

var ErrStaleSubscription = errors.New("subscription changed or was removed during the operation")

type Registry struct {
	mu       sync.RWMutex
	path     string
	items    []Subscription
	revision uint64
}

type registryData struct {
	Subs []Subscription `json:"subscriptions"`
}

func Load(path string) (*Registry, error) {
	r := &Registry{path: path}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	var data registryData
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, fmt.Errorf("subscriptions.json: %w", err)
	}
	seen := map[string]bool{}
	for i := range data.Subs {
		s := &data.Subs[i]
		if !ValidName(s.Name) || seen[s.Name] {
			return nil, fmt.Errorf("invalid or duplicate subscription name")
		}
		seen[s.Name] = true
		r.revision++
		s.revision = r.revision
	}
	r.items = data.Subs
	return r, nil
}

func cloneSubscriptions(items []Subscription) []Subscription {
	out := slices.Clone(items)
	for i := range out {
		out[i].ProxyNames = slices.Clone(out[i].ProxyNames)
	}
	return out
}

// Snapshot and Find never expose the registry's mutable backing storage.
func (r *Registry) Snapshot() []Subscription {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneSubscriptions(r.items)
}
func (r *Registry) Find(name string) *Subscription {
	for _, s := range r.Snapshot() {
		if s.Name == name {
			return &s
		}
	}
	return nil
}

func (r *Registry) persist(items []Subscription) error {
	b, err := json.MarshalIndent(registryData{items}, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(r.path, b, 0600)
}
func (r *Registry) Save() error { r.mu.Lock(); defer r.mu.Unlock(); return r.persist(r.items) }

func (r *Registry) add(s Subscription) error {
	if !ValidName(s.Name) {
		return fmt.Errorf("订阅名只能用字母、数字、下划线和连字符")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.items {
		if v.Name == s.Name {
			return fmt.Errorf("已存在同名订阅 %s", s.Name)
		}
	}
	next := cloneSubscriptions(r.items)
	s.revision = r.revision + 1
	next = append(next, s)
	if err := r.persist(next); err != nil {
		return err
	}
	r.revision++
	r.items = next
	return nil
}
func (r *Registry) Add(name, rawURL string) error {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return fmt.Errorf("订阅地址必须以 http:// 或 https:// 开头")
	}
	return r.add(Subscription{Name: name, URL: rawURL})
}
func (r *Registry) AddLocal(name string) error { return r.add(Subscription{Name: name, Local: true}) }

func (r *Registry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, s := range r.items {
		if s.Name != name {
			continue
		}
		next := cloneSubscriptions(r.items)
		next = append(next[:i], next[i+1:]...)
		if err := r.persist(next); err != nil {
			return err
		}
		r.items = next
		return nil
	}
	return fmt.Errorf("没有订阅 %s", name)
}

// Update commits a detached Find result only if its generation is still current.
// Removal and re-adding a name never resurrect an earlier operation's result.
func (r *Registry) Update(candidate *Subscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.updateLocked(candidate)
}
func (r *Registry) updateLocked(candidate *Subscription) error {
	if candidate == nil {
		return ErrStaleSubscription
	}
	for i, s := range r.items {
		if s.Name != candidate.Name || s.revision != candidate.revision {
			continue
		}
		next := cloneSubscriptions(r.items)
		next[i] = *candidate
		next[i].ProxyNames = slices.Clone(candidate.ProxyNames)
		next[i].revision = r.revision + 1
		if err := r.persist(next); err != nil {
			return err
		}
		r.revision++
		r.items = next
		candidate.revision = next[i].revision
		return nil
	}
	return ErrStaleSubscription
}
