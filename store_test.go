package main

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestSetGet(t *testing.T) {
	s := NewStore()
	s.Set("name", "pranav")

	got, ok := s.Get("name")
	if !ok || got != "pranav" {
		t.Fatalf("got %q (found=%v), want %q", got, ok, "pranav")
	}

	if _, ok := s.Get("missing"); ok {
		t.Fatal("expected missing key to not be found")
	}
}

func TestDelete(t *testing.T) {
	s := NewStore()
	s.Set("a", "1")

	if !s.Delete("a") {
		t.Fatal("expected Delete to report the key existed")
	}
	if s.Delete("a") {
		t.Fatal("expected second Delete to report the key was gone")
	}
	if s.Exists("a") {
		t.Fatal("key should not exist after delete")
	}
}

func TestIncr(t *testing.T) {
	s := NewStore()

	n, err := s.IncrBy("counter", 1)
	if err != nil || n != 1 {
		t.Fatalf("got %d, %v; want 1, nil", n, err)
	}

	n, _ = s.IncrBy("counter", 5)
	if n != 6 {
		t.Fatalf("got %d, want 6", n)
	}

	s.Set("name", "pranav")
	if _, err := s.IncrBy("name", 1); err != ErrNotInteger {
		t.Fatalf("expected ErrNotInteger, got %v", err)
	}
}

// 100 goroutines each increment 100 times.
// Without the mutex, some increments get lost.
func TestConcurrentIncr(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.IncrBy("counter", 1)
			}
		}()
	}
	wg.Wait() // block until all 100 goroutines finish

	got, _ := s.Get("counter")
	if got != "10000" {
		t.Fatalf("got %s, want 10000", got)
	}
}
func TestExpiry(t *testing.T) {
	s := NewStore()
	s.SetWithTTL("temp", "hi", 50*time.Millisecond)

	if _, ok := s.Get("temp"); !ok {
		t.Fatal("key should exist before it expires")
	}

	time.Sleep(80 * time.Millisecond)

	if _, ok := s.Get("temp"); ok {
		t.Fatal("key should be gone after it expires")
	}
	if s.Exists("temp") {
		t.Fatal("Exists should be false after expiry")
	}
}

func TestTTL(t *testing.T) {
	s := NewStore()

	if got := s.PTTL("missing"); got != ttlMissing {
		t.Fatalf("missing key: got %d, want %d", got, ttlMissing)
	}

	s.Set("a", "1")
	if got := s.PTTL("a"); got != ttlNoExpiry {
		t.Fatalf("no expiry: got %d, want %d", got, ttlNoExpiry)
	}

	s.Expire("a", 10*time.Second)
	if got := s.PTTL("a"); got <= 9000 || got > 10000 {
		t.Fatalf("after EXPIRE 10s: got %d ms, want about 10000", got)
	}

	if !s.Persist("a") {
		t.Fatal("Persist should report it removed a timeout")
	}
	if got := s.PTTL("a"); got != ttlNoExpiry {
		t.Fatalf("after PERSIST: got %d, want %d", got, ttlNoExpiry)
	}
}

func TestSetClearsTTL(t *testing.T) {
	s := NewStore()
	s.SetWithTTL("a", "1", time.Minute)
	s.Set("a", "2") // a plain SET removes the old timeout

	if got := s.PTTL("a"); got != ttlNoExpiry {
		t.Fatalf("got %d, want %d", got, ttlNoExpiry)
	}
}

func TestActiveExpiry(t *testing.T) {
	s := NewStore()
	for i := 0; i < 100; i++ {
		s.SetWithTTL("key:"+strconv.Itoa(i), "v", 20*time.Millisecond)
	}

	done := make(chan struct{})
	defer close(done) // stop the loop when the test ends
	go s.RunExpiryLoop(10*time.Millisecond, done)

	time.Sleep(200 * time.Millisecond)

	// Nobody read these keys, so only active expiry could have removed them.
	if n := s.DBSize(); n != 0 {
		t.Fatalf("expected 0 keys in memory, got %d", n)
	}
}
