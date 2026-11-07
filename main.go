package main

import (
	"io"
	"log"
	"net"
	"strings"
)

func main() {
	store := NewStore() // one store shared by all clients

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
		go handleConn(conn, store)
	}
}

func handleConn(conn net.Conn, store *Store) {
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
		log.Println("command:", command)

		reply := handleCommand(store, command, args)
		conn.Write(reply.Marshal())
	}
}
