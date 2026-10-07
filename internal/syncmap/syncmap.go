package syncmap

import "sync"

type Map[K comparable, V any] struct {
	mu   sync.RWMutex
	data map[K]V
}

func NewMap[K comparable, V any]() Map[K, V] {
	return Map[K, V]{
		data: make(map[K]V),
	}
}

func (m *Map[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, ok := m.data[key]
	return value, ok
}

func (m *Map[K, V]) Set(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data[key] = value
}

func (m *Map[K, V]) Delete(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.data, key)
}
