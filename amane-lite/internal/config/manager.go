// 配置文件的热读取与原子写回。
package config

import "sync"

// Manager 持有当前配置，读写都经它，保证热更新不撕裂。
type Manager struct {
	mu   sync.RWMutex
	cfg  Config
	path string
}

// NewManager 载入配置；文件缺失时写入默认值。
func NewManager(path, mediaRoot, downloadRoot string) (*Manager, error) {
	cfg, err := Load(path, mediaRoot, downloadRoot)
	if err != nil {
		return nil, err
	}
	return &Manager{cfg: cfg, path: path}, nil
}

// Get 返回配置副本。
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Update 修改配置并写回磁盘；先归一化再校验，失败时保持原值。
func (m *Manager) Update(mutate func(*Config) error) (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.cfg
	if err := mutate(&next); err != nil {
		return m.cfg, err
	}
	next.Normalize()
	if err := next.Validate(); err != nil {
		return m.cfg, err
	}
	if err := Save(m.path, next); err != nil {
		return m.cfg, err
	}
	m.cfg = next
	return next, nil
}

// Path 返回配置文件路径。
func (m *Manager) Path() string { return m.path }
