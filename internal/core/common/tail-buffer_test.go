package common

import (
	"strings"
	"testing"
)

func TestTailBufferRemainsBounded(t *testing.T) {
	buffer := NewTailBuffer(8)
	for _, chunk := range []string{"12345", "67890", strings.Repeat("x", 10000), "tail"} {
		n, err := buffer.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatal("short write")
		}
		if len(buffer.data) > 8 {
			t.Fatal("diagnostics exceeded limit")
		}
	}
	if buffer.String() != "xxxxtail" {
		t.Fatalf("unexpected diagnostics: %q", buffer.String())
	}
	empty := NewTailBuffer(0)
	if n, _ := empty.Write([]byte("ignored")); n != 7 || empty.String() != "" {
		t.Fatal("zero-size buffer retained diagnostics")
	}
}
