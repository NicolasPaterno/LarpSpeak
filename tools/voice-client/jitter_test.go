package main

import (
	"encoding/binary"
	"testing"
	"time"
)

func frame(v int16) []byte {
	f := make([]byte, frameBytes)
	for i := 0; i < frameSamples; i++ {
		binary.LittleEndian.PutUint16(f[2*i:], uint16(v))
	}
	return f
}

func sample(f []byte) int16 { return int16(binary.LittleEndian.Uint16(f)) }

func TestReorderLossAndMixing(t *testing.T) {
	m, now := newMixer(), time.Now()
	// ana's packets arrive out of order (2 before 1) and packet 4 is lost:
	// packet 5 carries frame 4 as its redundant copy.
	m.push("ana", 2, frame(200), frame(100), now)
	m.push("ana", 1, frame(100), nil, now)
	m.push("ana", 3, frame(300), frame(200), now)
	m.push("ana", 5, frame(500), frame(400), now)
	// bob talks at the same time.
	for seq := uint32(1); seq <= 5; seq++ {
		m.push("bob", seq, frame(10), nil, now)
	}
	want := []int16{110, 210, 310, 410, 510} // ana + bob mixed, in order, frame 4 recovered
	for i, w := range want {
		if got := sample(m.tick(now)); got != w {
			t.Fatalf("tick %d: got %d want %d", i+1, got, w)
		}
	}
	// Nothing more from ana or bob: the last frames are faded, not cut to silence.
	if got := sample(m.tick(now)); got != 500/2+10/2 {
		t.Fatalf("concealment: got %d", got)
	}
	if m.stats.recovered != 1 {
		t.Fatalf("recovered = %d, want 1", m.stats.recovered)
	}
}

func TestClampAndLatePackets(t *testing.T) {
	m, now := newMixer(), time.Now()
	for _, u := range []string{"a", "b"} {
		for seq := uint32(1); seq <= 3; seq++ {
			m.push(u, seq, frame(30000), nil, now)
		}
	}
	if got := sample(m.tick(now)); got != 32767 {
		t.Fatalf("not clamped: %d", got)
	}
	m.push("a", 1, frame(1), nil, now) // already played: must be dropped
	if m.stats.late != 1 {
		t.Fatalf("late = %d, want 1", m.stats.late)
	}
}
