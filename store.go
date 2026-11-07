package main

import (
	"errors"
	"path"
	"strconv"
	"sync"
)

var ErrNotInteger = errors.New("ERR value is not an integer or out of range")

// Store is a key-value store that is safe to use from many goroutines.
type Store struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewStore() *Store {
	return &Store{data: make(map[string]string)}
}

func (s *Store) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

func (s *Store) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.data[key]
	return value, ok
}

// Delete removes a key and reports whether it existed.
func (s *Store) Delete(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[key]
	if ok {
		delete(s.data, key)
	}
	return ok
}

func (s *Store) Exists(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[key]
	return ok
}

// IncrBy adds delta to the number stored at key.
// A missing key counts as 0.
func (s *Store) IncrBy(key string, delta int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0
	if value, ok := s.data[key]; ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return 0, ErrNotInteger
		}
		n = parsed
	}
	n += delta
	s.data[key] = strconv.Itoa(n)
	return n, nil
}

// Keys returns all keys matching a glob pattern like "*" or "user:*".
func (s *Store) Keys(pattern string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := []string{}
	for key := range s.data {
		if matched, _ := path.Match(pattern, key); matched {
			keys = append(keys, key)
		}
	}
	return keys
}
