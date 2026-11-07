package main

import (
	"errors"
	"path"
	"strconv"
	"sync"
	"time"
)

var ErrNotInteger = errors.New("ERR value is not an integer or out of range")

// Special return values for PTTL, matching Redis.
const (
	ttlMissing  = -2 // key does not exist
	ttlNoExpiry = -1 // key exists but never expires
)

// Store is a key-value store that is safe to use from many goroutines.
type Store struct {
	mu      sync.RWMutex
	data    map[string]string
	expires map[string]time.Time // key -> deadline
}

func NewStore() *Store {
	return &Store{
		data:    make(map[string]string),
		expires: make(map[string]time.Time),
	}
}

// ---- internal helpers (caller must already hold the lock) ----

// isExpired reports whether key has a deadline that has passed.
// Caller must hold at least s.mu.RLock().
func (s *Store) isExpired(key string) bool {
	deadline, ok := s.expires[key]
	return ok && time.Now().After(deadline)
}

// deleteIfExpired removes key if its deadline has passed.
// Caller must hold s.mu.Lock().
func (s *Store) deleteIfExpired(key string) bool {
	if s.isExpired(key) {
		delete(s.data, key)
		delete(s.expires, key)
		return true
	}
	return false
}

// ---- public methods ----

// Set stores a value with no expiry. Like Redis, it clears any old TTL.
func (s *Store) Set(key, value string) {
	s.SetWithTTL(key, value, 0)
}

// SetWithTTL stores a value that expires after ttl (0 = never).
func (s *Store) SetWithTTL(key, value string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = value
	if ttl > 0 {
		s.expires[key] = time.Now().Add(ttl)
	} else {
		delete(s.expires, key)
	}
}

func (s *Store) Get(key string) (string, bool) {
	s.mu.RLock()
	value, ok := s.data[key]
	expired := ok && s.isExpired(key)
	s.mu.RUnlock()

	if expired {
		// Lazy expiry: we need a write lock to delete.
		// deleteIfExpired checks again, because another client may have
		// SET this key in the moment between RUnlock and Lock.
		s.mu.Lock()
		s.deleteIfExpired(key)
		s.mu.Unlock()
		return "", false
	}
	return value, ok
}

func (s *Store) Exists(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[key]
	return ok && !s.isExpired(key)
}

// Delete removes a key and reports whether it existed.
func (s *Store) Delete(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.deleteIfExpired(key) // an expired key counts as already gone
	_, ok := s.data[key]
	if ok {
		delete(s.data, key)
		delete(s.expires, key)
	}
	return ok
}

// IncrBy adds delta to the number stored at key. A missing key counts as 0.
// Like Redis, it keeps the key's existing TTL.
func (s *Store) IncrBy(key string, delta int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.deleteIfExpired(key)

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

// Keys returns all live keys matching a glob pattern like "*" or "user:*".
func (s *Store) Keys(pattern string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := []string{}
	for key := range s.data {
		if s.isExpired(key) {
			continue
		}
		if matched, _ := path.Match(pattern, key); matched {
			keys = append(keys, key)
		}
	}
	return keys
}

// Expire sets a timeout on an existing key. Returns false if the key is missing.
// A ttl of zero or less deletes the key right away, like Redis.
func (s *Store) Expire(key string, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.deleteIfExpired(key)
	if _, ok := s.data[key]; !ok {
		return false
	}
	if ttl <= 0 {
		delete(s.data, key)
		delete(s.expires, key)
		return true
	}
	s.expires[key] = time.Now().Add(ttl)
	return true
}

// PTTL returns milliseconds left, or ttlMissing / ttlNoExpiry.
func (s *Store) PTTL(key string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.data[key]; !ok || s.isExpired(key) {
		return ttlMissing
	}
	deadline, ok := s.expires[key]
	if !ok {
		return ttlNoExpiry
	}
	return time.Until(deadline).Milliseconds()
}

// Persist removes a key's timeout. Returns true if a timeout was removed.
func (s *Store) Persist(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.deleteIfExpired(key)
	if _, ok := s.expires[key]; ok {
		delete(s.expires, key)
		return true
	}
	return false
}

// DBSize returns how many keys are in memory, including expired keys
// that active expiry hasn't removed yet (real Redis behaves the same way).
func (s *Store) DBSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}

// ---- active expiry ----

// deleteExpiredSample checks up to sampleSize keys that have a deadline
// and deletes the expired ones. Returns how many it deleted.
func (s *Store) deleteExpiredSample(sampleSize int) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	checked, deleted := 0, 0
	// Go randomizes map iteration order, so this is a random sample.
	for key, deadline := range s.expires {
		if checked == sampleSize {
			break
		}
		checked++
		if now.After(deadline) {
			delete(s.data, key)
			delete(s.expires, key) // deleting during range is allowed in Go
			deleted++
		}
	}
	return deleted
}

// RunExpiryLoop runs active expiry every interval until done is closed.
// Start it with: go store.RunExpiryLoop(...)
func (s *Store) RunExpiryLoop(interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			// Like real Redis: if more than 25% of the sample was expired,
			// there are probably more, so sample again straight away.
			for s.deleteExpiredSample(20) > 5 {
			}
		}
	}
}
