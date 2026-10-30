package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// WorkspaceConfig is the project configuration stored at jxscout/config.json.
type WorkspaceConfig struct {
	Version int      `json:"version"`
	Project string   `json:"project"`
	Scope   []string `json:"scope"`
}

// DiscoverWorkspace searches upward from start for a workspace config. It also
// recognizes start itself as the jxscout output root when config.json is there.
func DiscoverWorkspace(start string) (root string, config WorkspaceConfig, found bool, err error) {
	if start == "" {
		start, err = os.Getwd()
		if err != nil {
			return "", WorkspaceConfig{}, false, fmt.Errorf("get current directory: %w", err)
		}
	}
	start, err = filepath.Abs(start)
	if err != nil {
		return "", WorkspaceConfig{}, false, fmt.Errorf("resolve workspace search path: %w", err)
	}
	info, err := os.Stat(start)
	if err != nil {
		return "", WorkspaceConfig{}, false, fmt.Errorf("stat workspace search path: %w", err)
	}
	if !info.IsDir() {
		start = filepath.Dir(start)
	}

	for dir := start; ; dir = filepath.Dir(dir) {
		candidates := []string{filepath.Join(dir, "jxscout", "config.json")}
		if filepath.Base(dir) == "jxscout" {
			candidates = append([]string{filepath.Join(dir, "config.json")}, candidates...)
		}
		for _, candidate := range candidates {
			if info, statErr := os.Stat(filepath.Dir(candidate)); statErr == nil && !info.IsDir() {
				continue
			}
			cfg, exists, readErr := readWorkspaceConfig(candidate)
			if readErr != nil {
				return "", WorkspaceConfig{}, false, readErr
			}
			if exists {
				return filepath.Dir(candidate), cfg, true, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return "", WorkspaceConfig{}, false, nil
}

func readWorkspaceConfig(path string) (WorkspaceConfig, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return WorkspaceConfig{}, false, nil
	}
	if err != nil {
		return WorkspaceConfig{}, false, fmt.Errorf("read workspace config %s: %w", path, err)
	}
	var config WorkspaceConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return WorkspaceConfig{}, false, fmt.Errorf("decode workspace config %s: %w", path, err)
	}
	if config.Version != 1 || config.Project == "" {
		return WorkspaceConfig{}, false, nil
	}
	if config.Scope == nil {
		config.Scope = []string{}
	}
	return config, true, nil
}
