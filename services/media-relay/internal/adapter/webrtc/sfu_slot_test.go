package webrtc

import "testing"

func TestSlotRewriteContinuesAcrossSources(t *testing.T) {
	sl := &slot{}
	for i := range uint16(3) { // first source: untouched
		seq, ts := sl.rewrite(0xA, 100+i, 5000+uint32(i)*960)
		if seq != 100+i || ts != 5000+uint32(i)*960 {
			t.Fatalf("A pkt %d: got %d/%d", i, seq, ts)
		}
	}
	if seq, ts := sl.rewrite(0xB, 60000, 4_000_000_000); seq != 103 || ts != 5000+3*960 {
		t.Fatalf("B first: got %d/%d, want 103/%d", seq, ts, 5000+3*960)
	}
	if seq, ts := sl.rewrite(0xB, 60001, 4_000_000_960); seq != 104 || ts != 5000+4*960 {
		t.Fatalf("B second: got %d/%d", seq, ts)
	}
}

func TestSlotRewriteWraps(t *testing.T) {
	sl := &slot{}
	sl.rewrite(0xA, 65535, 0xFFFFFC40) // last ts = 2^32-960
	if seq, ts := sl.rewrite(0xB, 7, 123); seq != 0 || ts != 0 {
		t.Fatalf("got %d/%d, want 0/0", seq, ts)
	}
	if seq, ts := sl.rewrite(0xB, 8, 1083); seq != 1 || ts != 960 {
		t.Fatalf("got %d/%d, want 1/960", seq, ts)
	}
}
