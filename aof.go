package main

import (
	"errors"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AOF is an append-only file of write commands, stored in RESP format.
type AOF struct {
	mu   sync.Mutex
	file *os.File
}

func NewAOF(path string) (*AOF, error) {
	// O_APPEND: every write goes to the end of the file.
	// O_CREATE: create the file if it doesn't exist.
	// 0644: owner can read/write, others can read.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &AOF{file: file}, nil
}

// Write appends one command. The OS holds it in memory until the next Sync.
func (a *AOF) Write(v Value) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err := a.file.Write(v.Marshal())
	return err
}

// Sync forces everything written so far onto the physical disk.
func (a *AOF) Sync() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.file.Sync()
}

func (a *AOF) Close() error {
	if err := a.Sync(); err != nil {
		return err
	}
	return a.file.Close()
}

// RunSyncLoop calls Sync every interval until done is closed.
// This is Redis's default "appendfsync everysec" policy.
func (a *AOF) RunSyncLoop(interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := a.Sync(); err != nil {
				log.Println("aof sync error:", err)
			}
		}
	}
}

// Replay reads every command in the file from the start and calls apply
// for each one. It returns how many commands it read.
func (a *AOF) Replay(apply func(v Value)) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, err := a.file.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}

	resp := NewResp(a.file)
	count := 0
	for {
		v, err := resp.Read()
		if errors.Is(err, io.EOF) {
			return count, nil // reached the end cleanly
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			// The server crashed halfway through writing the last command.
			// Keep everything before it, like Redis does by default.
			log.Println("warning: AOF ends with an incomplete command, ignoring it")
			return count, nil
		}
		if err != nil {
			return count, err
		}
		apply(v)
		count++
	}
}

// ---- deciding what to write ----

// writeCommands lists the commands that change data and must be logged.
// writeCommands lists the commands that change data and must be logged.
var writeCommands = map[string]bool{
	"SET":       true,
	"DEL":       true,
	"INCR":      true,
	"DECR":      true,
	"EXPIRE":    true,
	"PEXPIREAT": true,
	"PERSIST":   true,
	"LPUSH":     true,
	"RPUSH":     true,
	"LPOP":      true, // popping removes an item, so it's a write
	"RPOP":      true,
	"HSET":      true,
	"HDEL":      true,
}

// commandValue builds a RESP command array, like ["SET", "name", "pranav"].
func commandValue(parts ...string) Value {
	v := Value{typ: "array", array: make([]Value, 0, len(parts))}
	for _, p := range parts {
		v.array = append(v.array, bulkString(p))
	}
	return v
}

// toAOF returns what to append for a successful write command.
// Relative timeouts become absolute deadlines, so replaying later
// doesn't give keys extra time.
func toAOF(command string, args []Value) []Value {
	switch command {
	case "SET":
		if len(args) == 4 { // SET key value EX|PX n (already validated)
			n, _ := strconv.Atoi(args[3].str)
			ttl := time.Duration(n) * time.Second
			if strings.ToUpper(args[2].str) == "PX" {
				ttl = time.Duration(n) * time.Millisecond
			}
			deadline := time.Now().Add(ttl).UnixMilli()
			return []Value{
				commandValue("SET", args[0].str, args[1].str),
				commandValue("PEXPIREAT", args[0].str, strconv.FormatInt(deadline, 10)),
			}
		}

	case "EXPIRE": // EXPIRE key seconds (already validated)
		n, _ := strconv.Atoi(args[1].str)
		deadline := time.Now().Add(time.Duration(n) * time.Second).UnixMilli()
		return []Value{
			commandValue("PEXPIREAT", args[0].str, strconv.FormatInt(deadline, 10)),
		}
	}

	// Everything else is logged exactly as the client sent it.
	parts := []string{command}
	for _, arg := range args {
		parts = append(parts, arg.str)
	}
	return []Value{commandValue(parts...)}
}
