package main

import (
	"sync"
	"testing"
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
