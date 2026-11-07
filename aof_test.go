package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func replayInto(t *testing.T, aof *AOF, store *Store) int {
	t.Helper()
	count, err := aof.Replay(func(v Value) {
		handleCommand(store, strings.ToUpper(v.array[0].str), v.array[1:])
	})
	if err != nil {
		t.Fatalf("replay failed: %v", err)
	}
	return count
}

func TestAOFReplay(t *testing.T) {
	// t.TempDir gives a fresh folder that Go deletes after the test.
	path := filepath.Join(t.TempDir(), "test.aof")

	aof, err := NewAOF(path)
	if err != nil {
		t.Fatal(err)
	}
	aof.Write(commandValue("SET", "name", "pranav"))
	aof.Write(commandValue("INCR", "counter"))
	aof.Write(commandValue("INCR", "counter"))
	aof.Write(commandValue("SET", "temp", "x"))
	aof.Write(commandValue("DEL", "temp"))
	aof.Close()

	// Simulate a restart: reopen the file and replay into a fresh store.
	aof2, err := NewAOF(path)
	if err != nil {
		t.Fatal(err)
	}
	defer aof2.Close()

	store := NewStore()
	if count := replayInto(t, aof2, store); count != 5 {
		t.Fatalf("replayed %d commands, want 5", count)
	}

	if got, _ := store.Get("name"); got != "pranav" {
		t.Errorf("name = %q, want %q", got, "pranav")
	}
	if got, _ := store.Get("counter"); got != "2" {
		t.Errorf("counter = %q, want %q", got, "2")
	}
	if store.Exists("temp") {
		t.Error("temp should have been deleted")
	}
}

func TestToAOFUsesAbsoluteDeadline(t *testing.T) {
	args := []Value{bulkString("k"), bulkString("v"), bulkString("EX"), bulkString("10")}
	entries := toAOF("SET", args)

	if len(entries) != 2 || entries[1].array[0].str != "PEXPIREAT" {
		t.Fatalf("expected SET followed by PEXPIREAT, got %+v", entries)
	}

	ms, _ := strconv.ParseInt(entries[1].array[2].str, 10, 64)
	want := time.Now().Add(10 * time.Second).UnixMilli()
	if diff := want - ms; diff < 0 || diff > 1000 {
		t.Fatalf("deadline %d is not about 10s from now (%d)", ms, want)
	}
}

func TestReplayPastDeadlineDeletesKey(t *testing.T) {
	store := NewStore()
	handleCommand(store, "SET", []Value{bulkString("k"), bulkString("v")})

	past := strconv.FormatInt(time.Now().Add(-time.Minute).UnixMilli(), 10)
	handleCommand(store, "PEXPIREAT", []Value{bulkString("k"), bulkString(past)})

	if store.Exists("k") {
		t.Fatal("a key whose deadline already passed should be deleted on replay")
	}
}
