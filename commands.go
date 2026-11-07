package main

import (
	"strconv"
	"strings"
	"time"
)

// A handler takes the store and the command's arguments and returns a reply.
type handlerFunc func(store *Store, args []Value) Value

var commands = map[string]handlerFunc{
	"PING":    ping,
	"ECHO":    echo,
	"SET":     set,
	"GET":     get,
	"DEL":     del,
	"EXISTS":  exists,
	"INCR":    incr,
	"DECR":    decr,
	"KEYS":    keys,
	"EXPIRE":  expire,
	"TTL":     ttl,
	"PTTL":    pttl,
	"PERSIST": persist,
	"DBSIZE":  dbsize,
}

func handleCommand(store *Store, command string, args []Value) Value {
	handler, ok := commands[command]
	if !ok {
		return errorValue("ERR unknown command '" + command + "'")
	}
	return handler(store, args)
}

// ---- reply helpers ----

func simpleString(s string) Value { return Value{typ: "string", str: s} }
func bulkString(s string) Value   { return Value{typ: "bulk", str: s} }
func integer(n int) Value         { return Value{typ: "integer", num: n} }
func errorValue(s string) Value   { return Value{typ: "error", str: s} }
func nullValue() Value            { return Value{typ: "null"} }

func wrongArgs(command string) Value {
	return errorValue("ERR wrong number of arguments for '" + strings.ToLower(command) + "' command")
}

// ---- handlers ----

func ping(store *Store, args []Value) Value {
	if len(args) == 0 {
		return simpleString("PONG")
	}
	return bulkString(args[0].str)
}

func echo(store *Store, args []Value) Value {
	if len(args) != 1 {
		return wrongArgs("echo")
	}
	return bulkString(args[0].str)
}

// SET key value [EX seconds | PX milliseconds]
func set(store *Store, args []Value) Value {
	if len(args) < 2 {
		return wrongArgs("set")
	}
	if len(args) != 2 && len(args) != 4 {
		return errorValue("ERR syntax error")
	}

	var expiry time.Duration
	if len(args) == 4 {
		n, err := strconv.Atoi(args[3].str)
		if err != nil {
			return errorValue(ErrNotInteger.Error())
		}
		if n <= 0 {
			return errorValue("ERR invalid expire time in 'set' command")
		}
		switch strings.ToUpper(args[2].str) {
		case "EX":
			expiry = time.Duration(n) * time.Second
		case "PX":
			expiry = time.Duration(n) * time.Millisecond
		default:
			return errorValue("ERR syntax error")
		}
	}

	store.SetWithTTL(args[0].str, args[1].str, expiry)
	return simpleString("OK")
}

// GET key
func get(store *Store, args []Value) Value {
	if len(args) != 1 {
		return wrongArgs("get")
	}
	value, ok := store.Get(args[0].str)
	if !ok {
		return nullValue()
	}
	return bulkString(value)
}

// DEL key [key ...]  -> number of keys deleted
func del(store *Store, args []Value) Value {
	if len(args) == 0 {
		return wrongArgs("del")
	}
	count := 0
	for _, arg := range args {
		if store.Delete(arg.str) {
			count++
		}
	}
	return integer(count)
}

// EXISTS key [key ...]  -> number of keys that exist
func exists(store *Store, args []Value) Value {
	if len(args) == 0 {
		return wrongArgs("exists")
	}
	count := 0
	for _, arg := range args {
		if store.Exists(arg.str) {
			count++
		}
	}
	return integer(count)
}

func incr(store *Store, args []Value) Value {
	if len(args) != 1 {
		return wrongArgs("incr")
	}
	n, err := store.IncrBy(args[0].str, 1)
	if err != nil {
		return errorValue(err.Error())
	}
	return integer(n)
}

func decr(store *Store, args []Value) Value {
	if len(args) != 1 {
		return wrongArgs("decr")
	}
	n, err := store.IncrBy(args[0].str, -1)
	if err != nil {
		return errorValue(err.Error())
	}
	return integer(n)
}

// KEYS pattern
func keys(store *Store, args []Value) Value {
	if len(args) != 1 {
		return wrongArgs("keys")
	}
	result := Value{typ: "array", array: []Value{}}
	for _, key := range store.Keys(args[0].str) {
		result.array = append(result.array, bulkString(key))
	}
	return result
}

// EXPIRE key seconds  -> 1 if the timeout was set, 0 if the key is missing
func expire(store *Store, args []Value) Value {
	if len(args) != 2 {
		return wrongArgs("expire")
	}
	seconds, err := strconv.Atoi(args[1].str)
	if err != nil {
		return errorValue(ErrNotInteger.Error())
	}
	if store.Expire(args[0].str, time.Duration(seconds)*time.Second) {
		return integer(1)
	}
	return integer(0)
}

// TTL key  -> seconds left, -1 if no expiry, -2 if missing
func ttl(store *Store, args []Value) Value {
	if len(args) != 1 {
		return wrongArgs("ttl")
	}
	ms := store.PTTL(args[0].str)
	if ms < 0 {
		return integer(int(ms))
	}
	return integer(int((ms + 500) / 1000)) // round to the nearest second
}

// PTTL key  -> milliseconds left, -1 if no expiry, -2 if missing
func pttl(store *Store, args []Value) Value {
	if len(args) != 1 {
		return wrongArgs("pttl")
	}
	return integer(int(store.PTTL(args[0].str)))
}

// PERSIST key  -> 1 if a timeout was removed, 0 otherwise
func persist(store *Store, args []Value) Value {
	if len(args) != 1 {
		return wrongArgs("persist")
	}
	if store.Persist(args[0].str) {
		return integer(1)
	}
	return integer(0)
}

// DBSIZE  -> number of keys in memory
func dbsize(store *Store, args []Value) Value {
	if len(args) != 0 {
		return wrongArgs("dbsize")
	}
	return integer(store.DBSize())
}
