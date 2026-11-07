package main

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// startServer runs a real server on a random free port.
// stop shuts it down gracefully; it also runs automatically when the test ends.
func startServer(t *testing.T, aofPath string) (addr string, stop func()) {
	t.Helper()

	// Port 0 tells the OS to pick any free port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx, listener, aofPath) }()

	var once sync.Once
	stop = func() {
		once.Do(func() { // safe to call stop more than once
			cancel()
			if err := <-errCh; err != nil {
				t.Errorf("server returned an error: %v", err)
			}
		})
	}
	t.Cleanup(stop)
	return listener.Addr().String(), stop
}

type testClient struct {
	conn net.Conn
	resp *Resp
}

func dial(t *testing.T, addr string) *testClient {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return &testClient{conn: conn, resp: NewResp(conn)}
}

// send writes one command and reads its reply.
func (c *testClient) send(parts ...string) (Value, error) {
	if _, err := c.conn.Write(commandValue(parts...).Marshal()); err != nil {
		return Value{}, err
	}
	c.conn.SetReadDeadline(time.Now().Add(2 * time.Second)) // never hang forever
	return c.resp.Read()
}

// must is send for the main test goroutine: it stops the test on error.
func (c *testClient) must(t *testing.T, parts ...string) Value {
	t.Helper()
	v, err := c.send(parts...)
	if err != nil {
		t.Fatalf("%v: %v", parts, err)
	}
	return v
}

func TestServerEndToEnd(t *testing.T) {
	addr, _ := startServer(t, filepath.Join(t.TempDir(), "test.aof"))
	c := dial(t, addr)

	tests := []struct {
		cmd  []string
		want Value
	}{
		{[]string{"PING"}, simpleString("PONG")},
		{[]string{"SET", "name", "pranav"}, simpleString("OK")},
		{[]string{"GET", "name"}, bulkString("pranav")},
		{[]string{"GET", "missing"}, nullValue()},
		{[]string{"INCR", "n"}, integer(1)},
		{[]string{"RPUSH", "l", "a", "b"}, integer(2)},
		{[]string{"LPOP", "l"}, bulkString("a")},
		{[]string{"GET", "l"}, errorValue(ErrWrongType.Error())},
		{[]string{"NOPE"}, errorValue("ERR unknown command 'NOPE'")},
	}

	for _, tt := range tests {
		got := c.must(t, tt.cmd...)
		// Comparing the RESP bytes checks the type and the value together.
		if string(got.Marshal()) != string(tt.want.Marshal()) {
			t.Errorf("%v: got %q, want %q", tt.cmd, got.Marshal(), tt.want.Marshal())
		}
	}
}

func TestServerManyClients(t *testing.T) {
	addr, _ := startServer(t, filepath.Join(t.TempDir(), "test.aof"))

	const numClients, incrsEach = 20, 50

	// Connect everyone first, in the main test goroutine.
	clients := make([]*testClient, numClients)
	for i := range clients {
		clients[i] = dial(t, addr)
	}

	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrsEach; j++ {
				if _, err := c.send("INCR", "counter"); err != nil {
					t.Errorf("INCR failed: %v", err) // Errorf is safe in goroutines
					return
				}
			}
		}()
	}
	wg.Wait()

	got := clients[0].must(t, "GET", "counter")
	if want := strconv.Itoa(numClients * incrsEach); got.str != want {
		t.Fatalf("counter = %q, want %q", got.str, want)
	}
}

func TestServerRestartKeepsData(t *testing.T) {
	aofPath := filepath.Join(t.TempDir(), "test.aof")

	addr, stop := startServer(t, aofPath)
	c := dial(t, addr)
	c.must(t, "SET", "name", "pranav")
	c.must(t, "HSET", "user", "city", "bengaluru")

	// c is still connected and idle. A working graceful shutdown must
	// disconnect it instead of waiting forever.
	stop()

	addr2, _ := startServer(t, aofPath)
	c2 := dial(t, addr2)

	if got := c2.must(t, "GET", "name"); got.str != "pranav" {
		t.Errorf("after restart, name = %q, want pranav", got.str)
	}
	if got := c2.must(t, "HGET", "user", "city"); got.str != "bengaluru" {
		t.Errorf("after restart, city = %q, want bengaluru", got.str)
	}
}
