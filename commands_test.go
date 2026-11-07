package main

import "testing"

func TestCommands(t *testing.T) {
	s := NewStore()

	// Runs in order: later rows depend on earlier ones.
	tests := []struct {
		cmd  []string
		want Value
	}{
		// ---- strings ----
		{[]string{"PING", "hello"}, bulkString("hello")},
		{[]string{"ECHO", "hi"}, bulkString("hi")},
		{[]string{"ECHO"}, wrongArgs("echo")},
		{[]string{"SET", "k", "v"}, simpleString("OK")},
		{[]string{"SET", "k"}, wrongArgs("set")},
		{[]string{"SET", "k", "v", "EX"}, errorValue("ERR syntax error")},
		{[]string{"SET", "k", "v", "XX", "5"}, errorValue("ERR syntax error")},
		{[]string{"SET", "k", "v", "EX", "0"}, errorValue("ERR invalid expire time in 'set' command")},
		{[]string{"SET", "k", "v", "EX", "abc"}, errorValue(ErrNotInteger.Error())},
		{[]string{"SET", "t", "v", "PX", "60000"}, simpleString("OK")},
		{[]string{"GET", "k"}, bulkString("v")},
		{[]string{"GET"}, wrongArgs("get")},
		{[]string{"DECR", "num"}, integer(-1)},
		{[]string{"INCR"}, wrongArgs("incr")},
		{[]string{"DECR", "k"}, errorValue(ErrNotInteger.Error())},

		// ---- keys ----
		{[]string{"EXISTS", "k", "nope"}, integer(1)},
		{[]string{"EXISTS"}, wrongArgs("exists")},
		{[]string{"TYPE", "k"}, simpleString("string")},
		{[]string{"TYPE", "nope"}, simpleString("none")},
		{[]string{"KEYS", "t"}, arrayOf([]string{"t"})},
		{[]string{"KEYS", "zzz*"}, arrayOf([]string{})},
		{[]string{"TTL", "k"}, integer(-1)},
		{[]string{"TTL", "nope"}, integer(-2)},
		{[]string{"TTL", "t"}, integer(60)},
		{[]string{"PTTL", "nope"}, integer(-2)},
		{[]string{"EXPIRE", "k", "100"}, integer(1)},
		{[]string{"EXPIRE", "nope", "100"}, integer(0)},
		{[]string{"EXPIRE", "k", "abc"}, errorValue(ErrNotInteger.Error())},
		{[]string{"PERSIST", "k"}, integer(1)},
		{[]string{"PERSIST", "k"}, integer(0)},
		{[]string{"PEXPIREAT", "k", "abc"}, errorValue(ErrNotInteger.Error())},
		{[]string{"DBSIZE"}, integer(3)}, // k, t, num
		{[]string{"DBSIZE", "extra"}, wrongArgs("dbsize")},
		{[]string{"DEL", "k", "nope"}, integer(1)},
		{[]string{"DEL"}, wrongArgs("del")},

		// ---- lists ----
		{[]string{"RPUSH", "l"}, wrongArgs("rpush")},
		{[]string{"RPUSH", "l", "a", "b", "c"}, integer(3)},
		{[]string{"LPUSH", "t", "x"}, errorValue(ErrWrongType.Error())},
		{[]string{"LLEN", "l"}, integer(3)},
		{[]string{"LLEN", "nope"}, integer(0)},
		{[]string{"LRANGE", "l", "0", "-1"}, arrayOf([]string{"a", "b", "c"})},
		{[]string{"LRANGE", "l", "x", "1"}, errorValue(ErrNotInteger.Error())},
		{[]string{"LRANGE", "t", "0", "1"}, errorValue(ErrWrongType.Error())},
		{[]string{"RPOP", "l"}, bulkString("c")},
		{[]string{"LPOP", "nope"}, nullValue()},
		{[]string{"LPOP"}, wrongArgs("lpop")},

		// ---- hashes ----
		{[]string{"HSET", "h", "f"}, wrongArgs("hset")},
		{[]string{"HSET", "h", "a", "1", "b"}, wrongArgs("hset")},
		{[]string{"HSET", "h", "a", "1", "b", "2"}, integer(2)},
		{[]string{"HGET", "h", "a"}, bulkString("1")},
		{[]string{"HGET", "h", "zzz"}, nullValue()},
		{[]string{"HGET", "l", "a"}, errorValue(ErrWrongType.Error())},
		{[]string{"HLEN", "h"}, integer(2)},
		{[]string{"HDEL", "h", "a", "zzz"}, integer(1)},
		{[]string{"HGETALL", "h"}, arrayOf([]string{"b", "2"})},
		{[]string{"HGETALL", "nope"}, arrayOf([]string{})},
		{[]string{"TYPE", "h"}, simpleString("hash")},

		// ---- unknown ----
		{[]string{"NOPE"}, errorValue("ERR unknown command 'NOPE'")},
	}

	for _, tt := range tests {
		args := make([]Value, 0, len(tt.cmd)-1)
		for _, a := range tt.cmd[1:] {
			args = append(args, bulkString(a))
		}

		got := handleCommand(s, tt.cmd[0], args)
		if string(got.Marshal()) != string(tt.want.Marshal()) {
			t.Errorf("%v: got %q, want %q", tt.cmd, got.Marshal(), tt.want.Marshal())
		}
	}
}
