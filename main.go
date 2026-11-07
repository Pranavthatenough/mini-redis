package main

import (
	"io"
	"log"
	"net"
	"strings"
)

func main() {
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
		go handleConn(conn)
	}
}

func handleConn(conn net.Conn) {
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
			conn.Write(Value{typ: "error", str: "ERR invalid request"}.Marshal())
			continue
		}

		command := strings.ToUpper(value.array[0].str)
		args := value.array[1:]
		log.Println("command:", command)

		reply := handleCommand(command, args)
		conn.Write(reply.Marshal())
	}
}

func handleCommand(command string, args []Value) Value {
	switch command {
	case "PING":
		if len(args) == 0 {
			return Value{typ: "string", str: "PONG"}
		}
		return Value{typ: "bulk", str: args[0].str}

	case "ECHO":
		if len(args) != 1 {
			return Value{typ: "error", str: "ERR wrong number of arguments for 'echo' command"}
		}
		return Value{typ: "bulk", str: args[0].str}

	default:
		return Value{typ: "error", str: "ERR unknown command '" + command + "'"}
	}
}
