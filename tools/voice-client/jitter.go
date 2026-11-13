package main

import (
	"encoding/binary"
	"sync"
	"time"
)

// Audio format: 16 kHz, mono, signed 16-bit little-endian PCM, 20 ms frames.
const (
	sampleRate    = 16000
	frameSamples  = sampleRate / 50 // 320 samples = 20 ms
	frameBytes    = frameSamples * 2
	frameDuration = 20 * time.Millisecond

	playoutDelay = 3  // frames buffered before a speaker starts playing (60 ms)
	maxBuffered  = 15 // frames; beyond this we skip ahead to keep latency low
	maxConceal   = 3  // consecutive lost frames we hide by fading the last frame
	speakerGone  = time.Second
)

// stream is one remote speaker's jitter buffer.
type stream struct {
	frames   map[uint32][]byte
	next     uint32 // sequence number to play on the next tick
	started  bool
	last     []byte // last frame played, used to conceal losses
	misses   int
	lastRecv time.Time
}

// mixer keeps a jitter buffer per speaker and mixes everyone into one output
// frame every 20 ms, so several people can talk at the same time.
type mixer struct {
	mu      sync.Mutex
	streams map[string]*stream
	stats   stats
}

type stats struct{ received, late, recovered, concealed, lost int }

func newMixer() *mixer { return &mixer{streams: map[string]*stream{}} }

// push stores a received packet. prev is the redundant copy of frame seq-1
// carried in the same packet: it rebuilds that frame if its own packet was lost.
func (m *mixer) push(user string, seq uint32, cur, prev []byte, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.streams[user]
	if s == nil {
		s = &stream{frames: map[uint32][]byte{}}
		m.streams[user] = s
	}
	s.lastRecv = now
	m.stats.received++
	if !m.store(s, seq, cur) {
		m.stats.late++
	}
	if prev != nil && seq > 0 {
		if _, have := s.frames[seq-1]; !have && m.store(s, seq-1, prev) && s.started {
			m.stats.recovered++
		}
	}
	if !s.started && len(s.frames) >= playoutDelay {
		s.started, s.next = true, minKey(s.frames)
	}
	if s.started && len(s.frames) > maxBuffered { // sender's clock runs fast or we stalled: catch up
		s.next = maxKey(s.frames) - playoutDelay + 1
		for k := range s.frames {
			if k < s.next {
				delete(s.frames, k)
			}
		}
	}
}

func (m *mixer) store(s *stream, seq uint32, frame []byte) bool {
	if len(frame) != frameBytes || (s.started && seq < s.next) {
		return false // malformed or too late to play
	}
	if _, have := s.frames[seq]; !have {
		s.frames[seq] = append([]byte(nil), frame...)
	}
	return true
}

// tick produces the next 20 ms of mixed audio.
func (m *mixer) tick(now time.Time) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	var acc [frameSamples]int32
	for user, s := range m.streams {
		if now.Sub(s.lastRecv) > speakerGone {
			delete(m.streams, user)
			continue
		}
		if !s.started {
			continue
		}
		if f, ok := s.frames[s.next]; ok {
			delete(s.frames, s.next)
			s.last, s.misses = f, 0
			addFrame(&acc, f, 1)
		} else {
			s.misses++
			if s.last != nil && s.misses <= maxConceal {
				addFrame(&acc, s.last, 1/float64(int(1)<<s.misses)) // repeat last frame, fading out
				m.stats.concealed++
			} else {
				m.stats.lost++
			}
		}
		s.next++
	}
	out := make([]byte, frameBytes)
	for i, v := range acc {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(int16(max(-32768, min(32767, v)))))
	}
	return out
}

func addFrame(acc *[frameSamples]int32, f []byte, gain float64) {
	for i := range acc {
		acc[i] += int32(float64(int16(binary.LittleEndian.Uint16(f[2*i:]))) * gain)
	}
}

func minKey(m map[uint32][]byte) uint32 {
	first, k := true, uint32(0)
	for x := range m {
		if first || x < k {
			first, k = false, x
		}
	}
	return k
}

func maxKey(m map[uint32][]byte) uint32 {
	var k uint32
	for x := range m {
		k = max(k, x)
	}
	return k
}
