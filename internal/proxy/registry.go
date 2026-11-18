package proxy

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	// ErrDriverNotFound is returned when looking up an unregistered proxy engine name.
	ErrDriverNotFound = errors.New("proxy driver not found")

	// ErrInvalidDriver is returned when attempting to register a nil driver or one with an empty name.
	ErrInvalidDriver = errors.New("invalid proxy driver")
)

// Registry maintains a thread-safe map of registered reverse proxy drivers.
type Registry struct {
	mu      sync.RWMutex
	drivers map[string]Driver
}

// NewRegistry creates an empty thread-safe driver registry.
func NewRegistry() *Registry {
	return &Registry{
		drivers: make(map[string]Driver),
	}
}

// Register adds or replaces a driver in the registry keyed by its normalized Name().
func (r *Registry) Register(d Driver) error {
	if d == nil {
		return fmt.Errorf("%w: driver is nil", ErrInvalidDriver)
	}
	name := strings.ToLower(strings.TrimSpace(d.Name()))
	if name == "" {
		return fmt.Errorf("%w: driver name is empty", ErrInvalidDriver)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.drivers == nil {
		r.drivers = make(map[string]Driver)
	}
	r.drivers[name] = d
	return nil
}

// Get retrieves a registered driver by name (case-insensitive).
// Returns ErrDriverNotFound if no driver is registered under that name.
func (r *Registry) Get(name string) (Driver, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.drivers[key]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrDriverNotFound, name)
	}
	return d, nil
}

// List returns all registered drivers sorted alphabetically by Name().
func (r *Registry) List() []Driver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.drivers))
	for k := range r.drivers {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]Driver, 0, len(names))
	for _, k := range names {
		out = append(out, r.drivers[k])
	}
	return out
}

// Unregister removes a registered driver by name (primarily for test isolation).
func (r *Registry) Unregister(name string) {
	key := strings.ToLower(strings.TrimSpace(name))
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.drivers, key)
}

var defaultRegistry = NewRegistry()

// Register adds a driver to the default global proxy driver registry.
func Register(d Driver) error {
	return defaultRegistry.Register(d)
}

// Get retrieves a driver from the default global proxy driver registry.
func Get(name string) (Driver, error) {
	return defaultRegistry.Get(name)
}

// List returns all drivers registered in the default global proxy driver registry,
// sorted alphabetically by Name().
func List() []Driver {
	return defaultRegistry.List()
}
