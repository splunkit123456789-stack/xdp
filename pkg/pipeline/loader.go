package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadFile(path string) (Pipeline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Pipeline{}, fmt.Errorf("read pipeline file %s: %w", path, err)
	}
	var pipe Pipeline
	if err := yaml.Unmarshal(data, &pipe); err != nil {
		return Pipeline{}, fmt.Errorf("parse pipeline file %s: %w", path, err)
	}
	if err := pipe.Validate(); err != nil {
		return Pipeline{}, fmt.Errorf("validate pipeline file %s: %w", path, err)
	}
	return pipe, nil
}

func LoadDir(dir string) ([]Pipeline, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read pipeline dir %s: %w", dir, err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		ext := filepath.Ext(name)
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no pipeline yaml files found in %s", dir)
	}
	sort.Strings(paths)
	pipes := make([]Pipeline, 0, len(paths))
	for _, path := range paths {
		pipe, err := LoadFile(path)
		if err != nil {
			return nil, err
		}
		pipes = append(pipes, pipe)
	}
	return pipes, nil
}

func LoadPath(path string) ([]Pipeline, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("pipeline path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat pipeline path %s: %w", path, err)
	}
	if info.IsDir() {
		return LoadDir(path)
	}
	pipe, err := LoadFile(path)
	if err != nil {
		return nil, err
	}
	return []Pipeline{pipe}, nil
}
