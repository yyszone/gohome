// Package registry provides the extension point for future brand drivers.
package registry

import (
	"context"
	"errors"
	"strings"
	"sync"
)

type Device struct {
	ID       string
	Protocol string
	RemoteID string
}

type Command struct {
	Name    string
	Channel *int
}

type Driver interface {
	ID() string
	Execute(context.Context, Device, Command) error
}

type Registry struct {
	mu      sync.RWMutex
	drivers map[string]Driver
}

func New() *Registry { return &Registry{drivers: make(map[string]Driver)} }

func (r *Registry) Register(driver Driver) error {
	if driver == nil || strings.TrimSpace(driver.ID()) == "" {
		return errors.New("品牌驱动和 ID 不能为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.ToLower(strings.TrimSpace(driver.ID()))
	if _, exists := r.drivers[key]; exists {
		return errors.New("品牌驱动已注册：" + key)
	}
	r.drivers[key] = driver
	return nil
}

func (r *Registry) Get(id string) (Driver, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	driver, ok := r.drivers[strings.ToLower(strings.TrimSpace(id))]
	return driver, ok
}
