package subs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/carrotciallo/clashgo/internal/config"
	"gopkg.in/yaml.v3"
)

const maxPayloadSize = 32 << 20

// Decode into values as well as nodes: yaml.Node alone does not reject duplicate keys.
func decodePool(body []byte) ([]map[string]any, error) {
	if len(body) > maxPayloadSize {
		return nil, fmt.Errorf("订阅超过 32 MiB")
	}
	var doc map[string]any
	dec := yaml.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("订阅 YAML 无效")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("订阅必须是单一 YAML 文档")
	}
	items, ok := doc["proxies"].([]any)
	if !ok {
		return nil, fmt.Errorf("订阅缺少 proxies 列表")
	}
	seen := map[string]bool{}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		node, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("节点必须是映射")
		}
		name, _ := node["name"].(string)
		kind, _ := node["type"].(string)
		if name == "" || kind == "" || seen[name] {
			return nil, fmt.Errorf("节点名称或类型缺失，或名称重复")
		}
		seen[name] = true
		out = append(out, node)
	}
	return out, nil
}

func preparePayload(body []byte) (payload, plumbing []byte, names []string, split bool, err error) {
	nodes, err := decodePool(body)
	if err != nil {
		return nil, nil, nil, false, err
	}
	original := nodes
	if p, d, _, ok := SplitFullConfig(body); ok {
		payload, plumbing, split = p, d, true
		nodes, err = decodePool(payload)
		if err != nil {
			return nil, nil, nil, false, err
		}
	}
	kept := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		name := node["name"].(string)
		kind := node["type"].(string)
		if config.IsNoticeName(name) || strings.HasPrefix(name, "__") || strings.EqualFold(kind, "direct") || strings.EqualFold(kind, "reject") {
			continue
		}
		kept = append(kept, node)
		names = append(names, name)
	}
	if len(kept) == 0 {
		return nil, nil, nil, false, fmt.Errorf("订阅没有可用节点，保留原节点池")
	}
	// Resolve dialers from the original pool in both full-config families.
	byName := map[string]map[string]any{}
	for _, node := range original {
		byName[node["name"].(string)] = node
	}
	visited, visiting := map[string]bool{}, map[string]bool{}
	var dependencies []map[string]any
	var walk func(map[string]any) error
	walk = func(node map[string]any) error {
		dp, _ := node["dialer-proxy"].(string)
		if dp == "" || dp == "DIRECT" {
			return nil
		}
		if visiting[dp] {
			return fmt.Errorf("订阅 dialer 链存在循环")
		}
		if visited[dp] {
			return nil
		}
		target := byName[dp]
		if target == nil {
			return fmt.Errorf("订阅 dialer 引用缺失或指向策略组")
		}
		visiting[dp] = true
		if err := walk(target); err != nil {
			return err
		}
		delete(visiting, dp)
		visited[dp] = true
		if split {
			target["udp"] = true
		}
		dependencies = append(dependencies, target)
		return nil
	}
	for _, node := range kept {
		if err := walk(node); err != nil {
			return nil, nil, nil, false, err
		}
	}
	if len(dependencies) > 0 {
		split = true
	}
	plumbing, err = yaml.Marshal(map[string]any{"proxies": dependencies})
	if err != nil {
		return nil, nil, nil, false, err
	}
	// Canonical data boundary: no DNS, rules, groups or arbitrary top-level fields survive.
	payload, err = yaml.Marshal(map[string]any{"proxies": kept})
	if err != nil {
		return nil, nil, nil, false, err
	}
	return payload, plumbing, names, split, nil
}

// Stage both artifacts before installation. Roll back ordinary filesystem errors;
// this is not a crash-atomic two-file transaction (see AUDIT.md).
func installPool(dir, name string, payload, plumbing []byte) error {
	paths := []string{filepath.Join(dir, name+".plumbing.yaml"), filepath.Join(dir, name+".yaml")}
	data := [][]byte{plumbing, payload}
	if len(plumbing) == 0 {
		data[0] = []byte("proxies: []\n")
	}
	old := make([][]byte, 2)
	existed := make([]bool, 2)
	temps := make([]string, 2)
	defer func() {
		for _, p := range temps {
			if p != "" {
				_ = os.Remove(p)
			}
		}
	}()
	for i, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		old[i], existed[i] = b, err == nil
		f, err := os.CreateTemp(dir, ".pool-*")
		if err != nil {
			return err
		}
		temps[i] = f.Name()
		_, writeErr := f.Write(data[i])
		closeErr := f.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return err
		}
	}
	for i, path := range paths {
		if err := os.Rename(temps[i], path); err != nil {
			for j := 0; j < i; j++ {
				if existed[j] {
					err = errors.Join(err, os.WriteFile(paths[j], old[j], 0o600))
				} else {
					err = errors.Join(err, os.Remove(paths[j]))
				}
			}
			return err
		}
	}
	return nil
}
