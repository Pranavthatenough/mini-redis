# mini-redis

A Redis-compatible in-memory key-value server, written from scratch in Go.
It speaks the real Redis protocol, so it works with the official `redis-cli` and `redis-benchmark`.

## Features

- **RESP protocol**: compatible with real Redis clients
- **Data types**: strings, lists and hashes, with `WRONGTYPE` errors
- **Key expiry**: lazy and active expiry, the same strategy Redis uses
- **Persistence**: append-only file with fsync every second; data survives restarts
- **Concurrency**: one goroutine per client, race-free (tested with `go test -race`)
- **Graceful shutdown**: on Ctrl+C or SIGTERM, finishes in-flight commands and flushes to disk

## Quick start

```bash
git clone https://github.com/Pranavthatenough/mini-redis.git
cd mini-redis
go build -o mini-redis .
./mini-redis
```

In another terminal:

```bash
redis-cli -p 6380 SET name pranav
redis-cli -p 6380 GET name
```

| Flag | Default | Description |
| --- | --- | --- |
| `-port` | `6380` | TCP port to listen on |
| `-aof` | `appendonly.aof` | Path to the append-only file |

## Supported commands

| Type | Commands |
| --- | --- |
| Strings | `SET` (with `EX` / `PX`), `GET`, `INCR`, `DECR` |
| Lists | `LPUSH`, `RPUSH`, `LPOP`, `RPOP`, `LLEN`, `LRANGE` |
| Hashes | `HSET`, `HGET`, `HDEL`, `HGETALL`, `HLEN` |
| Keys | `DEL`, `EXISTS`, `KEYS`, `TYPE`, `EXPIRE`, `PEXPIREAT`, `TTL`, `PTTL`, `PERSIST`, `DBSIZE` |
| Server | `PING`, `ECHO` |

## How it works

- **Networking:** a TCP listener starts one goroutine per client. Each goroutine reads RESP commands, runs them, and writes RESP replies.
- **Storage:** a map guarded by a `sync.RWMutex`, so many clients can read at once while writes are exclusive. Each key holds a typed entry (string, list or hash).
- **Expiry:** deadlines are kept in a separate map. Reads check the deadline (lazy expiry), and a background goroutine samples keys every 100 ms to remove expired ones nobody reads (active expiry).
- **Persistence:** every successful write is appended to the AOF in RESP format and replayed on startup. Relative timeouts like `EX 60` are stored as absolute deadlines (`PEXPIREAT`), so a restart never gives keys extra time. A write lock keeps the file in the same order as the in-memory changes.

## Benchmarks

`redis-benchmark -n 100000 -q`, 50 clients, AOF enabled on both servers.
Machine: <your CPU>, <your RAM>, Ubuntu <version>.

| Command | mini-redis (req/s) | Redis (req/s) |
| --- | --- | --- |
| SET | <your number> | <your number> |
| GET | <your number> | <your number> |
| INCR | <your number> | <your number> |
| LPUSH | <your number> | <your number> |
| HSET | <your number> | <your number> |

## Testing

```bash
go test -race -cover ./...
```

Includes unit tests for the RESP parser, store, expiry and AOF, plus end-to-end tests that start a real server and talk to it over TCP.

## What I learned

<!-- Write 3 to 5 points in your own words, for example: -->
<!-- - Why maps need a mutex, and how the race detector caught a lost update -->
<!-- - How Redis combines lazy and active expiry -->
<!-- - Why AOF stores absolute deadlines instead of relative TTLs -->
<!-- - How to shut down a concurrent server without losing data -->

## Not yet implemented

Pub/Sub, transactions (`MULTI` / `EXEC`), sorted sets, AOF rewriting, replication.