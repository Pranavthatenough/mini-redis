package main

import (
	"errors"
	"path"
	"strconv"
	"sync"
	"time"
)

var (
	ErrNotInteger = errors.New("ERR value is not an integer or out of range")
	ErrWrongType  = errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")
)

// Special return values for PTTL, matching Redis.
const (
	ttlMissing  = -2 // key does not exist
	ttlNoExpiry = -1 // key exists but never expires
)

// kind says which Redis type an entry holds.
type kind int

const (
	kindString kind = iota // 0
	kindList               // 1
	kindHash               // 2
)

// entry is one value in the store. Only the field matching kind is used.
type entry struct {
	kind kind
	str  string
	list []string
	hash map[string]string
}

// Store is a key-value store that is safe to use from many goroutines.
type Store struct {
	mu      sync.RWMutex
	data    map[string]*entry
	expires map[string]time.Time // key -> deadline
}

func NewStore() *Store {
	return &Store{
		data:    make(map[string]*entry),
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

// remove deletes a key and its expiry. Caller must hold s.mu.Lock().
func (s *Store) remove(key string) {
	delete(s.data, key)
	delete(s.expires, key)
}

// deleteIfExpired removes key if its deadline has passed.
// Caller must hold s.mu.Lock().
func (s *Store) deleteIfExpired(key string) bool {
	if s.isExpired(key) {
		s.remove(key)
		return true
	}
	return false
}

// readEntry returns the live entry for key, or nil if missing or expired.
// Caller must hold at least s.mu.RLock().
func (s *Store) readEntry(key string) *entry {
	if s.isExpired(key) {
		return nil
	}
	return s.data[key]
}

// writeEntry returns the live entry for key, or nil if missing.
// It deletes the key first if it has expired. Caller must hold s.mu.Lock().
func (s *Store) writeEntry(key string) *entry {
	s.deleteIfExpired(key)
	return s.data[key]
}

// ==================== strings ====================

// Set stores a string with no expiry. Like Redis, it overwrites any type
// and clears any old TTL.
func (s *Store) Set(key, value string) {
	s.SetWithTTL(key, value, 0)
}

// SetWithTTL stores a string that expires after ttl (0 = never).
func (s *Store) SetWithTTL(key, value string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = &entry{kind: kindString, str: value}
	if ttl > 0 {
		s.expires[key] = time.Now().Add(ttl)
	} else {
		delete(s.expires, key)
	}
}

// GetString returns the string at key.
// found is false if the key is missing; err is ErrWrongType for non-strings.
func (s *Store) GetString(key string) (value string, found bool, err error) {
	s.mu.RLock()
	e, ok := s.data[key]
	expired := ok && s.isExpired(key)
	if ok && !expired {
		// Copy what we need while still holding the lock.
		if e.kind != kindString {
			err = ErrWrongType
		} else {
			value = e.str
		}
	}
	s.mu.RUnlock()

	if expired {
		// Lazy expiry: upgrade to a write lock and check again.
		s.mu.Lock()
		s.deleteIfExpired(key)
		s.mu.Unlock()
		return "", false, nil
	}
	if !ok || err != nil {
		return "", false, err
	}
	return value, true, nil
}

// Get is a shortcut for GetString that ignores type errors.
// It keeps the earlier tests working.
func (s *Store) Get(key string) (string, bool) {
	value, found, err := s.GetString(key)
	return value, found && err == nil
}

// IncrBy adds delta to the number stored at key. A missing key counts as 0.
// Like Redis, it keeps the key's existing TTL.
func (s *Store) IncrBy(key string, delta int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0
	if e := s.writeEntry(key); e != nil {
		if e.kind != kindString {
			return 0, ErrWrongType
		}
		parsed, err := strconv.Atoi(e.str)
		if err != nil {
			return 0, ErrNotInteger
		}
		n = parsed
	}
	n += delta

	if e := s.data[key]; e != nil {
		e.str = strconv.Itoa(n) // update in place, keeping the TTL
	} else {
		s.data[key] = &entry{kind: kindString, str: strconv.Itoa(n)}
	}
	return n, nil
}

// ==================== lists ====================

// Push adds values to the left (LPUSH) or right (RPUSH) of a list,
// creating it if needed. Returns the new length.
func (s *Store) Push(key string, values []string, left bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e := s.writeEntry(key)
	if e == nil {
		e = &entry{kind: kindList}
		s.data[key] = e
	} else if e.kind != kindList {
		return 0, ErrWrongType
	}

	if left {
		// LPUSH a b c pushes a, then b, then c onto the front,
		// so the list ends up as [c b a], same as Redis.
		for _, v := range values {
			e.list = append([]string{v}, e.list...)
		}
	} else {
		e.list = append(e.list, values...)
	}
	return len(e.list), nil
}

// Pop removes and returns one value from the left (LPOP) or right (RPOP).
// found is false if the key is missing.
func (s *Store) Pop(key string, left bool) (value string, found bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e := s.writeEntry(key)
	if e == nil {
		return "", false, nil
	}
	if e.kind != kindList {
		return "", false, ErrWrongType
	}

	if left {
		value = e.list[0]
		e.list = e.list[1:]
	} else {
		last := len(e.list) - 1
		value = e.list[last]
		e.list = e.list[:last]
	}

	if len(e.list) == 0 {
		s.remove(key) // empty lists don't exist in Redis
	}
	return value, true, nil
}

// LLen returns the length of a list (0 if missing).
func (s *Store) LLen(key string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e := s.readEntry(key)
	if e == nil {
		return 0, nil
	}
	if e.kind != kindList {
		return 0, ErrWrongType
	}
	return len(e.list), nil
}

// LRange returns items from start to stop, inclusive.
// Negative indexes count from the end: -1 is the last item.
func (s *Store) LRange(key string, start, stop int) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e := s.readEntry(key)
	if e == nil {
		return []string{}, nil
	}
	if e.kind != kindList {
		return nil, ErrWrongType
	}

	n := len(e.list)
	if start < 0 {
		start += n
	}
	if stop < 0 {
		stop += n
	}
	if start < 0 {
		start = 0
	}
	if stop >= n {
		stop = n - 1
	}
	if start > stop || start >= n {
		return []string{}, nil
	}

	// Return a COPY. Returning e.list[start:stop+1] directly would share
	// memory with the store, and another goroutine could change it after
	// we unlock.
	result := make([]string, stop-start+1)
	copy(result, e.list[start:stop+1])
	return result, nil
}

// ==================== hashes ====================

// HSet sets field/value pairs in a hash, creating it if needed.
// pairs is [field1, value1, field2, value2, ...].
// Returns how many fields were newly added (not updated).
func (s *Store) HSet(key string, pairs []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e := s.writeEntry(key)
	if e == nil {
		e = &entry{kind: kindHash, hash: make(map[string]string)}
		s.data[key] = e
	} else if e.kind != kindHash {
		return 0, ErrWrongType
	}

	added := 0
	for i := 0; i+1 < len(pairs); i += 2 {
		field, value := pairs[i], pairs[i+1]
		if _, exists := e.hash[field]; !exists {
			added++
		}
		e.hash[field] = value
	}
	return added, nil
}

// HGet returns one field of a hash. found is false if the key or field is missing.
func (s *Store) HGet(key, field string) (value string, found bool, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e := s.readEntry(key)
	if e == nil {
		return "", false, nil
	}
	if e.kind != kindHash {
		return "", false, ErrWrongType
	}
	value, found = e.hash[field]
	return value, found, nil
}

// HDel removes fields from a hash. Returns how many were removed.
func (s *Store) HDel(key string, fields []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e := s.writeEntry(key)
	if e == nil {
		return 0, nil
	}
	if e.kind != kindHash {
		return 0, ErrWrongType
	}

	removed := 0
	for _, field := range fields {
		if _, exists := e.hash[field]; exists {
			delete(e.hash, field)
			removed++
		}
	}
	if len(e.hash) == 0 {
		s.remove(key) // empty hashes don't exist in Redis
	}
	return removed, nil
}

// HGetAll returns [field1, value1, field2, value2, ...] (a copy).
func (s *Store) HGetAll(key string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e := s.readEntry(key)
	if e == nil {
		return []string{}, nil
	}
	if e.kind != kindHash {
		return nil, ErrWrongType
	}

	result := make([]string, 0, len(e.hash)*2)
	for field, value := range e.hash {
		result = append(result, field, value)
	}
	return result, nil
}

// HLen returns the number of fields in a hash (0 if missing).
func (s *Store) HLen(key string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e := s.readEntry(key)
	if e == nil {
		return 0, nil
	}
	if e.kind != kindHash {
		return 0, ErrWrongType
	}
	return len(e.hash), nil
}

// ==================== works on any type ====================

// Type returns "string", "list", "hash", or "none".
func (s *Store) Type(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e := s.readEntry(key)
	if e == nil {
		return "none"
	}
	switch e.kind {
	case kindList:
		return "list"
	case kindHash:
		return "hash"
	default:
		return "string"
	}
}

func (s *Store) Exists(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readEntry(key) != nil
}

// Delete removes a key of any type and reports whether it existed.
func (s *Store) Delete(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writeEntry(key) == nil {
		return false
	}
	s.remove(key)
	return true
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
func (s *Store) Expire(key string, ttl time.Duration) bool {
	return s.ExpireAt(key, time.Now().Add(ttl))
}

// ExpireAt sets an absolute deadline on an existing key.
// A deadline that has already passed deletes the key right away, like Redis.
func (s *Store) ExpireAt(key string, deadline time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writeEntry(key) == nil {
		return false
	}
	if !deadline.After(time.Now()) {
		s.remove(key)
		return true
	}
	s.expires[key] = deadline
	return true
}

// PTTL returns milliseconds left, or ttlMissing / ttlNoExpiry.
func (s *Store) PTTL(key string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.readEntry(key) == nil {
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

	if s.writeEntry(key) == nil {
		return false
	}
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

// ==================== active expiry ====================

// deleteExpiredSample checks up to sampleSize keys that have a deadline
// and deletes the expired ones. Returns how many it deleted.
func (s *Store) deleteExpiredSample(sampleSize int) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	checked, deleted := 0, 0
	for key, deadline := range s.expires {
		if checked == sampleSize {
			break
		}
		checked++
		if now.After(deadline) {
			s.remove(key) // deleting during range is allowed in Go
			deleted++
		}
	}
	return deleted
}

// RunExpiryLoop runs active expiry every interval until done is closed.
func (s *Store) RunExpiryLoop(interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			for s.deleteExpiredSample(20) > 5 {
			}
		}
	}
}
