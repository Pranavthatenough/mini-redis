package main

import (
	"strconv"
	"strings"
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
func TestListPushPop(t *testing.T) {
	s := NewStore()

	s.Push("tasks", []string{"a", "b", "c"}, false) // RPUSH -> [a b c]
	s.Push("tasks", []string{"x", "y"}, true)       // LPUSH -> [y x a b c]

	items, _ := s.LRange("tasks", 0, -1)
	want := []string{"y", "x", "a", "b", "c"}
	if strings.Join(items, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", items, want)
	}

	if v, _, _ := s.Pop("tasks", true); v != "y" {
		t.Errorf("LPOP got %q, want y", v)
	}
	if v, _, _ := s.Pop("tasks", false); v != "c" {
		t.Errorf("RPOP got %q, want c", v)
	}
	if n, _ := s.LLen("tasks"); n != 3 {
		t.Errorf("LLEN got %d, want 3", n)
	}
}

func TestLRangeIndexes(t *testing.T) {
	s := NewStore()
	s.Push("l", []string{"a", "b", "c", "d", "e"}, false)

	tests := []struct {
		start, stop int
		want        string
	}{
		{0, -1, "a,b,c,d,e"},
		{1, 2, "b,c"},
		{-2, -1, "d,e"},
		{0, 100, "a,b,c,d,e"}, // stop past the end is clamped
		{3, 1, ""},            // start after stop is empty
		{10, 20, ""},          // start past the end is empty
	}
	for _, tt := range tests {
		items, _ := s.LRange("l", tt.start, tt.stop)
		if got := strings.Join(items, ","); got != tt.want {
			t.Errorf("LRANGE %d %d = %q, want %q", tt.start, tt.stop, got, tt.want)
		}
	}
}

func TestEmptyListIsDeleted(t *testing.T) {
	s := NewStore()
	s.Push("l", []string{"only"}, false)
	s.Pop("l", true)

	if s.Exists("l") {
		t.Fatal("a list with no items should not exist")
	}
}

func TestHash(t *testing.T) {
	s := NewStore()

	added, _ := s.HSet("user", []string{"name", "pranav", "city", "bengaluru"})
	if added != 2 {
		t.Fatalf("HSET added %d, want 2", added)
	}
	added, _ = s.HSet("user", []string{"city", "delhi"}) // update, not new
	if added != 0 {
		t.Fatalf("HSET on existing field added %d, want 0", added)
	}

	if v, _, _ := s.HGet("user", "city"); v != "delhi" {
		t.Errorf("HGET city = %q, want delhi", v)
	}
	if n, _ := s.HLen("user"); n != 2 {
		t.Errorf("HLEN = %d, want 2", n)
	}

	removed, _ := s.HDel("user", []string{"name", "city", "missing"})
	if removed != 2 {
		t.Errorf("HDEL removed %d, want 2", removed)
	}
	if s.Exists("user") {
		t.Error("a hash with no fields should not exist")
	}
}

func TestWrongType(t *testing.T) {
	s := NewStore()
	s.Push("mylist", []string{"a"}, false)
	s.Set("mystring", "hi")

	if _, _, err := s.GetString("mylist"); err != ErrWrongType {
		t.Errorf("GET on a list: got %v, want ErrWrongType", err)
	}
	if _, err := s.Push("mystring", []string{"x"}, true); err != ErrWrongType {
		t.Errorf("LPUSH on a string: got %v, want ErrWrongType", err)
	}
	if _, err := s.IncrBy("mylist", 1); err != ErrWrongType {
		t.Errorf("INCR on a list: got %v, want ErrWrongType", err)
	}

	s.Set("mylist", "now a string") // SET overwrites any type
	if got := s.Type("mylist"); got != "string" {
		t.Errorf("TYPE after SET = %q, want string", got)
	}
}
