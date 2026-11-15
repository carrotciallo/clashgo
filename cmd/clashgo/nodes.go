package main

import (
	"github.com/carrotciallo/clashgo/internal/core"
	"strings"
	"sync"
)

func filterNodes(nodes []core.Proxy, query string) []core.Proxy {
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]core.Proxy, 0, len(nodes))
	for _, p := range nodes {
		if strings.HasPrefix(p.Name, "__") {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(p.Name+" "+p.Type), query) {
			out = append(out, p)
		}
	}
	return out
}

// Test only this pool, with bounded parallelism and provider-qualified routes.
func (a *App) testProviderDelay(provider string) {
	a.mu.Lock()
	nodes := filterNodes(a.providers[provider], "")
	a.mu.Unlock()
	go func() {
		a.setBusy(true, "测速 "+provider+"…")
		defer a.setBusy(false, "")
		results := make(map[string]int, len(nodes))
		var mu sync.Mutex
		var wg sync.WaitGroup
		slots := make(chan struct{}, 4)
		for _, node := range nodes {
			slots <- struct{}{}
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				defer func() { <-slots }()
				delay, err := a.cli.ProviderDelay(provider, name, "https://www.gstatic.com/generate_204", 5000)
				if err != nil {
					delay = 0
				}
				mu.Lock()
				results[name] = delay
				mu.Unlock()
			}(node.Name)
		}
		wg.Wait()
		a.mu.Lock()
		if a.delays == nil {
			a.delays = map[string]map[string]int{}
		}
		if a.delays["节点选择"] == nil {
			a.delays["节点选择"] = map[string]int{}
		}
		for name, delay := range results {
			a.delays["节点选择"][name] = delay
		}
		a.mu.Unlock()
	}()
}
