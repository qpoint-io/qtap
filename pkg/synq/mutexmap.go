package synq

import "sync"

// MutexMap is a map of keyed mutexes that is safe for concurrent use.
type MutexMap[T comparable] struct {
	m *Map[T, *sync.Mutex]
}

func NewMutexMap[T comparable]() *MutexMap[T] {
	return &MutexMap[T]{
		m: NewMap[T, *sync.Mutex](),
	}
}

func (m *MutexMap[T]) Get(key T) *sync.Mutex {
	mutex, _ := m.m.LoadOrInsert(key, &sync.Mutex{})
	return mutex
}
