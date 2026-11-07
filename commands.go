package main

import (
	"strconv"
	"strings"
	"time"
)

// A handler takes the store and the command's arguments and returns a reply.
type handlerFunc func(store *Store, args []Value) Value

var commands = map[string]handlerFunc{
	// strings
	"PING": ping,
	"ECHO": echo,
	"SET":  set,
	"GET":  get,
	"INCR": incr,
	"DECR": decr,
	// any type
	"DEL":       del,
	"EXISTS":    exists,
	"KEYS":      keys,
	"TYPE":      typeCommand,
	"EXPIRE":    expire,
	"PEXPIREAT": pexpireat,
	"TTL":       ttl,
	"PTTL":      pttl,
	"PERSIST":   persist,
	"DBSIZE":    dbsize,
	// lists
	"LPUSH":  lpush,
	"RPUSH":  rpush,
	"LPOP":   lpop,
	"RPOP":   rpop,
	"LLEN":   llen,
	"LRANGE": lrange,
	// hashes
	"HSET":    hset,
	"HGET":    hget,
	"HDEL":    hdel,
	"HGETALL": hgetall,
	"HLEN":    hlen,
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
// GET key
func get(store *Store, args []Value) Value {
	if len(args) != 1 {
		return wrongArgs("get")
	}
	value, found, err := store.GetString(args[0].str)
	if err != nil {
		return errorValue(err.Error())
	}
	if !found {
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

// PEXPIREAT key unix-time-in-milliseconds  -> 1 if set, 0 if key missing
func pexpireat(store *Store, args []Value) Value {
	if len(args) != 2 {
		return wrongArgs("pexpireat")
	}
	ms, err := strconv.ParseInt(args[1].str, 10, 64)
	if err != nil {
		return errorValue(ErrNotInteger.Error())
	}
	if store.ExpireAt(args[0].str, time.UnixMilli(ms)) {
		return integer(1)
	}
	return integer(0)
}

// ---- more helpers ----

// arrayOf turns []string into a RESP array of bulk strings.
func arrayOf(items []string) Value {
	result := Value{typ: "array", array: make([]Value, 0, len(items))}
	for _, item := range items {
		result.array = append(result.array, bulkString(item))
	}
	return result
}

// stringsOf turns []Value into []string.
func stringsOf(args []Value) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = arg.str
	}
	return out
}

// TYPE key  ("type" is a Go keyword, so the function needs another name)
func typeCommand(store *Store, args []Value) Value {
	if len(args) != 1 {
		return wrongArgs("type")
	}
	return simpleString(store.Type(args[0].str))
}

// ---- lists ----

func push(store *Store, args []Value, left bool, name string) Value {
	if len(args) < 2 {
		return wrongArgs(name)
	}
	n, err := store.Push(args[0].str, stringsOf(args[1:]), left)
	if err != nil {
		return errorValue(err.Error())
	}
	return integer(n)
}

// LPUSH key value [value ...]  -> new length
func lpush(store *Store, args []Value) Value { return push(store, args, true, "lpush") }

// RPUSH key value [value ...]  -> new length
func rpush(store *Store, args []Value) Value { return push(store, args, false, "rpush") }

func pop(store *Store, args []Value, left bool, name string) Value {
	if len(args) != 1 {
		return wrongArgs(name)
	}
	value, found, err := store.Pop(args[0].str, left)
	if err != nil {
		return errorValue(err.Error())
	}
	if !found {
		return nullValue()
	}
	return bulkString(value)
}

// LPOP key  -> first item, or nil
func lpop(store *Store, args []Value) Value { return pop(store, args, true, "lpop") }

// RPOP key  -> last item, or nil
func rpop(store *Store, args []Value) Value { return pop(store, args, false, "rpop") }

// LLEN key
func llen(store *Store, args []Value) Value {
	if len(args) != 1 {
		return wrongArgs("llen")
	}
	n, err := store.LLen(args[0].str)
	if err != nil {
		return errorValue(err.Error())
	}
	return integer(n)
}

// LRANGE key start stop
func lrange(store *Store, args []Value) Value {
	if len(args) != 3 {
		return wrongArgs("lrange")
	}
	start, err1 := strconv.Atoi(args[1].str)
	stop, err2 := strconv.Atoi(args[2].str)
	if err1 != nil || err2 != nil {
		return errorValue(ErrNotInteger.Error())
	}
	items, err := store.LRange(args[0].str, start, stop)
	if err != nil {
		return errorValue(err.Error())
	}
	return arrayOf(items)
}

// ---- hashes ----

// HSET key field value [field value ...]  -> number of new fields
func hset(store *Store, args []Value) Value {
	// Need a key plus at least one field/value pair, and pairs must be complete.
	if len(args) < 3 || (len(args)-1)%2 != 0 {
		return wrongArgs("hset")
	}
	n, err := store.HSet(args[0].str, stringsOf(args[1:]))
	if err != nil {
		return errorValue(err.Error())
	}
	return integer(n)
}

// HGET key field
func hget(store *Store, args []Value) Value {
	if len(args) != 2 {
		return wrongArgs("hget")
	}
	value, found, err := store.HGet(args[0].str, args[1].str)
	if err != nil {
		return errorValue(err.Error())
	}
	if !found {
		return nullValue()
	}
	return bulkString(value)
}

// HDEL key field [field ...]  -> number of fields removed
func hdel(store *Store, args []Value) Value {
	if len(args) < 2 {
		return wrongArgs("hdel")
	}
	n, err := store.HDel(args[0].str, stringsOf(args[1:]))
	if err != nil {
		return errorValue(err.Error())
	}
	return integer(n)
}

// HGETALL key  -> [field1, value1, field2, value2, ...]
func hgetall(store *Store, args []Value) Value {
	if len(args) != 1 {
		return wrongArgs("hgetall")
	}
	items, err := store.HGetAll(args[0].str)
	if err != nil {
		return errorValue(err.Error())
	}
	return arrayOf(items)
}

// HLEN key
func hlen(store *Store, args []Value) Value {
	if len(args) != 1 {
		return wrongArgs("hlen")
	}
	n, err := store.HLen(args[0].str)
	if err != nil {
		return errorValue(err.Error())
	}
	return integer(n)
}
