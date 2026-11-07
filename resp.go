package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Value is one piece of RESP data.
// typ is one of: "string", "error", "integer", "bulk", "array", "null"
type Value struct {
	typ   string
	str   string  // used by string, error and bulk
	num   int     // used by integer
	array []Value // used by array
}

// Marshal turns a Value into RESP bytes to send to the client.
func (v Value) Marshal() []byte {
	switch v.typ {
	case "string":
		return []byte("+" + v.str + "\r\n")
	case "error":
		return []byte("-" + v.str + "\r\n")
	case "integer":
		return []byte(":" + strconv.Itoa(v.num) + "\r\n")
	case "bulk":
		return []byte("$" + strconv.Itoa(len(v.str)) + "\r\n" + v.str + "\r\n")
	case "array":
		out := []byte("*" + strconv.Itoa(len(v.array)) + "\r\n")
		for _, item := range v.array {
			out = append(out, item.Marshal()...)
		}
		return out
	case "null":
		return []byte("$-1\r\n")
	default:
		return []byte{}
	}
}

// Resp reads RESP values from a connection.
type Resp struct {
	reader *bufio.Reader
}

func NewResp(rd io.Reader) *Resp {
	return &Resp{reader: bufio.NewReader(rd)}
}

// Read reads one complete value (usually a command array).
func (r *Resp) Read() (Value, error) {
	typ, err := r.reader.ReadByte()
	if err != nil {
		return Value{}, err
	}

	switch typ {
	case '*':
		return r.readArray()
	case '$':
		return r.readBulk()
	case '+':
		line, err := r.readLine()
		return Value{typ: "string", str: line}, err
	case '-':
		line, err := r.readLine()
		return Value{typ: "error", str: line}, err
	case ':':
		n, err := r.readInteger()
		return Value{typ: "integer", num: n}, err
	default:
		return Value{}, fmt.Errorf("protocol error: unknown type %q", typ)
	}
}

// readLine reads up to \r\n and returns the line without it.
func (r *Resp) readLine() (string, error) {
	line, err := r.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return "", errors.New("protocol error: expected \\r\\n")
	}
	return line[:len(line)-2], nil
}

// readInteger reads a line and converts it to a number.
func (r *Resp) readInteger() (int, error) {
	line, err := r.readLine()
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(line)
	if err != nil {
		return 0, fmt.Errorf("protocol error: invalid number %q", line)
	}
	return n, nil
}

func (r *Resp) readArray() (Value, error) {
	n, err := r.readInteger()
	if err != nil {
		return Value{}, err
	}
	if n < 0 {
		return Value{typ: "null"}, nil
	}

	v := Value{typ: "array", array: make([]Value, 0, n)}
	for i := 0; i < n; i++ {
		item, err := r.Read() // each item is itself a RESP value
		if err != nil {
			return Value{}, err
		}
		v.array = append(v.array, item)
	}
	return v, nil
}

func (r *Resp) readBulk() (Value, error) {
	n, err := r.readInteger()
	if err != nil {
		return Value{}, err
	}
	if n < 0 {
		return Value{typ: "null"}, nil
	}

	buf := make([]byte, n+2) // the data plus the trailing \r\n
	if _, err := io.ReadFull(r.reader, buf); err != nil {
		return Value{}, err
	}
	return Value{typ: "bulk", str: string(buf[:n])}, nil
}
