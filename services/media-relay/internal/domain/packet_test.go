package domain

import "testing"

func TestParseHeader(t *testing.T) {
	c, u, ok := ParseHeader([]byte("call-1 ana\nAUDIO"))
	if !ok || c != "call-1" || u != "ana" {
		t.Fatalf("got %q %q %v", c, u, ok)
	}
	if c, u, ok := ParseHeader([]byte("call-1 ana 42\nAUDIO")); !ok || c != "call-1" || u != "ana" {
		t.Fatalf("with seq: got %q %q %v", c, u, ok)
	}
	for _, bad := range []string{"no newline", "onlycall\nx", " \nx", "call-1 \nx"} {
		if _, _, ok := ParseHeader([]byte(bad)); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}
