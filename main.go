package main

import (
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// Server holds everything shared by all client connections.
type Server struct {
	store   *Store
	aof     *AOF
	writeMu sync.Mutex // keeps the AOF in the same order as the store
}

func main() {
	store := NewStore()

	aof, err := NewAOF("appendonly.aof")
	if err != nil {
		log.Fatal("could not open AOF:", err)
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
		log.Fatal("could not replay AOF:", err)
	}
	log.Printf("loaded %d commands from appendonly.aof", count)

	server := &Server{store: store, aof: aof}

	// Background goroutines. done will be closed on shutdown in Phase 7.
	done := make(chan struct{})
	go store.RunExpiryLoop(100*time.Millisecond, done)
	go aof.RunSyncLoop(time.Second, done)

	listener, err := net.Listen("tcp", ":6380")
	if err != nil {
		log.Fatal("could not start server:", err)
	}
	log.Println("mini-redis listening on :6380")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("accept error:", err)
			continue
		}
		go server.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	addr := conn.RemoteAddr().String()
	log.Println("client connected:", addr)

	resp := NewResp(conn)
	for {
		value, err := resp.Read()
		if err != nil {
			if err != io.EOF {
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
		// Reads don't touch the AOF, so they don't need writeMu.
		return handleCommand(s.store, command, args)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	reply := handleCommand(s.store, command, args)
	if reply.typ != "error" { // only log commands that succeeded
		for _, entry := range toAOF(command, args) {
			if err := s.aof.Write(entry); err != nil {
				log.Println("aof write error:", err)
			}
		}
	}
	return reply
}
