package plugin

import (
	"fmt"
	"sync"
)

type Factory func() any

type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
	metadata  map[string]Metadata
}

func NewRegistry() *Registry {
	return &Registry{
		factories: map[string]Factory{},
		metadata:  map[string]Metadata{},
	}
}

func (r *Registry) Register(meta Metadata, factory Factory) error {
	if meta.Type == "" || meta.Code == "" || meta.Version == "" {
		return fmt.Errorf("plugin metadata requires type, code and version")
	}
	if factory == nil {
		return fmt.Errorf("plugin factory is required")
	}

	key := registryKey(meta.Type, meta.Code, meta.Version)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[key]; exists {
		return fmt.Errorf("plugin already registered: %s", key)
	}
	r.factories[key] = factory
	r.metadata[key] = meta
	return nil
}

func (r *Registry) Get(pluginType Type, code string, version string) (Factory, Metadata, error) {
	key := registryKey(pluginType, code, version)
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, exists := r.factories[key]
	if !exists {
		return nil, Metadata{}, fmt.Errorf("plugin not found: %s", key)
	}
	return factory, r.metadata[key], nil
}

func (r *Registry) List(pluginType Type) []Metadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := []Metadata{}
	for _, meta := range r.metadata {
		if pluginType == "" || meta.Type == pluginType {
			items = append(items, meta)
		}
	}
	return items
}

func registryKey(pluginType Type, code string, version string) string {
	return fmt.Sprintf("%s/%s@%s", pluginType, code, version)
}
