package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Server holds everything shared by all client connections.
type Server struct {
	store   *Store
	aof     *AOF
	writeMu sync.Mutex // keeps the AOF in the same order as the store

	connMu  sync.Mutex
	conns   map[net.Conn]struct{} // open client connections
	clients sync.WaitGroup        // running handleConn goroutines
}

func main() {
	port := flag.Int("port", 6380, "TCP port to listen on")
	aofPath := flag.String("aof", "appendonly.aof", "path to the append-only file")
	flag.Parse()

	// ctx is cancelled on Ctrl+C (SIGINT) or when the OS asks us to stop (SIGTERM).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatal("could not start server: ", err)
	}

	if err := run(ctx, listener, *aofPath); err != nil {
		log.Fatal(err)
	}
}

// run serves clients on listener until ctx is cancelled, then shuts down gracefully.
func run(ctx context.Context, listener net.Listener, aofPath string) error {
	defer listener.Close()

	store := NewStore()

	aof, err := NewAOF(aofPath)
	if err != nil {
		return fmt.Errorf("open AOF: %w", err)
	}

	// Rebuild memory from disk BEFORE accepting any clients.
	count, err := aof.Replay(func(v Value) {
		if v.typ != "array" || len(v.array) == 0 {
			return
		}
		command := strings.ToUpper(v.array[0].str)
		handleCommand(store, command, v.array[1:])
	})
	if err != nil {
		aof.Close()
		return fmt.Errorf("replay AOF: %w", err)
	}
	log.Printf("loaded %d commands from %s", count, aofPath)

	server := &Server{
		store: store,
		aof:   aof,
		conns: make(map[net.Conn]struct{}),
	}

	// Background goroutines, tracked so shutdown can wait for them.
	done := make(chan struct{})
	var background sync.WaitGroup
	background.Add(2)
	go func() {
		defer background.Done()
		store.RunExpiryLoop(100*time.Millisecond, done)
	}()
	go func() {
		defer background.Done()
		aof.RunSyncLoop(time.Second, done)
	}()

	// When ctx is cancelled, close the listener. That makes Accept return
	// an error, which breaks the loop below.
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	log.Println("mini-redis listening on", listener.Addr())
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break // shutting down
			}
			log.Println("accept error:", err)
			continue
		}
		// Track the connection HERE, before starting the goroutine,
		// so closeAllConns can never miss it.
		server.track(conn)
		server.clients.Add(1)
		go server.handleConn(conn)
	}

	// ---- graceful shutdown, in order ----
	log.Println("shutting down...")
	server.closeAllConns() // 2. disconnect clients
	server.clients.Wait()  // 3. wait for in-flight commands to finish
	close(done)            // 4. stop expiry and fsync loops
	background.Wait()
	if err := aof.Close(); err != nil { // 5. flush to disk and close
		return fmt.Errorf("close AOF: %w", err)
	}
	log.Println("bye")
	return nil
}

func (s *Server) track(conn net.Conn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	s.conns[conn] = struct{}{}
}

func (s *Server) untrack(conn net.Conn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	delete(s.conns, conn)
}

func (s *Server) closeAllConns() {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	for conn := range s.conns {
		conn.Close() // unblocks that client's resp.Read()
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.clients.Done()
	defer s.untrack(conn)
	defer conn.Close()

	addr := conn.RemoteAddr().String()
	log.Println("client connected:", addr)

	resp := NewResp(conn)
	for {
		value, err := resp.Read()
		if err != nil {
			// EOF = client left; ErrClosed = we closed it during shutdown.
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				log.Println("read error:", err)
			}
			log.Println("client disconnected:", addr)
			return
		}

		if value.typ != "array" || len(value.array) == 0 {
			conn.Write(errorValue("ERR invalid request").Marshal())
			continue
		}

		command := strings.ToUpper(value.array[0].str)
		args := value.array[1:]

		reply := s.execute(command, args)
		conn.Write(reply.Marshal())
	}
}

// execute runs a command, and logs it to the AOF if it changed data.
func (s *Server) execute(command string, args []Value) Value {
	if !writeCommands[command] {
		return handleCommand(s.store, command, args)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	reply := handleCommand(s.store, command, args)
	if reply.typ != "error" {
		for _, entry := range toAOF(command, args) {
			if err := s.aof.Write(entry); err != nil {
				log.Println("aof write error:", err)
			}
		}
	}
	return reply
}
