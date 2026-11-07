package main

import (
	"strings"
	"testing"
)

func TestReadCommand(t *testing.T) {
	input := "*2\r\n$3\r\nGET\r\n$4\r\nname\r\n"

	v, err := NewResp(strings.NewReader(input)).Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.typ != "array" || len(v.array) != 2 {
		t.Fatalf("expected array of 2, got %+v", v)
	}
	if v.array[0].str != "GET" || v.array[1].str != "name" {
		t.Fatalf("wrong items: %+v", v.array)
	}
}

func TestReadInvalid(t *testing.T) {
	_, err := NewResp(strings.NewReader("?oops\r\n")).Read()
	if err == nil {
		t.Fatal("expected an error for unknown type, got nil")
	}
}

func TestMarshal(t *testing.T) {
	tests := []struct {
		name string
		in   Value
		want string
	}{
		{"simple string", Value{typ: "string", str: "OK"}, "+OK\r\n"},
		{"error", Value{typ: "error", str: "ERR bad"}, "-ERR bad\r\n"},
		{"integer", Value{typ: "integer", num: 42}, ":42\r\n"},
		{"bulk", Value{typ: "bulk", str: "hello"}, "$5\r\nhello\r\n"},
		{"empty bulk", Value{typ: "bulk", str: ""}, "$0\r\n\r\n"},
		{"null", Value{typ: "null"}, "$-1\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(tt.in.Marshal())
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
