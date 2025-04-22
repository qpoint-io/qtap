package tags

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
)

type List interface {
	Add(string, ...string)
	AddString(string) error
	List() []string
	Clone() List
	Merge(List)
	Map() map[string][]string
}

type tags struct {
	mu   sync.RWMutex
	data map[string][]string
}

func New() *tags {
	return &tags{
		data: make(map[string][]string),
	}
}

func FromValues(kv map[string]string) List {
	t := New()
	for k, v := range kv {
		t.add(k, v)
	}
	return t
}

func FromMultiValues(kv map[string][]string) List {
	t := New()

	for _, k := range slices.Sorted(maps.Keys(kv)) {
		t.add(k, kv[k]...)
	}
	return t
}

// Add associates a value with a key in the tags collection. Both key and value are normalized by:
// - Trimming whitespace
// - Converting to lowercase
// - Replacing spaces with hyphens
// The function silently returns without adding if either:
// - Key or value is empty after trimming
// - Key or value doesn't start and end with alphanumeric characters
func (t *tags) Add(key string, values ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.add(key, values...)
}

func (t *tags) add(key string, values ...string) {
	key = format(key)

	if key == "" {
		return
	}

	for _, v := range values {
		v = format(v)
		if v == "" {
			continue
		}
		t.data[key] = append(t.data[key], v)
	}
}

func (t *tags) List() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var list []string
	for key, values := range t.data {
		for _, value := range values {
			list = append(list, fmt.Sprintf("%s:%s", key, value))
		}
	}
	return list
}

func (t *tags) AddString(s string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.addString(s)
}

func (t *tags) addString(s string) error {
	key, value, found := strings.Cut(s, ":")
	if !found {
		return errors.New("invalid tag format: must be in the format key:value")
	}
	t.add(key, value)
	return nil
}

// Clone returns a deep copy of the tags collection
func (t *tags) Clone() List {
	t.mu.RLock()
	defer t.mu.RUnlock()

	clone := New()
	for key, values := range t.data {
		clone.data[key] = append([]string{}, values...)
	}
	return clone
}

// Merge combines the tags from another List into this one
func (t *tags) Merge(other List) {
	if other == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	for _, tag := range other.List() {
		_ = t.addString(tag)
	}
}

func (t *tags) Map() map[string][]string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	m := make(map[string][]string, len(t.data))
	for k, v := range t.data {
		m[k] = append(([]string)(nil), v...)
	}
	return m
}

func format(str string) string {
	// Make lowercase
	str = strings.ToLower(str)

	// Ensure starts and ends with alphanumeric. This will also trim whitespace.
	str = strings.TrimFunc(str, func(r rune) bool {
		return !isAlphanumeric(r)
	})

	// Replace spaces with hyphens
	str = strings.ReplaceAll(str, " ", "-")

	return str
}

func isAlphanumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}
